package jobqueue

import (
	"context"
	"fmt"
	"sync"
	"time"
)

const (
	MemoryJobPending   = "pending"
	MemoryJobRunning   = "running"
	MemoryJobCompleted = "completed"
	MemoryJobFailed    = "failed"
)

type MemoryJob struct {
	Key       string
	Kind      string
	Status    string
	Attempts  int
	LastError string
	UpdatedAt time.Time
}

type MemoryStore struct {
	mu   sync.Mutex
	jobs map[string]MemoryJob
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{jobs: make(map[string]MemoryJob)}
}

func (store *MemoryStore) Snapshot() []MemoryJob {
	store.mu.Lock()
	defer store.mu.Unlock()
	result := make([]MemoryJob, 0, len(store.jobs))
	for _, job := range store.jobs {
		result = append(result, job)
	}
	return result
}

type MemoryQueue struct {
	store    *MemoryStore
	handlers Handlers
}

func NewMemoryQueue(store *MemoryStore, handlers Handlers) *MemoryQueue {
	if store == nil {
		store = NewMemoryStore()
	}
	store.mu.Lock()
	for key, job := range store.jobs {
		if job.Status == MemoryJobRunning {
			job.Status = MemoryJobFailed
			job.LastError = "interrupted before completion"
			job.UpdatedAt = time.Now()
			store.jobs[key] = job
		}
	}
	store.mu.Unlock()
	return &MemoryQueue{store: store, handlers: handlers}
}

func (queue *MemoryQueue) EnqueueFetchSource(ctx context.Context, sourceID uint) error {
	return queue.run(ctx, FetchSourceJobKind, fmt.Sprintf("source:%d", sourceID), func() error {
		if queue.handlers.FetchSource == nil {
			return fmt.Errorf("%w: %s", ErrHandlerUnavailable, FetchSourceJobKind)
		}
		return queue.handlers.FetchSource(ctx, sourceID)
	})
}

func (queue *MemoryQueue) EnqueueScanBlogroll(ctx context.Context, siteID uint) error {
	return queue.run(ctx, ScanBlogrollJobKind, fmt.Sprintf("site:%d", siteID), func() error {
		if queue.handlers.ScanBlogroll == nil {
			return fmt.Errorf("%w: %s", ErrHandlerUnavailable, ScanBlogrollJobKind)
		}
		return queue.handlers.ScanBlogroll(ctx, siteID)
	})
}

func (queue *MemoryQueue) EnqueueBackfillSite(ctx context.Context, siteID uint) error {
	return queue.run(ctx, BackfillSiteJobKind, fmt.Sprintf("site:%d", siteID), func() error {
		if queue.handlers.BackfillSite == nil {
			return fmt.Errorf("%w: %s", ErrHandlerUnavailable, BackfillSiteJobKind)
		}
		return queue.handlers.BackfillSite(ctx, siteID)
	})
}

func (queue *MemoryQueue) EnqueueProcessCandidate(ctx context.Context, candidateID uint, contentVersion string) error {
	return queue.run(ctx, ProcessCandidateJobKind, fmt.Sprintf("candidate:%d:version:%s", candidateID, contentVersion), func() error {
		if queue.handlers.ProcessCandidate == nil {
			return fmt.Errorf("%w: %s", ErrHandlerUnavailable, ProcessCandidateJobKind)
		}
		return queue.handlers.ProcessCandidate(ctx, candidateID, contentVersion)
	})
}

func (queue *MemoryQueue) EnqueueGenerateDaily(ctx context.Context, userID uint, localDate string) error {
	return queue.run(ctx, GenerateDailyJobKind, fmt.Sprintf("user:%d:date:%s", userID, localDate), func() error {
		if queue.handlers.GenerateDaily == nil {
			return fmt.Errorf("%w: %s", ErrHandlerUnavailable, GenerateDailyJobKind)
		}
		return queue.handlers.GenerateDaily(ctx, userID, localDate)
	})
}

func (queue *MemoryQueue) run(ctx context.Context, kind string, identity string, execute func() error) error {
	key := kind + ":" + identity
	queue.store.mu.Lock()
	existing, exists := queue.store.jobs[key]
	if exists && (existing.Status == MemoryJobPending || existing.Status == MemoryJobRunning || existing.Status == MemoryJobCompleted) {
		queue.store.mu.Unlock()
		return nil
	}
	job := existing
	job.Key = key
	job.Kind = kind
	job.Status = MemoryJobRunning
	job.Attempts++
	job.LastError = ""
	job.UpdatedAt = time.Now()
	queue.store.jobs[key] = job
	queue.store.mu.Unlock()

	if err := ctx.Err(); err != nil {
		queue.finish(key, MemoryJobFailed, err)
		return err
	}
	err := execute()
	if err != nil {
		queue.finish(key, MemoryJobFailed, err)
		return err
	}
	queue.finish(key, MemoryJobCompleted, nil)
	return nil
}

func (queue *MemoryQueue) finish(key string, status string, err error) {
	queue.store.mu.Lock()
	defer queue.store.mu.Unlock()
	job := queue.store.jobs[key]
	job.Status = status
	job.UpdatedAt = time.Now()
	if err != nil {
		job.LastError = err.Error()
	}
	queue.store.jobs[key] = job
}
