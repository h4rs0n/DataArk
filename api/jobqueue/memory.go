package jobqueue

import (
	"context"
	"fmt"
	"sort"
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
	Key            string
	Kind           string
	TargetType     string
	TargetID       uint
	ContentVersion string
	Status         string
	Attempts       int
	LastError      string
	CreatedAt      time.Time
	StartedAt      *time.Time
	FinishedAt     *time.Time
	UpdatedAt      time.Time
}

type MemoryStore struct {
	mu      sync.Mutex
	jobs    map[string]MemoryJob
	running bool
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
	store.running = false
	for key, job := range store.jobs {
		if job.Status == MemoryJobRunning {
			job.Status = MemoryJobPending
			job.LastError = "interrupted before completion"
			job.StartedAt = nil
			job.FinishedAt = nil
			job.UpdatedAt = time.Now()
			store.jobs[key] = job
		}
	}
	store.mu.Unlock()
	return &MemoryQueue{store: store, handlers: handlers}
}

func (queue *MemoryQueue) EnqueueFetchSource(_ context.Context, sourceID uint) error {
	return queue.stage(FetchSourceJobKind, fmt.Sprintf("source:%d", sourceID), "source", sourceID, "")
}

func (queue *MemoryQueue) EnqueueScanBlogroll(_ context.Context, siteID uint) error {
	return queue.stage(ScanBlogrollJobKind, fmt.Sprintf("site:%d", siteID), "site", siteID, "")
}

func (queue *MemoryQueue) EnqueueBackfillSite(_ context.Context, siteID uint) error {
	return queue.stage(BackfillSiteJobKind, fmt.Sprintf("site:%d", siteID), "site", siteID, "")
}

func (queue *MemoryQueue) EnqueueProcessCandidate(_ context.Context, candidateID uint, contentVersion string) error {
	return queue.stage(ProcessCandidateJobKind, fmt.Sprintf("candidate:%d:version:%s", candidateID, contentVersion), "candidate", candidateID, contentVersion)
}

func (queue *MemoryQueue) EnqueueGenerateDaily(ctx context.Context, userID uint, localDate string) error {
	if queue.handlers.GenerateDaily == nil {
		return fmt.Errorf("%w: %s", ErrHandlerUnavailable, GenerateDailyJobKind)
	}
	return queue.handlers.GenerateDaily(ctx, userID, localDate)
}

func (queue *MemoryQueue) stage(kind, identity, targetType string, targetID uint, contentVersion string) error {
	key := kind + ":" + identity
	now := time.Now()
	queue.store.mu.Lock()
	defer queue.store.mu.Unlock()
	existing, exists := queue.store.jobs[key]
	if exists && (existing.Status == MemoryJobPending || existing.Status == MemoryJobRunning) {
		return nil
	}
	queue.store.jobs[key] = MemoryJob{
		Key: key, Kind: kind, TargetType: targetType, TargetID: targetID, ContentVersion: contentVersion,
		Status: MemoryJobPending, CreatedAt: now, UpdatedAt: now,
	}
	return nil
}

func (queue *MemoryQueue) Run(ctx context.Context) (*CrawlQueueSnapshot, error) {
	queue.store.mu.Lock()
	if !queue.store.running {
		queue.store.running = true
		go queue.drain()
	}
	queue.store.mu.Unlock()
	return queue.Snapshot(ctx, 50)
}

func (queue *MemoryQueue) drain() {
	for {
		jobs := queue.takePending(4)
		if len(jobs) == 0 {
			time.Sleep(500 * time.Millisecond)
			jobs = queue.takePending(4)
			if len(jobs) == 0 {
				queue.store.mu.Lock()
				queue.store.running = false
				queue.store.mu.Unlock()
				return
			}
		}
		var group sync.WaitGroup
		for _, job := range jobs {
			group.Add(1)
			go func(item MemoryJob) {
				defer group.Done()
				queue.execute(item)
			}(job)
		}
		group.Wait()
	}
}

func (queue *MemoryQueue) takePending(limit int) []MemoryJob {
	queue.store.mu.Lock()
	defer queue.store.mu.Unlock()
	keys := make([]string, 0)
	for key, job := range queue.store.jobs {
		if isCrawlJobKind(job.Kind) && job.Status == MemoryJobPending {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	if len(keys) > limit {
		keys = keys[:limit]
	}
	now := time.Now()
	result := make([]MemoryJob, 0, len(keys))
	for _, key := range keys {
		job := queue.store.jobs[key]
		job.Status = MemoryJobRunning
		job.Attempts++
		job.LastError = ""
		job.StartedAt = &now
		job.FinishedAt = nil
		job.UpdatedAt = now
		queue.store.jobs[key] = job
		result = append(result, job)
	}
	return result
}

func (queue *MemoryQueue) execute(job MemoryJob) {
	ctx := context.Background()
	var err error
	switch job.Kind {
	case FetchSourceJobKind:
		if queue.handlers.FetchSource == nil {
			err = fmt.Errorf("%w: %s", ErrHandlerUnavailable, job.Kind)
		} else {
			err = queue.handlers.FetchSource(ctx, job.TargetID)
		}
	case ScanBlogrollJobKind:
		if queue.handlers.ScanBlogroll == nil {
			err = fmt.Errorf("%w: %s", ErrHandlerUnavailable, job.Kind)
		} else {
			err = queue.handlers.ScanBlogroll(ctx, job.TargetID)
		}
	case BackfillSiteJobKind:
		if queue.handlers.BackfillSite == nil {
			err = fmt.Errorf("%w: %s", ErrHandlerUnavailable, job.Kind)
		} else {
			err = queue.handlers.BackfillSite(ctx, job.TargetID)
		}
	case ProcessCandidateJobKind:
		if queue.handlers.ProcessCandidate == nil {
			err = fmt.Errorf("%w: %s", ErrHandlerUnavailable, job.Kind)
		} else {
			err = queue.handlers.ProcessCandidate(ctx, job.TargetID, job.ContentVersion)
		}
	}
	queue.finish(job.Key, err)
}

func (queue *MemoryQueue) finish(key string, err error) {
	queue.store.mu.Lock()
	defer queue.store.mu.Unlock()
	job := queue.store.jobs[key]
	now := time.Now()
	job.Status = MemoryJobCompleted
	if err != nil {
		job.Status = MemoryJobFailed
		job.LastError = compactQueueError(err.Error())
	}
	job.FinishedAt = &now
	job.UpdatedAt = now
	queue.store.jobs[key] = job
}

func (queue *MemoryQueue) Snapshot(_ context.Context, limit int) (*CrawlQueueSnapshot, error) {
	limit = normalizeSnapshotLimit(limit)
	queue.store.mu.Lock()
	running := queue.store.running
	jobs := make([]MemoryJob, 0, len(queue.store.jobs))
	for _, job := range queue.store.jobs {
		if isCrawlJobKind(job.Kind) {
			jobs = append(jobs, job)
		}
	}
	queue.store.mu.Unlock()
	sort.Slice(jobs, func(i, j int) bool { return jobs[i].UpdatedAt.After(jobs[j].UpdatedAt) })
	now := time.Now()
	cutoff := now.Add(-24 * time.Hour)
	snapshot := &CrawlQueueSnapshot{Mode: CrawlQueueMode, UpdatedAt: now, Tasks: make([]CrawlQueueTask, 0, limit)}
	for _, job := range jobs {
		task := CrawlQueueTask{
			ID: job.Key, Kind: job.Kind, TargetType: job.TargetType, TargetID: job.TargetID,
			ContentVersion: job.ContentVersion, Attempts: job.Attempts, StartedAt: job.StartedAt,
			FinishedAt: job.FinishedAt, Error: job.LastError, CreatedAt: job.CreatedAt,
		}
		switch job.Status {
		case MemoryJobPending:
			task.Status = "pending"
			snapshot.Counts.Pending++
		case MemoryJobRunning:
			task.Status = "running"
			snapshot.Counts.Running++
		case MemoryJobCompleted:
			task.Status = "succeeded"
			if job.FinishedAt != nil && !job.FinishedAt.Before(cutoff) {
				snapshot.Counts.Succeeded24h++
			}
		case MemoryJobFailed:
			task.Status = "failed"
			if job.FinishedAt != nil && !job.FinishedAt.Before(cutoff) {
				snapshot.Counts.Failed24h++
			}
		}
		if len(snapshot.Tasks) < limit {
			snapshot.Tasks = append(snapshot.Tasks, task)
		}
	}
	snapshot.CanRun = !running && snapshot.Counts.Pending > 0
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
