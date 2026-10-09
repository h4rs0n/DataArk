package jobqueue

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestMemoryCrawlDisableFinishesOnlyClaimedJobs(t *testing.T) {
	ctx := context.Background()
	started := make(chan struct{}, 8)
	release := make(chan struct{})
	var executions atomic.Int32
	queue := NewMemoryQueue(NewMemoryStore(), Handlers{FetchSource: func(context.Context, uint) error {
		executions.Add(1)
		started <- struct{}{}
		<-release
		return nil
	}})
	view := memoryCrawlView{queue: queue}
	for id := uint(1); id <= 8; id++ {
		if err := queue.EnqueueFetchSource(ctx, id); err != nil {
			t.Fatal(err)
		}
	}
	if executions.Load() != 0 {
		t.Fatal("disabled queue executed jobs")
	}
	if _, err := view.SetAutoStart(ctx, true); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 4; index++ {
		select {
		case <-started:
		case <-time.After(3 * time.Second):
			t.Fatal("jobs did not start")
		}
	}
	if _, err := view.SetAutoStart(ctx, false); err != nil {
		t.Fatal(err)
	}
	close(release)
	waitForMemoryQueueIdle(t, queue)
	snapshot, err := view.Snapshot(ctx, 50)
	if err != nil || snapshot.Mode != "manual" || snapshot.Counts.Pending != 4 || snapshot.Counts.Succeeded24h != 4 || executions.Load() != 4 {
		t.Fatalf("snapshot=%#v err=%v executions=%d", snapshot, err, executions.Load())
	}
	if _, err := view.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := view.Run(ctx); err != nil {
		t.Fatal(err)
	}
	waitForMemoryQueueIdle(t, queue)
	if executions.Load() != 8 {
		t.Fatalf("executions=%d", executions.Load())
	}
	if err := queue.EnqueueFetchSource(ctx, 9); err != nil {
		t.Fatal(err)
	}
	snapshot, _ = view.Snapshot(ctx, 50)
	if snapshot.State != "waiting" || snapshot.Counts.Pending != 1 || !snapshot.CanRun || executions.Load() != 8 {
		t.Fatalf("new job must wait: %#v executions=%d", snapshot, executions.Load())
	}
}

func TestMemoryCrawlSwitchInvalidatesPreviousManualRun(t *testing.T) {
	ctx := context.Background()
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	var executions atomic.Int32
	queue := NewMemoryQueue(NewMemoryStore(), Handlers{FetchSource: func(_ context.Context, id uint) error {
		executions.Add(1)
		if id == 1 {
			started <- struct{}{}
			<-release
		}
		return nil
	}})
	view := memoryCrawlView{queue: queue}
	if err := queue.EnqueueFetchSource(ctx, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := view.Run(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("manual run did not start")
	}
	for _, enabled := range []bool{true, false, true} {
		if _, err := view.SetAutoStart(ctx, enabled); err != nil {
			t.Fatal(err)
		}
	}
	if err := queue.EnqueueFetchSource(ctx, 2); err != nil {
		t.Fatal(err)
	}
	close(release)
	waitForMemoryQueueIdle(t, queue)
	if err := queue.EnqueueFetchSource(ctx, 3); err != nil {
		t.Fatal(err)
	}
	waitForMemoryQueueIdle(t, queue)
	snapshot, _ := view.Snapshot(ctx, 50)
	if snapshot.Mode != "automatic" || snapshot.CanRun || executions.Load() != 3 {
		t.Fatalf("snapshot=%#v executions=%d", snapshot, executions.Load())
	}
}

func TestMemoryCrawlInterruptedManualRunWaitsAfterRestart(t *testing.T) {
	store := NewMemoryStore()
	store.jobs["interrupted"] = MemoryJob{Key: "interrupted", Kind: FetchSourceJobKind, TargetID: 1, Status: MemoryJobRunning}
	var executions atomic.Int32
	queue := NewMemoryQueue(store, Handlers{FetchSource: func(context.Context, uint) error { executions.Add(1); return nil }})
	view := memoryCrawlView{queue: queue}
	snapshot, _ := view.Snapshot(context.Background(), 50)
	if snapshot.Counts.Pending != 1 || snapshot.Mode != "manual" || executions.Load() != 0 {
		t.Fatalf("snapshot=%#v", snapshot)
	}
	if _, err := view.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	waitForMemoryQueueIdle(t, queue)
	if executions.Load() != 1 {
		t.Fatalf("executions=%d", executions.Load())
	}
}
