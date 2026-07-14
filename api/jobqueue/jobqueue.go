package jobqueue

import (
	"DataArk/observability"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverdatabasesql"
	"gorm.io/gorm"
)

const (
	FetchSourceJobKind      = "discovery_fetch_source"
	ScanBlogrollJobKind     = "discovery_scan_blogroll"
	BackfillSiteJobKind     = "discovery_backfill_site"
	ProcessCandidateJobKind = "discovery_process_candidate"
	GenerateDailyJobKind    = "recommendation_generate_daily"
)

var ErrHandlerUnavailable = errors.New("job handler is not registered")

type JobEnqueuer interface {
	EnqueueFetchSource(context.Context, uint) error
	EnqueueScanBlogroll(context.Context, uint) error
	EnqueueBackfillSite(context.Context, uint) error
	EnqueueProcessCandidate(context.Context, uint, string) error
	EnqueueGenerateDaily(context.Context, uint, string) error
}

type Handlers struct {
	FetchSource      func(context.Context, uint) error
	ScanBlogroll     func(context.Context, uint) error
	BackfillSite     func(context.Context, uint) error
	ProcessCandidate func(context.Context, uint, string) error
	GenerateDaily    func(context.Context, uint, string) error
}

type RecoveryFunc func(context.Context, JobEnqueuer) error

type FetchSourceArgs struct {
	SourceID uint `json:"source_id"`
}

func (FetchSourceArgs) Kind() string { return FetchSourceJobKind }
func (FetchSourceArgs) InsertOpts() river.InsertOpts {
	return uniqueByArgsOpts()
}

type ScanBlogrollArgs struct {
	SiteID uint `json:"site_id"`
}

func (ScanBlogrollArgs) Kind() string { return ScanBlogrollJobKind }
func (ScanBlogrollArgs) InsertOpts() river.InsertOpts {
	return uniqueByArgsOpts()
}

type BackfillSiteArgs struct {
	SiteID uint `json:"site_id"`
}

func (BackfillSiteArgs) Kind() string { return BackfillSiteJobKind }
func (BackfillSiteArgs) InsertOpts() river.InsertOpts {
	return uniqueByArgsOpts()
}

type ProcessCandidateArgs struct {
	CandidateID    uint   `json:"candidate_id"`
	ContentVersion string `json:"content_version"`
}

func (ProcessCandidateArgs) Kind() string { return ProcessCandidateJobKind }
func (ProcessCandidateArgs) InsertOpts() river.InsertOpts {
	return uniqueByArgsOpts()
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

func uniqueByArgsOpts() river.InsertOpts {
	return river.InsertOpts{UniqueOpts: river.UniqueOpts{ByArgs: true}}
}

type defaultQueueEntry struct {
	id    uint64
	queue JobEnqueuer
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

func installDefault(queue JobEnqueuer) func() {
	id := defaultQueueSequence.Add(1)
	defaultQueue.Lock()
	previous := defaultQueue.entry
	defaultQueue.entry = defaultQueueEntry{id: id, queue: queue}
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
	client *river.Client[*sql.Tx]
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

func (queue *riverQueue) EnqueueGenerateDaily(ctx context.Context, userID uint, localDate string) error {
	_, err := queue.client.Insert(ctx, GenerateDailyArgs{UserID: userID, LocalDate: localDate}, nil)
	return err
}

func Start(ctx context.Context, database *gorm.DB, handlers Handlers, recover RecoveryFunc) (func(), error) {
	if database == nil || database.Dialector.Name() != "postgres" {
		queue := NewMemoryQueue(fallbackMemoryStore(database), handlers)
		restore := installDefault(queue)
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
	client, err := river.NewClient(riverdatabasesql.New(sqlDB), &river.Config{
		Queues: map[string]river.QueueConfig{
			river.QueueDefault: {MaxWorkers: 4},
		},
		Workers: workers,
	})
	if err != nil {
		return func() {}, err
	}
	if err := client.Start(ctx); err != nil {
		return func() {}, err
	}
	queue := &riverQueue{client: client}
	restore := installDefault(queue)
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
			stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := client.Stop(stopCtx); err != nil {
				log.Printf("shared River queue stop failed: %v", err)
			}
		})
	}, nil
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

func (worker *processCandidateWorker) Work(ctx context.Context, job *river.Job[ProcessCandidateArgs]) error {
	if worker.handler == nil {
		return fmt.Errorf("%w: %s", ErrHandlerUnavailable, ProcessCandidateJobKind)
	}
	err := worker.handler(ctx, job.Args.CandidateID, job.Args.ContentVersion)
	logWorkerEvent("process_candidate", fmt.Sprint(job.ID), observability.Event{CandidateID: job.Args.CandidateID}, err)
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
	event.Name, event.JobID, event.Status = "job_"+name, jobID, "completed"
	if err != nil {
		event.Status, event.ErrorType = "failed", "handler"
	}
	observability.Log(event)
}

func registerWorkers(workers *river.Workers, handlers Handlers) {
	river.AddWorker(workers, &fetchSourceWorker{handler: handlers.FetchSource})
	river.AddWorker(workers, &scanBlogrollWorker{handler: handlers.ScanBlogroll})
	river.AddWorker(workers, &backfillSiteWorker{handler: handlers.BackfillSite})
	river.AddWorker(workers, &processCandidateWorker{handler: handlers.ProcessCandidate})
	river.AddWorker(workers, &generateDailyWorker{handler: handlers.GenerateDaily})
}
