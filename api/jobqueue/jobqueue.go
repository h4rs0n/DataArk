package jobqueue

import (
	"DataArk/config"
	"DataArk/observability"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver"
	"github.com/riverqueue/river/riverdriver/riverdatabasesql"
	"github.com/riverqueue/river/rivertype"
	"gorm.io/gorm"
)

const (
	FetchSourceJobKind      = "discovery_fetch_source"
	ScanBlogrollJobKind     = "discovery_scan_blogroll"
	BackfillSiteJobKind     = "discovery_backfill_site"
	ProcessCandidateJobKind = "discovery_process_candidate"
	AssessArticleJobKind    = "assessment_assess_article"
	GenerateDailyJobKind    = "recommendation_generate_daily"
)

var ErrHandlerUnavailable = errors.New("job handler is not registered")

type JobEnqueuer interface {
	EnqueueFetchSource(context.Context, uint) error
	EnqueueScanBlogroll(context.Context, uint) error
	EnqueueBackfillSite(context.Context, uint) error
	EnqueueProcessCandidate(context.Context, uint, string) error
	EnqueueAssessArticle(context.Context, uint, string) error
	EnqueueGenerateDaily(context.Context, uint, string) error
}

type Handlers struct {
	FetchSource      func(context.Context, uint) error
	ScanBlogroll     func(context.Context, uint) error
	BackfillSite     func(context.Context, uint) error
	ProcessCandidate func(context.Context, uint, string) error
	AssessArticle    func(context.Context, uint, string) error
	GenerateDaily    func(context.Context, uint, string) error
}

type RecoveryFunc func(context.Context, JobEnqueuer) error

type FetchSourceArgs struct {
	SourceID uint `json:"source_id"`
}

func (FetchSourceArgs) Kind() string { return FetchSourceJobKind }
func (FetchSourceArgs) InsertOpts() river.InsertOpts {
	return crawlUniqueByArgsOpts()
}

type ScanBlogrollArgs struct {
	SiteID uint `json:"site_id"`
}

func (ScanBlogrollArgs) Kind() string { return ScanBlogrollJobKind }
func (ScanBlogrollArgs) InsertOpts() river.InsertOpts {
	return crawlUniqueByArgsOpts()
}

type BackfillSiteArgs struct {
	SiteID uint `json:"site_id"`
}

func (BackfillSiteArgs) Kind() string { return BackfillSiteJobKind }
func (BackfillSiteArgs) InsertOpts() river.InsertOpts {
	return crawlUniqueByArgsOpts()
}

type ProcessCandidateArgs struct {
	CandidateID    uint   `json:"candidate_id"`
	ContentVersion string `json:"content_version"`
}

func (ProcessCandidateArgs) Kind() string { return ProcessCandidateJobKind }
func (ProcessCandidateArgs) InsertOpts() river.InsertOpts {
	return crawlUniqueByArgsOpts()
}

type GenerateDailyArgs struct {
	UserID    uint   `json:"user_id"`
	LocalDate string `json:"local_date"`
}

func (GenerateDailyArgs) Kind() string { return GenerateDailyJobKind }
func (GenerateDailyArgs) InsertOpts() river.InsertOpts {
	opts := uniqueByArgsOpts()
	opts.UniqueOpts.ByPeriod = 24 * time.Hour
	return opts
}

type AssessArticleArgs struct {
	CandidateID    uint   `json:"candidate_id"`
	ContentVersion string `json:"content_version"`
}

func (AssessArticleArgs) Kind() string { return AssessArticleJobKind }
func (AssessArticleArgs) InsertOpts() river.InsertOpts {
	opts := uniqueByArgsOpts()
	opts.Queue = AssessmentQueueName
	opts.MaxAttempts = 1
	return opts
}

func uniqueByArgsOpts() river.InsertOpts {
	return river.InsertOpts{UniqueOpts: river.UniqueOpts{ByArgs: true}}
}

func crawlUniqueByArgsOpts() river.InsertOpts {
	opts := uniqueByArgsOpts()
	opts.Queue = DiscoveryQueueName
	// Discovery handlers persist their own retry/backoff schedule. River must not
	// wake a failed crawl independently while the manual queue is unattended.
	opts.MaxAttempts = 1
	return opts
}

type defaultQueueEntry struct {
	id         uint64
	queue      JobEnqueuer
	controller CrawlQueueController
}

var defaultQueue struct {
	sync.RWMutex
	entry defaultQueueEntry
}

var defaultQueueSequence atomic.Uint64

var fallbackMemoryStores struct {
	sync.Mutex
	byDatabase map[*gorm.DB]*MemoryStore
	withoutDB  *MemoryStore
}

func Default() (JobEnqueuer, bool) {
	defaultQueue.RLock()
	defer defaultQueue.RUnlock()
	return defaultQueue.entry.queue, defaultQueue.entry.queue != nil
}

func installDefault(queue JobEnqueuer, controller CrawlQueueController) func() {
	id := defaultQueueSequence.Add(1)
	defaultQueue.Lock()
	previous := defaultQueue.entry
	defaultQueue.entry = defaultQueueEntry{id: id, queue: queue, controller: controller}
	defaultQueue.Unlock()
	return func() {
		defaultQueue.Lock()
		defer defaultQueue.Unlock()
		if defaultQueue.entry.id == id {
			defaultQueue.entry = previous
		}
	}
}

type riverQueue struct {
	client  *river.Client[*sql.Tx]
	runMu   sync.Mutex
	running bool
	stop    chan struct{}
}

type contextSQLExecer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

// reconcileManualDiscoveryJobs moves unfinished jobs created by releases that
// placed discovery work on River's automatically consumed default queue. It
// also makes jobs interrupted by the previous process available again. This
// runs before workers start, so neither category can execute before the owner
// explicitly starts the manual crawl queue.
func reconcileManualDiscoveryJobs(ctx context.Context, executor contextSQLExecer) (int64, error) {
	result, err := executor.ExecContext(ctx, `
UPDATE river_job
SET queue = $1,
	state = CASE WHEN state = 'running' THEN 'available'::river_job_state ELSE state END,
	attempted_at = CASE WHEN state = 'running' THEN NULL ELSE attempted_at END,
	attempted_by = CASE WHEN state = 'running' THEN NULL ELSE attempted_by END,
	scheduled_at = CASE WHEN state = 'running' THEN now() ELSE scheduled_at END,
	max_attempts = GREATEST(attempt, 1)
WHERE kind IN ($3, $4, $5, $6)
  AND (
	(queue = $2 AND state IN ('available', 'pending', 'retryable', 'running', 'scheduled'))
	OR (queue = $1 AND state = 'running')
  )`,
		DiscoveryQueueName,
		river.QueueDefault,
		FetchSourceJobKind,
		ScanBlogrollJobKind,
		BackfillSiteJobKind,
		ProcessCandidateJobKind,
	)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (queue *riverQueue) EnqueueFetchSource(ctx context.Context, sourceID uint) error {
	_, err := queue.client.Insert(ctx, FetchSourceArgs{SourceID: sourceID}, nil)
	return err
}

func (queue *riverQueue) EnqueueScanBlogroll(ctx context.Context, siteID uint) error {
	_, err := queue.client.Insert(ctx, ScanBlogrollArgs{SiteID: siteID}, nil)
	return err
}

func (queue *riverQueue) EnqueueBackfillSite(ctx context.Context, siteID uint) error {
	_, err := queue.client.Insert(ctx, BackfillSiteArgs{SiteID: siteID}, nil)
	return err
}

func (queue *riverQueue) EnqueueProcessCandidate(ctx context.Context, candidateID uint, contentVersion string) error {
	_, err := queue.client.Insert(ctx, ProcessCandidateArgs{CandidateID: candidateID, ContentVersion: contentVersion}, nil)
	return err
}

func (queue *riverQueue) EnqueueAssessArticle(ctx context.Context, candidateID uint, contentVersion string) error {
	_, err := queue.client.Insert(ctx, AssessArticleArgs{CandidateID: candidateID, ContentVersion: contentVersion}, nil)
	return err
}

func (queue *riverQueue) EnqueueGenerateDaily(ctx context.Context, userID uint, localDate string) error {
	_, err := queue.client.Insert(ctx, GenerateDailyArgs{UserID: userID, LocalDate: localDate}, nil)
	return err
}

func Start(ctx context.Context, database *gorm.DB, handlers Handlers, recover RecoveryFunc) (func(), error) {
	if database == nil || database.Dialector.Name() != "postgres" {
		queue := NewMemoryQueue(fallbackMemoryStore(database), handlers)
		restore := installDefault(queue, queue)
		if recover != nil {
			if err := recover(ctx, queue); err != nil {
				restore()
				return func() {}, err
			}
		}
		var once sync.Once
		return func() { once.Do(restore) }, nil
	}

	sqlDB, err := database.DB()
	if err != nil {
		return func() {}, err
	}
	workers := river.NewWorkers()
	registerWorkers(workers, handlers)
	assessmentWorkers := config.ARTICLEASSESSMENTCONCURRENCY
	if assessmentWorkers < 1 {
		assessmentWorkers = 2
	}
	client, err := river.NewClient(riverdatabasesql.New(sqlDB), &river.Config{
		Queues: map[string]river.QueueConfig{
			river.QueueDefault:  {MaxWorkers: 4},
			DiscoveryQueueName:  {MaxWorkers: 4},
			AssessmentQueueName: {MaxWorkers: assessmentWorkers},
		},
		Workers: workers,
	})
	if err != nil {
		return func() {}, err
	}
	now := time.Now()
	if _, err := client.Driver().GetExecutor().QueueCreateOrSetUpdatedAt(ctx, &riverdriver.QueueCreateOrSetUpdatedAtParams{
		Metadata: []byte("{}"), Name: DiscoveryQueueName, PausedAt: &now, UpdatedAt: &now,
	}); err != nil {
		return func() {}, err
	}
	if err := client.QueuePause(ctx, DiscoveryQueueName, nil); err != nil {
		return func() {}, err
	}
	reconciledJobs, err := reconcileManualDiscoveryJobs(ctx, sqlDB)
	if err != nil {
		return func() {}, fmt.Errorf("reconcile manual discovery jobs: %w", err)
	}
	if reconciledJobs > 0 {
		log.Printf("reconciled %d legacy or interrupted discovery jobs into paused queue %s", reconciledJobs, DiscoveryQueueName)
	}
	if err := client.Start(ctx); err != nil {
		return func() {}, err
	}
	queue := &riverQueue{client: client, stop: make(chan struct{})}
	restore := installDefault(queue, queue)
	if recover != nil {
		if err := recover(ctx, queue); err != nil {
			restore()
			stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			_ = client.Stop(stopCtx)
			return func() {}, err
		}
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			restore()
			close(queue.stop)
			stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := client.Stop(stopCtx); err != nil {
				log.Printf("shared River queue stop failed: %v", err)
			}
		})
	}, nil
}

func (queue *riverQueue) Snapshot(ctx context.Context, limit int) (*CrawlQueueSnapshot, error) {
	limit = normalizeSnapshotLimit(limit)
	result, err := queue.client.JobList(ctx, river.NewJobListParams().
		Kinds(FetchSourceJobKind, ScanBlogrollJobKind, BackfillSiteJobKind, ProcessCandidateJobKind).
		Queues(DiscoveryQueueName).
		OrderBy(river.JobListOrderByID, river.SortOrderDesc).
		First(10_000))
	if err != nil {
		return nil, err
	}

	queue.runMu.Lock()
	running := queue.running
	queue.runMu.Unlock()
	now := time.Now()
	cutoff := now.Add(-24 * time.Hour)
	snapshot := &CrawlQueueSnapshot{Mode: CrawlQueueMode, UpdatedAt: now, Tasks: make([]CrawlQueueTask, 0, limit)}
	for _, row := range result.Jobs {
		task := crawlTaskFromRiverRow(row)
		switch task.Status {
		case "pending":
			snapshot.Counts.Pending++
		case "running":
			snapshot.Counts.Running++
		case "succeeded":
			if task.FinishedAt != nil && !task.FinishedAt.Before(cutoff) {
				snapshot.Counts.Succeeded24h++
			}
		case "failed":
			if task.FinishedAt != nil && !task.FinishedAt.Before(cutoff) {
				snapshot.Counts.Failed24h++
			}
		}
		if len(snapshot.Tasks) < limit {
			snapshot.Tasks = append(snapshot.Tasks, task)
		}
	}
	snapshot.CanRun = !running && riverJobsRunnable(result.Jobs, now)
	switch {
	case running:
		snapshot.State = "running"
	case snapshot.Counts.Pending > 0:
		snapshot.State = "waiting"
	default:
		snapshot.State = "idle"
	}
	return snapshot, nil
}

func (queue *riverQueue) Run(ctx context.Context) (*CrawlQueueSnapshot, error) {
	queue.runMu.Lock()
	if queue.running {
		queue.runMu.Unlock()
		return queue.Snapshot(ctx, 50)
	}
	queue.running = true
	queue.runMu.Unlock()
	if err := queue.client.QueueResume(ctx, DiscoveryQueueName, nil); err != nil {
		queue.runMu.Lock()
		queue.running = false
		queue.runMu.Unlock()
		return nil, err
	}
	go queue.pauseWhenDrained()
	return queue.Snapshot(ctx, 50)
}

func (queue *riverQueue) pauseWhenDrained() {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	quietChecks := 0
	for {
		select {
		case <-queue.stop:
			return
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			result, err := queue.client.JobList(ctx, river.NewJobListParams().
				Kinds(FetchSourceJobKind, ScanBlogrollJobKind, BackfillSiteJobKind, ProcessCandidateJobKind).
				Queues(DiscoveryQueueName).
				States(rivertype.JobStateAvailable, rivertype.JobStatePending, rivertype.JobStateRunning, rivertype.JobStateRetryable, rivertype.JobStateScheduled).
				First(10_000))
			cancel()
			if err != nil || riverJobsRunnable(result.Jobs, time.Now()) {
				quietChecks = 0
				continue
			}
			quietChecks++
			if quietChecks < 2 {
				continue
			}
			pauseCtx, pauseCancel := context.WithTimeout(context.Background(), 5*time.Second)
			err = queue.client.QueuePause(pauseCtx, DiscoveryQueueName, nil)
			pauseCancel()
			if err != nil {
				log.Printf("manual discovery queue pause failed: %v", err)
				quietChecks = 0
				continue
			}
			queue.runMu.Lock()
			queue.running = false
			queue.runMu.Unlock()
			return
		}
	}
}

func riverJobsRunnable(rows []*rivertype.JobRow, now time.Time) bool {
	for _, row := range rows {
		switch row.State {
		case rivertype.JobStateAvailable, rivertype.JobStatePending, rivertype.JobStateRunning:
			return true
		case rivertype.JobStateRetryable, rivertype.JobStateScheduled:
			if !row.ScheduledAt.After(now) {
				return true
			}
		}
	}
	return false
}

func crawlTaskFromRiverRow(row *rivertype.JobRow) CrawlQueueTask {
	task := CrawlQueueTask{ID: fmt.Sprint(row.ID), Kind: row.Kind, Attempts: row.Attempt, CreatedAt: row.CreatedAt}
	if !row.ScheduledAt.IsZero() {
		scheduled := row.ScheduledAt
		task.ScheduledAt = &scheduled
	}
	task.StartedAt = row.AttemptedAt
	task.FinishedAt = row.FinalizedAt
	if len(row.Errors) > 0 {
		task.Error = compactQueueError(row.Errors[len(row.Errors)-1].Error)
	}
	switch row.State {
	case rivertype.JobStateRunning:
		task.Status = "running"
	case rivertype.JobStateCompleted:
		task.Status = "succeeded"
	case rivertype.JobStateCancelled, rivertype.JobStateDiscarded:
		task.Status = "failed"
	default:
		task.Status = "pending"
	}
	decodeSafeCrawlTarget(&task, row.EncodedArgs)
	return task
}

func fallbackMemoryStore(database *gorm.DB) *MemoryStore {
	fallbackMemoryStores.Lock()
	defer fallbackMemoryStores.Unlock()
	if database == nil {
		if fallbackMemoryStores.withoutDB == nil {
			fallbackMemoryStores.withoutDB = NewMemoryStore()
		}
		return fallbackMemoryStores.withoutDB
	}
	if fallbackMemoryStores.byDatabase == nil {
		fallbackMemoryStores.byDatabase = make(map[*gorm.DB]*MemoryStore)
	}
	store := fallbackMemoryStores.byDatabase[database]
	if store == nil {
		store = NewMemoryStore()
		fallbackMemoryStores.byDatabase[database] = store
	}
	return store
}

type fetchSourceWorker struct {
	river.WorkerDefaults[FetchSourceArgs]
	handler func(context.Context, uint) error
}

func (worker *fetchSourceWorker) Work(ctx context.Context, job *river.Job[FetchSourceArgs]) error {
	if worker.handler == nil {
		return fmt.Errorf("%w: %s", ErrHandlerUnavailable, FetchSourceJobKind)
	}
	err := worker.handler(ctx, job.Args.SourceID)
	logWorkerEvent("fetch_source", fmt.Sprint(job.ID), observability.Event{SourceID: job.Args.SourceID}, err)
	return err
}

type scanBlogrollWorker struct {
	river.WorkerDefaults[ScanBlogrollArgs]
	handler func(context.Context, uint) error
}

func (worker *scanBlogrollWorker) Work(ctx context.Context, job *river.Job[ScanBlogrollArgs]) error {
	if worker.handler == nil {
		return fmt.Errorf("%w: %s", ErrHandlerUnavailable, ScanBlogrollJobKind)
	}
	err := worker.handler(ctx, job.Args.SiteID)
	logWorkerEvent("scan_blogroll", fmt.Sprint(job.ID), observability.Event{SiteID: job.Args.SiteID}, err)
	return err
}

type backfillSiteWorker struct {
	river.WorkerDefaults[BackfillSiteArgs]
	handler func(context.Context, uint) error
}

func (worker *backfillSiteWorker) Work(ctx context.Context, job *river.Job[BackfillSiteArgs]) error {
	if worker.handler == nil {
		return fmt.Errorf("%w: %s", ErrHandlerUnavailable, BackfillSiteJobKind)
	}
	err := worker.handler(ctx, job.Args.SiteID)
	logWorkerEvent("backfill_site", fmt.Sprint(job.ID), observability.Event{SiteID: job.Args.SiteID}, err)
	return err
}

type processCandidateWorker struct {
	river.WorkerDefaults[ProcessCandidateArgs]
	handler func(context.Context, uint, string) error
}

func (worker *processCandidateWorker) Timeout(*river.Job[ProcessCandidateArgs]) time.Duration {
	return 2 * time.Minute
}

func (worker *processCandidateWorker) Work(ctx context.Context, job *river.Job[ProcessCandidateArgs]) error {
	if worker.handler == nil {
		return fmt.Errorf("%w: %s", ErrHandlerUnavailable, ProcessCandidateJobKind)
	}
	err := worker.handler(ctx, job.Args.CandidateID, job.Args.ContentVersion)
	logWorkerEvent("process_candidate", fmt.Sprint(job.ID), observability.Event{CandidateID: job.Args.CandidateID}, err)
	return err
}

type assessArticleWorker struct {
	river.WorkerDefaults[AssessArticleArgs]
	handler func(context.Context, uint, string) error
}

// Timeout 评估作业跟随 LLM_TIMEOUT，避免模型返回前被 River 默认一分钟限制取消。
func (worker *assessArticleWorker) Timeout(*river.Job[AssessArticleArgs]) time.Duration {
	return assessArticleJobTimeout()
}

func assessArticleJobTimeout() time.Duration {
	timeout, err := time.ParseDuration(strings.TrimSpace(config.LLMTIMEOUT))
	if err != nil || timeout <= 0 {
		return 30 * time.Second
	}
	return timeout
}

func (worker *assessArticleWorker) Work(ctx context.Context, job *river.Job[AssessArticleArgs]) error {
	if worker.handler == nil {
		return fmt.Errorf("%w: %s", ErrHandlerUnavailable, AssessArticleJobKind)
	}
	err := worker.handler(ctx, job.Args.CandidateID, job.Args.ContentVersion)
	logWorkerEvent("assess_article", fmt.Sprint(job.ID), observability.Event{CandidateID: job.Args.CandidateID}, err)
	return err
}

type generateDailyWorker struct {
	river.WorkerDefaults[GenerateDailyArgs]
	handler func(context.Context, uint, string) error
}

func (worker *generateDailyWorker) Work(ctx context.Context, job *river.Job[GenerateDailyArgs]) error {
	if worker.handler == nil {
		return fmt.Errorf("%w: %s", ErrHandlerUnavailable, GenerateDailyJobKind)
	}
	err := worker.handler(ctx, job.Args.UserID, job.Args.LocalDate)
	logWorkerEvent("generate_daily", fmt.Sprint(job.ID), observability.Event{UserID: job.Args.UserID, LocalDate: job.Args.LocalDate}, err)
	return err
}

func logWorkerEvent(name string, jobID string, event observability.Event, err error) {
	observability.Log(workerEvent(name, jobID, event, err))
}

func workerEvent(name string, jobID string, event observability.Event, err error) observability.Event {
	event.Name, event.JobID, event.Status = "job_"+name, jobID, "completed"
	if err != nil {
		event.Status = "failed"
		event = observability.WithError(event, err)
	}
	return event
}

func registerWorkers(workers *river.Workers, handlers Handlers) {
	river.AddWorker(workers, &fetchSourceWorker{handler: handlers.FetchSource})
	river.AddWorker(workers, &scanBlogrollWorker{handler: handlers.ScanBlogroll})
	river.AddWorker(workers, &backfillSiteWorker{handler: handlers.BackfillSite})
	river.AddWorker(workers, &processCandidateWorker{handler: handlers.ProcessCandidate})
	river.AddWorker(workers, &assessArticleWorker{handler: handlers.AssessArticle})
	river.AddWorker(workers, &generateDailyWorker{handler: handlers.GenerateDaily})
}
