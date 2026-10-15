package jobqueue

import (
	"context"
	"testing"
)

func TestMemoryPrunesAssessmentsInBatchesAndKeepsRunningJobs(t *testing.T) {
	store := NewMemoryStore()
	queue := NewMemoryQueue(store, Handlers{AssessArticle: func(context.Context, uint, string) error {
		t.Error("pending assessment executed during pruning")
		return nil
	}})
	for id := uint(1); id <= 501; id++ {
		if err := queue.EnqueueAssessArticle(context.Background(), id, "1"); err != nil {
			t.Fatal(err)
		}
	}
	store.mu.Lock()
	for key, job := range store.jobs {
		if job.TargetID == 1 {
			job.Status = MemoryJobRunning
			store.jobs[key] = job
		}
	}
	store.mu.Unlock()
	calls, checked := 0, 0
	cancelled, err := queue.PruneAssessments(context.Background(), func(targets []AssessmentTarget) (map[uint]bool, error) {
		calls++
		checked += len(targets)
		if len(targets) > 250 {
			t.Fatalf("unbounded batch: %d", len(targets))
		}
		result := map[uint]bool{}
		for _, target := range targets {
			result[target.MaterialID] = target.MaterialID%2 == 1
		}
		return result, nil
	})
	if err != nil || cancelled != 250 || calls < 2 || calls > 3 || checked != 500 {
		t.Fatalf("cancelled=%d calls=%d checked=%d err=%v", cancelled, calls, checked, err)
	}
	for _, job := range store.Snapshot() {
		if job.TargetID == 1 && job.Status != MemoryJobRunning {
			t.Fatal("running assessment was cancelled")
		}
		if job.TargetID%2 == 0 && job.Status != MemoryJobPending {
			t.Fatal("qualified assessment was cancelled")
		}
	}
}
