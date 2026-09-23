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
	FetchSourceJobKind           = "discovery_fetch_source"
	ScanBlogrollJobKind          = "discovery_scan_blogroll"
	BackfillSiteJobKind          = "discovery_backfill_site"
	ProcessCandidateJobKind      = "discovery_process_candidate"
	AssessArticleJobKind         = "assessment_assess_article"
	GenerateDailyJobKind         = "recommendation_generate_daily"
	GenerateDigestSummaryJobKind = "recommendation_generate_digest_summary"
)

var ErrHandlerUnavailable = errors.New("job handler is not registered")

type JobEnqueuer interface {
	EnqueueFetchSource(context.Context, uint) error
	EnqueueScanBlogroll(context.Context, uint) error
	EnqueueBackfillSite(context.Context, uint) error
	EnqueueProcessCandidate(context.Context, uint, string) error
	EnqueueAssessArticle(context.Context, uint, string) error
	EnqueueGenerateDaily(context.Context, uint, string) error
	EnqueueGenerateDigestSummary(context.Context, uint, string) error
}

type Handlers struct {
	FetchSource           func(context.Context, uint) error
	ScanBlogroll          func(context.Context, uint) error
	BackfillSite          func(context.Context, uint) error
	ProcessCandidate      func(context.Context, uint, string) error
	AssessArticle         func(context.Context, uint, string) error
	AssessLegacyCandidate func(context.Context, uint, string) error
	GenerateDaily         func(context.Context, uint, string) error
	GenerateDigestSummary func(context.Context, uint, string) error
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

// GenerateDigestSummaryArgs 按用户本地日期预生成日报摘要；补文后需能再次入队，因此不加 24h 周期唯一。
type GenerateDigestSummaryArgs struct {
	UserID    uint   `json:"user_id"`
	LocalDate string `json:"local_date"`
}

func (GenerateDigestSummaryArgs) Kind() string { return GenerateDigestSummaryJobKind }
func (GenerateDigestSummaryArgs) InsertOpts() river.InsertOpts {
	opts := uniqueByArgsOpts()
	opts.MaxAttempts = 1
	return opts
}

type AssessArticleArgs struct {
	MaterialID     uint   `json:"material_id,omitempty"`
	CandidateID    uint   `json:"candidate_id"`
	ContentVersion string `json:"content_version"`
}

func (AssessArticleArgs) Kind() string { return AssessArticleJobKind }
func (AssessArticleArgs) InsertOpts() river.InsertOpts {
	opts := uniqueByArgsOpts()
	opts.Queue = AssessmentQueueName
	// 评估队列默认暂停，失败后由 owner 再次手动执行，River 不得自行重试。
	opts.MaxAttempts = 1
	return opts
}

func uniqueByArgsOpts() river.InsertOpts {
	return river.InsertOpts{UniqueOpts: river.UniqueOpts{ByArgs: true}}
}

func crawlUniqueByArgsOpts() river.InsertOpts {
	opts := uniqueByArgsOpts()
	opts.Queue = DiscoveryQueueName
	// 发现处理器自己维护退避；River 不得对失败的爬取作业自行重试。
	opts.MaxAttempts = 1
	return opts
}

type defaultQueueEntry struct {
	id         uint64
	queue      JobEnqueuer
	crawl      CrawlQueueController
	assessment AssessmentQueueController
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

func installDefault(queue JobEnqueuer, crawl CrawlQueueController, assessment AssessmentQueueController) func() {
	id := defaultQueueSequence.Add(1)
	defaultQueue.Lock()
	previous := defaultQueue.entry
	defaultQueue.entry = defaultQueueEntry{id: id, queue: queue, crawl: crawl, assessment: assessment}
	defaultQueue.Unlock()
	return func() {
		defaultQueue.Lock()
		defer defaultQueue.Unlock()
		if defaultQueue.entry.id == id {
			defaultQueue.entry = previous
		}
	}
}

var (
	discoveryJobKinds  = []string{FetchSourceJobKind, ScanBlogrollJobKind, BackfillSiteJobKind, ProcessCandidateJobKind}
	assessmentJobKinds = []string{AssessArticleJobKind}
)

type riverQueue struct {
	client       *river.Client[*sql.Tx]
	db           *sql.DB
	runMu        sync.Mutex
	running      bool
	runStartedAt time.Time
	stop         chan struct{}
}

// riverQueueCountResult 是 river_job 全表计数，不受 JobList 1 万条上限影响。
type riverQueueCountResult struct {
	Counts   CrawlQueueCounts
	Runnable int
}

// riverCountRow 抽象 QueryRow.Scan，便于测试注入假计数。
type riverCountRow interface {
	Scan(dest ...any) error
}

// contextSQLQuerier 查询 river_job 聚合计数；生产走 *sql.DB，测试可注入。
type contextSQLQuerier interface {
	QueryRowContext(context.Context, string, ...any) riverCountRow
}

type stdSQLQuerier struct {
	db *sql.DB
}

func (querier stdSQLQuerier) QueryRowContext(ctx context.Context, query string, args ...any) riverCountRow {
	return querier.db.QueryRowContext(ctx, query, args...)
}

type riverCrawlView struct {
	queue *riverQueue
}

func (view riverCrawlView) Snapshot(ctx context.Context, limit int) (*CrawlQueueSnapshot, error) {
	return view.queue.crawlSnapshot(ctx, limit)
}

type riverAssessmentView struct {
	queue *riverQueue
}

func (view riverAssessmentView) Snapshot(ctx context.Context, limit int) (*CrawlQueueSnapshot, error) {
	return view.queue.assessmentSnapshot(ctx, limit)
}

func (view riverAssessmentView) Run(ctx context.Context) (*CrawlQueueSnapshot, error) {
	return view.queue.assessmentRun(ctx)
}

type contextSQLExecer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

// reconcileManualDiscoveryJobs 把误放进默认队列的发现作业迁回 discovery_crawl，
// 并把上一进程中断的 running 作业改回 available，供自动工人继续消费。
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

// reconcileManualAssessmentJobs 把评估作业固定到暂停的 article_assessment 队列，
// 避免上一版本自动工人遗留的 running 作业在启动瞬间继续调用 LLM。
func reconcileManualAssessmentJobs(ctx context.Context, executor contextSQLExecer) (int64, error) {
	result, err := executor.ExecContext(ctx, `
UPDATE river_job
SET queue = $1,
	state = CASE WHEN state = 'running' THEN 'available'::river_job_state ELSE state END,
	attempted_at = CASE WHEN state = 'running' THEN NULL ELSE attempted_at END,
	attempted_by = CASE WHEN state = 'running' THEN NULL ELSE attempted_by END,
	scheduled_at = CASE WHEN state = 'running' THEN now() ELSE scheduled_at END,
	max_attempts = GREATEST(attempt, 1)
WHERE kind = $2
  AND (
	(queue <> $1 AND state IN ('available', 'pending', 'retryable', 'running', 'scheduled'))
	OR (queue = $1 AND state = 'running')
  )`,
		AssessmentQueueName,
		AssessArticleJobKind,
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

func (queue *riverQueue) EnqueueAssessArticle(ctx context.Context, materialID uint, contentVersion string) error {
	_, err := queue.client.Insert(ctx, AssessArticleArgs{MaterialID: materialID, ContentVersion: contentVersion}, nil)
	return err
}

func (queue *riverQueue) EnqueueGenerateDaily(ctx context.Context, userID uint, localDate string) error {
	_, err := queue.client.Insert(ctx, GenerateDailyArgs{UserID: userID, LocalDate: localDate}, nil)
	return err
}

func (queue *riverQueue) EnqueueGenerateDigestSummary(ctx context.Context, userID uint, localDate string) error {
	_, err := queue.client.Insert(ctx, GenerateDigestSummaryArgs{UserID: userID, LocalDate: localDate}, nil)
	return err
}

func Start(ctx context.Context, database *gorm.DB, handlers Handlers, recover RecoveryFunc) (func(), error) {
	if database == nil || database.Dialector.Name() != "postgres" {
		queue := NewMemoryQueue(fallbackMemoryStore(database), handlers)
		restore := installDefault(queue, memoryCrawlView{queue: queue}, memoryAssessmentView{queue: queue})
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
		Metadata: []byte("{}"), Name: DiscoveryQueueName, UpdatedAt: &now,
	}); err != nil {
		return func() {}, err
	}
	if _, err := client.Driver().GetExecutor().QueueCreateOrSetUpdatedAt(ctx, &riverdriver.QueueCreateOrSetUpdatedAtParams{
		Metadata: []byte("{}"), Name: AssessmentQueueName, PausedAt: &now, UpdatedAt: &now,
	}); err != nil {
		return func() {}, err
	}
	// 升级后若发现队列仍处于上一版本的暂停状态，则恢复自动消费。
	if err := client.QueueResume(ctx, DiscoveryQueueName, nil); err != nil {
		return func() {}, err
	}
	if err := client.QueuePause(ctx, AssessmentQueueName, nil); err != nil {
		return func() {}, err
	}
	reconciledJobs, err := reconcileManualDiscoveryJobs(ctx, sqlDB)
	if err != nil {
		return func() {}, fmt.Errorf("reconcile discovery jobs: %w", err)
	}
	if reconciledJobs > 0 {
		log.Printf("reconciled %d legacy or interrupted discovery jobs into automatic queue %s", reconciledJobs, DiscoveryQueueName)
	}
	reconciledAssessments, err := reconcileManualAssessmentJobs(ctx, sqlDB)
	if err != nil {
		return func() {}, fmt.Errorf("reconcile assessment jobs: %w", err)
	}
	if reconciledAssessments > 0 {
		log.Printf("reconciled %d assessment jobs into paused queue %s", reconciledAssessments, AssessmentQueueName)
	}
	if err := client.Start(ctx); err != nil {
		return func() {}, err
	}
	queue := &riverQueue{client: client, db: sqlDB, stop: make(chan struct{})}
	restore := installDefault(queue, riverCrawlView{queue: queue}, riverAssessmentView{queue: queue})
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

func (queue *riverQueue) crawlSnapshot(ctx context.Context, limit int) (*CrawlQueueSnapshot, error) {
	limit = normalizeSnapshotLimit(limit)
	now := time.Now()
	counted, err := countRiverQueueJobs(ctx, stdSQLQuerier{db: queue.db}, DiscoveryQueueName, discoveryJobKinds, now.Add(-24*time.Hour))
	if err != nil {
		return nil, err
	}
	result, err := queue.client.JobList(ctx, river.NewJobListParams().
		Kinds(discoveryJobKinds...).
		Queues(DiscoveryQueueName).
		OrderBy(river.JobListOrderByID, river.SortOrderDesc).
		First(limit))
	if err != nil {
		return nil, err
	}
	return buildQueueSnapshot(result.Jobs, limit, CrawlQueueMode, false, now, counted.Counts), nil
}

func (queue *riverQueue) assessmentSnapshot(ctx context.Context, limit int) (*CrawlQueueSnapshot, error) {
	limit = normalizeSnapshotLimit(limit)
	queue.runMu.Lock()
	running := queue.running
	startedAt := queue.runStartedAt
	queue.runMu.Unlock()
	now := time.Now()
	cutoff := now
	if running && !startedAt.IsZero() {
		cutoff = startedAt
	}
	counted, err := countRiverQueueJobs(ctx, stdSQLQuerier{db: queue.db}, AssessmentQueueName, assessmentJobKinds, cutoff)
	if err != nil {
		return nil, err
	}
	result, err := queue.client.JobList(ctx, river.NewJobListParams().
		Kinds(assessmentJobKinds...).
		Queues(AssessmentQueueName).
		OrderBy(river.JobListOrderByID, river.SortOrderDesc).
		First(limit))
	if err != nil {
		return nil, err
	}
	snapshot := buildQueueSnapshot(result.Jobs, limit, AssessmentQueueMode, running, now, counted.Counts)
	snapshot.CanRun = !running && counted.Runnable > 0
	if running {
		snapshot.State = "running"
		if !startedAt.IsZero() {
			started := startedAt
			snapshot.RunStartedAt = &started
		}
	}
	return snapshot, nil
}

func (queue *riverQueue) assessmentRun(ctx context.Context) (*CrawlQueueSnapshot, error) {
	queue.runMu.Lock()
	if queue.running {
		queue.runMu.Unlock()
		return queue.assessmentSnapshot(ctx, 50)
	}
	queue.running = true
	queue.runStartedAt = time.Now()
	queue.runMu.Unlock()
	if err := queue.client.QueueResume(ctx, AssessmentQueueName, nil); err != nil {
		queue.clearAssessmentRun()
		return nil, err
	}
	go queue.pauseAssessmentWhenDrained()
	return queue.assessmentSnapshot(ctx, 50)
}

func (queue *riverQueue) clearAssessmentRun() {
	queue.runMu.Lock()
	queue.running = false
	queue.runStartedAt = time.Time{}
	queue.runMu.Unlock()
}

func (queue *riverQueue) pauseAssessmentWhenDrained() {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	quietChecks := 0
	for {
		select {
		case <-queue.stop:
			return
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			counted, err := countRiverQueueJobs(ctx, stdSQLQuerier{db: queue.db}, AssessmentQueueName, assessmentJobKinds, time.Now())
			cancel()
			if err != nil || counted.Runnable > 0 {
				quietChecks = 0
				continue
			}
			quietChecks++
			if quietChecks < 2 {
				continue
			}
			pauseCtx, pauseCancel := context.WithTimeout(context.Background(), 5*time.Second)
			err = queue.client.QueuePause(pauseCtx, AssessmentQueueName, nil)
			pauseCancel()
			if err != nil {
				log.Printf("manual assessment queue pause failed: %v", err)
				quietChecks = 0
				continue
			}
			queue.clearAssessmentRun()
			return
		}
	}
}

// buildQueueSnapshot 只用 JobList 行填充任务预览；pending/成功/失败来自 SQL COUNT。
func buildQueueSnapshot(rows []*rivertype.JobRow, limit int, mode string, forceRunning bool, now time.Time, counts CrawlQueueCounts) *CrawlQueueSnapshot {
	snapshot := &CrawlQueueSnapshot{Mode: mode, UpdatedAt: now, Counts: counts, Tasks: make([]CrawlQueueTask, 0, limit)}
	for _, row := range rows {
		if len(snapshot.Tasks) >= limit {
			break
		}
		snapshot.Tasks = append(snapshot.Tasks, crawlTaskFromRiverRow(row))
	}
	switch {
	case forceRunning || snapshot.Counts.Running > 0:
		snapshot.State = "running"
	case snapshot.Counts.Pending > 0:
		snapshot.State = "waiting"
	default:
		snapshot.State = "idle"
	}
	return snapshot
}

// countRiverQueueJobs 按队列与 kind 对 river_job 做全表 COUNT，避免 JobList 1 万条抽样截断。
func countRiverQueueJobs(ctx context.Context, querier contextSQLQuerier, queueName string, kinds []string, cutoff time.Time) (riverQueueCountResult, error) {
	if querier == nil {
		return riverQueueCountResult{}, errors.New("river job counter is unavailable")
	}
	if len(kinds) == 0 {
		return riverQueueCountResult{}, errors.New("river job count requires at least one kind")
	}
	row := querier.QueryRowContext(ctx, riverQueueCountQuery(len(kinds)), riverQueueCountArgs(queueName, cutoff, kinds)...)
	if row == nil {
		return riverQueueCountResult{}, errors.New("river job count returned no row")
	}
	var pending, running, succeeded, failed, runnable int64
	if err := row.Scan(&pending, &running, &succeeded, &failed, &runnable); err != nil {
		return riverQueueCountResult{}, err
	}
	return riverQueueCountResult{
		Counts: CrawlQueueCounts{
			Pending:      int(pending),
			Running:      int(running),
			Succeeded24h: int(succeeded),
			Failed24h:    int(failed),
		},
		Runnable: int(runnable),
	}, nil
}

// riverQueueCountQuery 生成按 state 过滤的聚合 SQL；$1 队列，$2 cutoff，$3 起为 kind。
func riverQueueCountQuery(kindCount int) string {
	placeholders := make([]string, kindCount)
	for index := range placeholders {
		placeholders[index] = fmt.Sprintf("$%d", index+3)
	}
	return `
SELECT
  COUNT(*) FILTER (WHERE state IN ('available', 'pending', 'retryable', 'scheduled')),
  COUNT(*) FILTER (WHERE state = 'running'),
  COUNT(*) FILTER (WHERE state = 'completed' AND finalized_at >= $2),
  COUNT(*) FILTER (WHERE state IN ('cancelled', 'discarded') AND finalized_at >= $2),
  COUNT(*) FILTER (
    WHERE state IN ('available', 'pending', 'running')
       OR (state IN ('retryable', 'scheduled') AND scheduled_at <= now())
  )
FROM river_job
WHERE queue = $1 AND kind IN (` + strings.Join(placeholders, ", ") + `)`
}

func riverQueueCountArgs(queueName string, cutoff time.Time, kinds []string) []any {
	args := make([]any, 0, 2+len(kinds))
	args = append(args, queueName, cutoff)
	for _, kind := range kinds {
		args = append(args, kind)
	}
	return args
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
	handler       func(context.Context, uint, string) error
	legacyHandler func(context.Context, uint, string) error
}

// Timeout 评估作业跟随 LLM_TIMEOUT，避免模型返回前被 River 默认一分钟限制取消。
func (worker *assessArticleWorker) Timeout(*river.Job[AssessArticleArgs]) time.Duration {
	return llmJobTimeout()
}

func llmJobTimeout() time.Duration {
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
	id, handler := job.Args.MaterialID, worker.handler
	if id == 0 {
		id = job.Args.CandidateID
		if worker.legacyHandler != nil {
			handler = worker.legacyHandler
		}
	}
	err := handler(ctx, id, job.Args.ContentVersion)
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

type generateDigestSummaryWorker struct {
	river.WorkerDefaults[GenerateDigestSummaryArgs]
	handler func(context.Context, uint, string) error
}

// Timeout 摘要作业跟随 LLM_TIMEOUT，避免模型返回前被 River 默认一分钟限制取消。
func (worker *generateDigestSummaryWorker) Timeout(*river.Job[GenerateDigestSummaryArgs]) time.Duration {
	return llmJobTimeout()
}

func (worker *generateDigestSummaryWorker) Work(ctx context.Context, job *river.Job[GenerateDigestSummaryArgs]) error {
	if worker.handler == nil {
		return fmt.Errorf("%w: %s", ErrHandlerUnavailable, GenerateDigestSummaryJobKind)
	}
	err := worker.handler(ctx, job.Args.UserID, job.Args.LocalDate)
	logWorkerEvent("generate_digest_summary", fmt.Sprint(job.ID), observability.Event{UserID: job.Args.UserID, LocalDate: job.Args.LocalDate}, err)
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
	river.AddWorker(workers, &assessArticleWorker{handler: handlers.AssessArticle, legacyHandler: handlers.AssessLegacyCandidate})
	river.AddWorker(workers, &generateDailyWorker{handler: handlers.GenerateDaily})
	river.AddWorker(workers, &generateDigestSummaryWorker{handler: handlers.GenerateDigestSummary})
}
