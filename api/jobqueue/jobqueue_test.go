package jobqueue

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/riverqueue/river"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

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
	if err := firstProcess.EnqueueFetchSource(context.Background(), 1); err == nil {
		t.Fatal("first source attempt should fail")
	}
	if err := firstProcess.EnqueueFetchSource(context.Background(), 2); err != nil {
		t.Fatalf("second source should not be blocked: %v", err)
	}

	secondProcess := NewMemoryQueue(store, handlers)
	if err := secondProcess.EnqueueFetchSource(context.Background(), 1); err != nil {
		t.Fatalf("failed job should resume after restart: %v", err)
	}
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
	if executions.Load() != 1 {
		t.Fatalf("executions = %d, want 1", executions.Load())
	}
	jobs := store.Snapshot()
	if len(jobs) != 1 || jobs[0].Status != MemoryJobCompleted || jobs[0].Attempts != 1 {
		t.Fatalf("jobs = %#v", jobs)
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

func TestStartSQLiteDuplicateRecoveryRunsOnce(t *testing.T) {
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
	stopSecond, err := Start(context.Background(), database, handlers, recover)
	if err != nil {
		stopFirst()
		t.Fatal(err)
	}
	if executions.Load() != 1 {
		t.Fatalf("duplicate startup executions = %d, want 1", executions.Load())
	}
	stopSecond()
	stopFirst()
}
