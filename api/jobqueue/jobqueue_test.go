package jobqueue

import (
	"DataArk/observability"
	"context"
	"database/sql"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/riverqueue/river"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type queueDiagnosticError struct{}

func (queueDiagnosticError) Error() string {
	return "fetch https://private.example/feed?token=secret failed; password=hunter2"
}

func (queueDiagnosticError) ObservabilityFailure() observability.FailureDetails {
	return observability.FailureDetails{ErrorType: "http_status", Domain: "feed.example.com", HTTPStatus: 502}
}

type recordingSQLExecer struct {
	query string
	args  []any
	rows  int64
	err   error
}

func (executor *recordingSQLExecer) ExecContext(_ context.Context, query string, args ...any) (sql.Result, error) {
	executor.query = query
	executor.args = args
	return affectedRowsResult(executor.rows), executor.err
}

type affectedRowsResult int64

func (affectedRowsResult) LastInsertId() (int64, error) { return 0, errors.New("unsupported") }
func (result affectedRowsResult) RowsAffected() (int64, error) {
	return int64(result), nil
}

func TestJobArgsUseStableIdentityAndUniqueOptions(t *testing.T) {
	tests := []struct {
		kind string
		args interface {
			Kind() string
			InsertOpts() river.InsertOpts
		}
	}{
		{FetchSourceJobKind, FetchSourceArgs{SourceID: 1}},
		{ScanBlogrollJobKind, ScanBlogrollArgs{SiteID: 2}},
		{BackfillSiteJobKind, BackfillSiteArgs{SiteID: 3}},
		{ProcessCandidateJobKind, ProcessCandidateArgs{CandidateID: 4, ContentVersion: "2"}},
		{GenerateDailyJobKind, GenerateDailyArgs{UserID: 5, LocalDate: "2026-07-13"}},
	}
	for _, testCase := range tests {
		if testCase.args.Kind() != testCase.kind {
			t.Fatalf("kind = %q, want %q", testCase.args.Kind(), testCase.kind)
		}
		if !testCase.args.InsertOpts().UniqueOpts.ByArgs {
			t.Fatalf("%s must be unique by stable args", testCase.kind)
		}
		if testCase.kind == GenerateDailyJobKind && testCase.args.InsertOpts().Queue == DiscoveryQueueName {
			t.Fatal("daily generation must not use the manually gated discovery queue")
		}
		if testCase.kind != GenerateDailyJobKind && testCase.args.InsertOpts().Queue != DiscoveryQueueName {
			t.Fatalf("%s queue = %q, want %q", testCase.kind, testCase.args.InsertOpts().Queue, DiscoveryQueueName)
		}
		if testCase.kind != GenerateDailyJobKind && testCase.args.InsertOpts().MaxAttempts != 1 {
			t.Fatalf("%s max attempts = %d, want 1 so domain backoff remains manually gated", testCase.kind, testCase.args.InsertOpts().MaxAttempts)
		}
	}
}

func TestCompactQueueErrorRedactsURLsAndSecrets(t *testing.T) {
	got := compactQueueError("fetch https://private.example/feed failed; token=secret-value")
	if got != "fetch [url] failed; token=[redacted]" {
		t.Fatalf("sanitized error = %q", got)
	}
}

func TestWorkerEventIncludesSafeFailureDiagnostics(t *testing.T) {
	event := workerEvent("fetch_source", "42", observability.Event{SourceID: 7}, queueDiagnosticError{})
	if event.Name != "job_fetch_source" || event.JobID != "42" || event.SourceID != 7 || event.Status != "failed" {
		t.Fatalf("event identity = %#v", event)
	}
	if event.ErrorType != "http_status" || event.Domain != "feed.example.com" || event.HTTPStatus != 502 {
		t.Fatalf("event diagnostics = %#v", event)
	}
	if strings.Contains(event.ErrorMessage, "private.example") || strings.Contains(event.ErrorMessage, "secret") || strings.Contains(event.ErrorMessage, "hunter2") {
		t.Fatalf("unsafe message = %q", event.ErrorMessage)
	}
}

func TestReconcileManualDiscoveryJobsMovesActiveAndResetsInterruptedWork(t *testing.T) {
	executor := &recordingSQLExecer{rows: 7}
	reconciled, err := reconcileManualDiscoveryJobs(context.Background(), executor)
	if err != nil {
		t.Fatal(err)
	}
	if reconciled != 7 {
		t.Fatalf("reconciled jobs = %d, want 7", reconciled)
	}
	for _, fragment := range []string{
		"UPDATE river_job",
		"state = CASE WHEN state = 'running' THEN 'available'::river_job_state ELSE state END",
		"attempted_at = CASE WHEN state = 'running' THEN NULL ELSE attempted_at END",
		"max_attempts = GREATEST(attempt, 1)",
		"(queue = $1 AND state = 'running')",
	} {
		if !strings.Contains(executor.query, fragment) {
			t.Fatalf("migration query missing %q: %s", fragment, executor.query)
		}
	}
	wantArgs := []any{
		DiscoveryQueueName,
		river.QueueDefault,
		FetchSourceJobKind,
		ScanBlogrollJobKind,
		BackfillSiteJobKind,
		ProcessCandidateJobKind,
	}
	if len(executor.args) != len(wantArgs) {
		t.Fatalf("migration args = %#v, want %#v", executor.args, wantArgs)
	}
	for index := range wantArgs {
		if executor.args[index] != wantArgs[index] {
			t.Fatalf("migration arg %d = %#v, want %#v", index, executor.args[index], wantArgs[index])
		}
	}
}

func TestReconcileManualDiscoveryJobsReturnsDatabaseError(t *testing.T) {
	want := errors.New("database unavailable")
	_, err := reconcileManualDiscoveryJobs(context.Background(), &recordingSQLExecer{err: want})
	if !errors.Is(err, want) {
		t.Fatalf("migration error = %v, want %v", err, want)
	}
}

func TestMemoryQueueRetriesInterruptedJobsAndIsolatesFailures(t *testing.T) {
	store := NewMemoryStore()
	var mu sync.Mutex
	attempts := map[uint]int{}
	handlers := Handlers{FetchSource: func(_ context.Context, sourceID uint) error {
		mu.Lock()
		defer mu.Unlock()
		attempts[sourceID]++
		if sourceID == 1 && attempts[sourceID] == 1 {
			return errors.New("source one failed")
		}
		return nil
	}}

	firstProcess := NewMemoryQueue(store, handlers)
	if err := firstProcess.EnqueueFetchSource(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	if err := firstProcess.EnqueueFetchSource(context.Background(), 2); err != nil {
		t.Fatalf("second source should not be blocked: %v", err)
	}
	if executions := len(store.Snapshot()); executions != 2 || len(attempts) != 0 {
		t.Fatalf("staged jobs=%d attempts=%#v; enqueue must not execute", executions, attempts)
	}
	if _, err := firstProcess.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	waitForMemoryQueueIdle(t, firstProcess)

	secondProcess := NewMemoryQueue(store, handlers)
	if err := secondProcess.EnqueueFetchSource(context.Background(), 1); err != nil {
		t.Fatalf("failed job should resume after restart: %v", err)
	}
	if _, err := secondProcess.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	waitForMemoryQueueIdle(t, secondProcess)
	if err := secondProcess.EnqueueFetchSource(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	if attempts[1] != 2 || attempts[2] != 1 {
		t.Fatalf("attempts = %#v", attempts)
	}

	store.mu.Lock()
	store.jobs[ScanBlogrollJobKind+":site:9"] = MemoryJob{Key: ScanBlogrollJobKind + ":site:9", Kind: ScanBlogrollJobKind, Status: MemoryJobRunning, Attempts: 1}
	store.mu.Unlock()
	var scanned atomic.Int32
	thirdProcess := NewMemoryQueue(store, Handlers{ScanBlogroll: func(context.Context, uint) error {
		scanned.Add(1)
		return nil
	}})
	if err := thirdProcess.EnqueueScanBlogroll(context.Background(), 9); err != nil {
		t.Fatal(err)
	}
	if _, err := thirdProcess.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	waitForMemoryQueueIdle(t, thirdProcess)
	if scanned.Load() != 1 {
		t.Fatalf("interrupted scan executions = %d", scanned.Load())
	}
}

func TestMemoryQueueConcurrentDuplicateExecutesOnce(t *testing.T) {
	store := NewMemoryStore()
	var executions atomic.Int32
	queue := NewMemoryQueue(store, Handlers{ProcessCandidate: func(context.Context, uint, string) error {
		executions.Add(1)
		time.Sleep(time.Millisecond)
		return nil
	}})
	var group sync.WaitGroup
	for index := 0; index < 100; index++ {
		group.Add(1)
		go func() {
			defer group.Done()
			if err := queue.EnqueueProcessCandidate(context.Background(), 7, "v3"); err != nil {
				t.Errorf("enqueue: %v", err)
			}
		}()
	}
	group.Wait()
	if executions.Load() != 0 {
		t.Fatalf("enqueue executed handlers before manual run: %d", executions.Load())
	}
	if _, err := queue.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	waitForMemoryQueueIdle(t, queue)
	if executions.Load() != 1 {
		t.Fatalf("executions = %d, want 1", executions.Load())
	}
	jobs := store.Snapshot()
	if len(jobs) != 1 || jobs[0].Status != MemoryJobCompleted || jobs[0].Attempts != 1 {
		t.Fatalf("jobs = %#v", jobs)
	}
}

func TestMemoryQueueManualRunDrainsDerivedJobs(t *testing.T) {
	store := NewMemoryStore()
	var queue *MemoryQueue
	var processed atomic.Int32
	queue = NewMemoryQueue(store, Handlers{
		FetchSource: func(ctx context.Context, sourceID uint) error {
			if sourceID != 5 {
				t.Fatalf("source ID = %d, want 5", sourceID)
			}
			return queue.EnqueueProcessCandidate(ctx, 8, "3")
		},
		ProcessCandidate: func(_ context.Context, candidateID uint, version string) error {
			if candidateID != 8 || version != "3" {
				t.Fatalf("candidate = %d version=%q", candidateID, version)
			}
			processed.Add(1)
			return nil
		},
	})
	if err := queue.EnqueueFetchSource(context.Background(), 5); err != nil {
		t.Fatal(err)
	}
	if processed.Load() != 0 {
		t.Fatal("derived work ran before the manual queue was started")
	}
	if _, err := queue.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	waitForMemoryQueueIdle(t, queue)
	if processed.Load() != 1 {
		t.Fatalf("processed = %d, want 1", processed.Load())
	}
	snapshot, err := queue.Snapshot(context.Background(), 50)
	if err != nil || snapshot.Counts.Succeeded24h != 2 || snapshot.Counts.Pending != 0 || snapshot.State != "idle" {
		t.Fatalf("snapshot = %#v err=%v", snapshot, err)
	}
}

func TestStartSQLiteInstallsRecoverableSharedQueue(t *testing.T) {
	database, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	var generated atomic.Int32
	stop, err := Start(context.Background(), database, Handlers{GenerateDaily: func(context.Context, uint, string) error {
		generated.Add(1)
		return nil
	}}, func(ctx context.Context, queue JobEnqueuer) error {
		return queue.EnqueueGenerateDaily(ctx, 8, "2026-07-13")
	})
	if err != nil {
		t.Fatal(err)
	}
	if generated.Load() != 1 {
		t.Fatalf("recovered daily jobs = %d", generated.Load())
	}
	if _, available := Default(); !available {
		t.Fatal("shared queue was not installed")
	}
	stop()
	stop()
	if _, available := Default(); available {
		t.Fatal("shared queue was not restored on stop")
	}
}

func TestStartSQLiteRecoveryWaitsForManualRun(t *testing.T) {
	database, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	var executions atomic.Int32
	handlers := Handlers{FetchSource: func(context.Context, uint) error {
		executions.Add(1)
		return nil
	}}
	recover := func(ctx context.Context, queue JobEnqueuer) error {
		return queue.EnqueueFetchSource(ctx, 99)
	}
	stopFirst, err := Start(context.Background(), database, handlers, recover)
	if err != nil {
		t.Fatal(err)
	}
	if executions.Load() != 0 {
		t.Fatalf("startup executions = %d, want 0", executions.Load())
	}
	controller, available := CrawlControl()
	if !available {
		t.Fatal("crawl queue controller unavailable")
	}
	snapshot, err := controller.Snapshot(context.Background(), 50)
	if err != nil || snapshot.State != "waiting" || snapshot.Counts.Pending != 1 {
		t.Fatalf("snapshot = %#v err=%v", snapshot, err)
	}
	if _, err := controller.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	waitForControllerIdle(t, controller)
	if executions.Load() != 1 {
		t.Fatalf("manual executions = %d, want 1", executions.Load())
	}
	stopFirst()
}

func waitForMemoryQueueIdle(t *testing.T, queue *MemoryQueue) {
	t.Helper()
	waitForControllerIdle(t, queue)
}

func waitForControllerIdle(t *testing.T, controller CrawlQueueController) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		snapshot, err := controller.Snapshot(context.Background(), 50)
		if err != nil {
			t.Fatal(err)
		}
		if snapshot.State != "running" {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("manual queue did not return to idle")
}
