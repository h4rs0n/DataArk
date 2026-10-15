package assessment

import (
	"DataArk/config"
	"DataArk/jobqueue"
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestBatchHTTPPausesEveryProviderStageUntilManualResume(t *testing.T) {
	stages := []struct{ op, phase string }{
		{"upload", "prepared"}, {"file", "file_processing"}, {"create", "file_processing"},
		{"find_file", "upload_unknown"}, {"find_batch", "submission_unknown"},
		{"retrieve", "submitted"}, {"download", "importing"},
	}
	for _, code := range []int{401, 429} {
		for _, s := range stages {
			t.Run(fmt.Sprintf("%d/%s", code, s.op), func(t *testing.T) {
				p, stop, _ := setupBatchFixture(t)
				ctx := context.Background()
				_ = StartBatchRun(ctx)
				batchStep(t)
				batchStep(t)
				var b Batch
				if err := db.First(&b).Error; err != nil {
					t.Fatal(err)
				}
				if err := db.Model(&b).Updates(map[string]any{"phase": s.phase, "input_file_id": "file-fixture", "remote_id": "remote-fixture", "output_file_id": "output-fixture"}).Error; err != nil {
					t.Fatal(err)
				}
				p.operationErrors = map[string]error{s.op: batchHTTPError{Status: code}}
				batchStep(t)
				var run BatchRun
				db.First(&run)
				if run.Status != "paused" || run.ErrorCode != code || run.ResumeStatus != "processing" || run.PausedAt == nil || run.ActiveKey == nil {
					t.Fatalf("not durably paused: %#v", run)
				}
				db.First(&b, b.ID)
				if b.Phase != s.phase {
					t.Fatal("resume phase lost", b.Phase)
				}
				var item BatchItem
				db.First(&item)
				if item.Status != "processing" || item.ActiveIdentity == nil {
					t.Fatal("HTTP error finalized whole batch", item)
				}
				requests := p.requests
				stop()
				stop2, err := jobqueue.Start(ctx, db, jobqueue.Handlers{ReservedAssessments: ReservedBatchTargets}, nil)
				if err != nil {
					t.Fatal(err)
				}
				defer stop2()
				for n := 0; n < 2; n++ {
					if err := RecoverBatchJobs(ctx); err != nil {
						t.Fatal(err)
					}
					if err := ProcessBatchTick(ctx); err != nil {
						t.Fatal(err)
					}
					if err := StartBatchRun(ctx); err != nil {
						t.Fatal(err)
					}
				}
				if p.requests != requests {
					t.Fatal("paused restart called provider")
				}
				snapshot, _ := jobqueue.AssessmentControl()
				snap, err := snapshot.Snapshot(ctx, 1)
				if err != nil {
					t.Fatal(err)
				}
				decorated, err := DecorateQueue(ctx, snap)
				if err != nil {
					t.Fatal(err)
				}
				if decorated.State != "paused" || !decorated.CanRun || decorated.BatchRun.ErrorCode != code {
					t.Fatal(decorated)
				}
				resumed, _ := ResumeBatchRun(ctx) // fixture intentionally has no tick handler
				if !resumed {
					t.Fatal("manual resume failed")
				}
				var after BatchRun
				db.First(&after, run.ID)
				if after.Status != "processing" || after.ErrorCode != 0 || after.PausedAt != nil || after.ID != run.ID || after.Cursor != run.Cursor {
					t.Fatal(after)
				}
			})
		}
	}
}

func TestBatchOutputHTTPPausePreservesPriorRowsAndContinuesOnResume(t *testing.T) {
	for _, code := range []int{401, 429} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			p, _, _ := setupBatchFixture(t)
			ctx := context.Background()
			q, _ := jobqueue.Default()
			for n := 0; n < 2; n++ {
				c := createAssessmentCandidate(t, "Output", fmt.Sprintf("https://batch.invalid/output-%d", n), "Output", strings.Repeat("output evidence ", 100), time.Now())
				if err := q.EnqueueAssessArticle(ctx, c.MaterialID, "1"); err != nil {
					t.Fatal(err)
				}
			}
			_ = StartBatchRun(ctx)
			reachSubmittedBatch(t, p)
			var items []BatchItem
			db.Order("id").Find(&items)
			p.output = batchResult(items[0].CustomID, validBatchAssessment) + fmt.Sprintf(`{"custom_id":%q,"response":{"status_code":%d,"body":{}},"error":null}`+"\n", items[1].CustomID, code) + batchResult(items[2].CustomID, validBatchAssessment)
			batchStep(t)
			batchStep(t)
			var run BatchRun
			db.First(&run)
			if run.Status != "paused" {
				t.Fatal(run)
			}
			db.Order("id").Find(&items)
			if items[0].Status != "succeeded" || items[1].Status != "failed" || items[2].Status != "processing" {
				t.Fatal(items)
			}
			_, _ = ResumeBatchRun(ctx)
			batchStep(t)
			batchStep(t)
			db.Order("id").Find(&items)
			if items[2].Status != "succeeded" {
				t.Fatal(items)
			}
			var calls int64
			db.Model(&LLMCall{}).Count(&calls)
			if calls != 3 || p.creates != 1 {
				t.Fatal("repeated inference/import", calls, p.creates)
			}
		})
	}
}

func TestBatchTenSlotsHundredArticlesLeaveRemainderInRiver(t *testing.T) {
	p, _, _ := setupBatchFixture(t)
	_ = p
	oldSize, oldConcurrency := config.LLMBATCHSIZE, config.LLMBATCHCONCURRENCY
	config.LLMBATCHSIZE, config.LLMBATCHCONCURRENCY = 100, 10
	t.Cleanup(func() { config.LLMBATCHSIZE, config.LLMBATCHCONCURRENCY = oldSize, oldConcurrency })
	ctx := context.Background()
	q, _ := jobqueue.Default()
	for n := 0; n < 100; n++ {
		c := createAssessmentCandidate(t, "Capacity", fmt.Sprintf("https://batch.invalid/capacity-%d", n), "Capacity", strings.Repeat("capacity evidence ", 100), time.Now())
		if err := q.EnqueueAssessArticle(ctx, c.MaterialID, "1"); err != nil {
			t.Fatal(err)
		}
	}
	_ = StartBatchRun(ctx)
	var run BatchRun
	db.First(&run)
	for n := 0; n < 9; n++ {
		if err := db.Create(&Batch{RunID: run.ID, UUID: fmt.Sprintf("capacity-%d", n), Phase: "submitted", Total: 100, NextCheckAt: time.Now().Add(time.Hour), CreatedAt: time.Now()}).Error; err != nil {
			t.Fatal(err)
		}
	}
	transfer, _ := batchTransfer()
	if err := processBatchRunStep(ctx, &run, transfer); err != nil {
		t.Fatal(err)
	}
	var count int64
	db.Model(&Batch{}).Where("run_id=?", run.ID).Count(&count)
	if count != 10 {
		t.Fatal(count)
	}
	var newest Batch
	db.Order("id DESC").First(&newest)
	if newest.Total != 100 {
		t.Fatal("batch not limited to 100", newest.Total)
	}
	if err := db.Model(&Batch{}).Where("run_id=?", run.ID).Update("next_check_at", time.Now().Add(time.Hour)).Error; err != nil {
		t.Fatal(err)
	}
	cursor := run.Cursor
	if err := processBatchRunStep(ctx, &run, transfer); err != nil {
		t.Fatal(err)
	}
	if run.Cursor != cursor {
		t.Fatal("capacity admitted an eleventh batch")
	}
	controller, _ := jobqueue.AssessmentControl()
	snapshot, err := controller.Snapshot(ctx, 5)
	if err != nil || snapshot.Counts.Pending != 1 {
		t.Fatal("remainder left River", snapshot, err)
	}
}
