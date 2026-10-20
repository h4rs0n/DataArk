package assessment

import (
	"DataArk/config"
	"DataArk/jobqueue"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestBatchRestartBoundaries(t *testing.T) {
	for _, phase := range []string{"pending", "prepared", "upload_unknown", "file_processing", "submission_unknown", "submitted", "importing"} {
		t.Run(phase, func(t *testing.T) {
			p, _, _ := setupBatchFixture(t)
			ctx := context.Background()
			_ = StartBatchRun(ctx) // Fixture has no automatic tick handler.
			if phase != "pending" {
				reachPreparedRestartBatch(t)
				if phase == "upload_unknown" {
					p.uploadError = errors.New("upload response lost")
				}
				if phase != "prepared" {
					batchStep(t)
				}
				if phase == "submission_unknown" {
					p.createError = errors.New("submission response lost")
				}
				if phase == "submission_unknown" || phase == "submitted" || phase == "importing" {
					batchStep(t)
				}
				if phase == "importing" {
					batchStep(t)
				}
				var b Batch
				if err := db.First(&b).Error; err != nil || b.Phase != phase {
					t.Fatal("wrong restart boundary", b.Phase, err)
				}
				var item BatchItem
				db.Where("batch_id=?", b.ID).First(&item)
				p.output = batchResult(item.CustomID, validBatchAssessment)
			}
			if err := ResetBatchAdmission(ctx); err != nil {
				t.Fatal(err)
			}
			if err := ResetBatchAdmission(ctx); err != nil {
				t.Fatal(err)
			}
			requests := p.requests
			for n := 0; n < 6; n++ {
				var active int64
				db.Model(&BatchRun{}).Where("active_key=1").Count(&active)
				if active == 0 {
					break
				}
				batchStep(t)
			}
			var run BatchRun
			db.First(&run)
			if run.AdmissionEnabled {
				t.Fatal("restart reauthorized admission")
			}
			if phase == "pending" || phase == "prepared" {
				if p.requests != requests {
					t.Fatal("restart started a provider request")
				}
				snapshot, err := DecorateQueue(ctx, &jobqueue.CrawlQueueSnapshot{})
				if err != nil || snapshot.State != "paused" || !snapshot.CanRun {
					t.Fatal("cannot resume", snapshot, err)
				}
				if err := ProcessBatchTick(ctx); err != nil {
					t.Fatal("idle tick rescheduled", err)
				}
				if err := RecoverBatchJobs(ctx); err != nil {
					t.Fatal("idle recovery rescheduled", err)
				}
				_ = StartBatchRun(ctx)
				db.First(&run)
				if run.AdmissionEnabled {
					t.Fatal("automatic start reauthorized existing run")
				}
				for n := 0; n < 2; n++ {
					resumed, err := ResumeBatchRun(ctx)
					if !resumed || !errors.Is(err, jobqueue.ErrHandlerUnavailable) {
						t.Fatal("manual resume", resumed, err)
					}
				}
				db.First(&run)
				if !run.AdmissionEnabled || run.ID != 1 {
					t.Fatal("resume replaced run", run)
				}
				reachSubmittedBatch(t, p)
			} else {
				var item BatchItem
				db.Where("status='succeeded'").First(&item)
				if item.AssessmentID == nil || run.ActiveKey != nil {
					t.Fatal("existing upload not completed", phase, item, run)
				}
			}
			if phase != "pending" && phase != "prepared" && (p.uploads != 1 || p.creates != 1) {
				t.Fatal("duplicate provider submission", p.uploads, p.creates)
			}
		})
	}
}

func TestBatchRestartKeepsRemainderAndOriginalPrefix(t *testing.T) {
	p, _, _ := setupBatchFixture(t)
	verifyBatchRestartRemainder(t, p)
}

// Also exercised against real PostgreSQL/River after the existing integration run.
func verifyBatchRestartRemainder(t *testing.T, p *fixtureBatchProvider) {
	t.Helper()
	ctx := context.Background()
	oldSize, oldConcurrency := config.LLMBATCHSIZE, config.LLMBATCHCONCURRENCY
	config.LLMBATCHSIZE, config.LLMBATCHCONCURRENCY = 1, 1
	defer func() { config.LLMBATCHSIZE, config.LLMBATCHCONCURRENCY = oldSize, oldConcurrency }()
	q, _ := jobqueue.Default()
	tail := createAssessmentCandidate(t, "Restart tail", "https://restart.invalid/tail", "Tail", strings.Repeat("restart tail evidence ", 100), time.Now())
	if err := q.EnqueueAssessArticle(ctx, tail.MaterialID, "1"); err != nil {
		t.Fatal(err)
	}
	_ = StartBatchRun(ctx)
	initialCreates := p.creates
	for n := 0; n < 8 && p.creates == initialCreates; n++ {
		batchStep(t)
	}
	var run BatchRun
	db.Where("active_key=1").First(&run)
	highWater, cursor, id := run.HighWater, run.Cursor, run.ID
	var item BatchItem
	db.Where("run_id=? AND active_identity IS NOT NULL", id).First(&item)
	p.output = batchResult(item.CustomID, validBatchAssessment)
	later := createAssessmentCandidate(t, "Later", "https://restart.invalid/later", "Later", strings.Repeat("later evidence ", 100), time.Now())
	if err := q.EnqueueAssessArticle(ctx, later.MaterialID, "1"); err != nil {
		t.Fatal(err)
	}
	if err := ResetBatchAdmission(ctx); err != nil {
		t.Fatal(err)
	}
	for n := 0; n < 6; n++ {
		batchStep(t)
	}
	db.First(&run, id)
	if run.Cursor != cursor || run.HighWater != highWater || run.AdmissionEnabled || p.creates != initialCreates+1 {
		t.Fatal("restart consumed remainder", run, p.creates)
	}
	if err := ProcessBatchTick(ctx); err != nil {
		t.Fatal("idle run kept scheduling", err)
	}
	_, _ = ResumeBatchRun(ctx)
	for n := 0; n < 8 && p.creates < initialCreates+2; n++ {
		batchStep(t)
	}
	db.First(&run, id)
	if run.HighWater != highWater {
		t.Fatal("resume expanded prefix")
	}
	var laterItems int64
	db.Model(&BatchItem{}).Where("run_id=? AND material_id=?", id, later.MaterialID).Count(&laterItems)
	if laterItems != 0 || p.creates != initialCreates+2 {
		t.Fatal("resume changed queue scope", laterItems, p.creates)
	}
	// The post-authorization article stays queued. Remove only that fixture job
	// from PostgreSQL so subsequent integration scenarios get a clean prefix.
	if db.Dialector.Name() == "postgres" {
		if err := db.Exec("DELETE FROM river_job WHERE queue=? AND args->>'material_id'=?", jobqueue.AssessmentQueueName, fmt.Sprint(later.MaterialID)).Error; err != nil {
			t.Fatal(err)
		}
	}
	// Leave the shared integration database ready for subsequent scenarios.
	var remaining BatchItem
	db.Where("run_id=? AND active_identity IS NOT NULL", id).First(&remaining)
	p.output = batchResult(remaining.CustomID, validBatchAssessment)
	for n := 0; n < 4; n++ {
		db.First(&run, id)
		if run.ActiveKey == nil {
			break
		}
		batchStep(t)
	}
}

func TestBatchRestartPreparedDoesNotBlockResults(t *testing.T) {
	p, _, _ := setupBatchFixture(t)
	ctx := context.Background()
	_ = StartBatchRun(ctx)
	reachPreparedRestartBatch(t)
	var prepared Batch
	db.First(&prepared)
	// An older prepared batch must not hide a later in-flight batch.
	remote, err := p.Create(ctx, "existing-file", "already-started")
	if err != nil {
		t.Fatal(err)
	}
	b := Batch{RunID: prepared.RunID, UUID: "existing-upload", Phase: "submitted", RemoteID: remote.ID, NextCheckAt: time.Now(), CreatedAt: time.Now()}
	if err := db.Create(&b).Error; err != nil {
		t.Fatal(err)
	}
	if err := ResetBatchAdmission(ctx); err != nil {
		t.Fatal(err)
	}
	p.status = "in_progress"
	batchStep(t)
	db.First(&b, b.ID)
	if b.ProviderStatus != "in_progress" || p.uploads != 0 {
		t.Fatal("prepared blocked polling", b, p.uploads)
	}
	var run BatchRun
	db.First(&run)
	if err := advanceBatch(ctx, &run, &prepared); err != nil {
		t.Fatal(err)
	}
	if p.uploads != 0 {
		t.Fatal("direct advance bypassed admission")
	}
}

func TestBatchRestartPreservesErrorAndCleanupPause(t *testing.T) {
	p, _, _ := setupBatchFixture(t)
	ctx := context.Background()
	_ = StartBatchRun(ctx)
	reachPreparedRestartBatch(t)
	p.operationErrors = map[string]error{"upload": batchHTTPError{Status: 429}}
	batchStep(t)
	if err := ResetBatchAdmission(ctx); err != nil {
		t.Fatal(err)
	}
	requests := p.requests
	if err := ProcessBatchTick(ctx); err != nil {
		t.Fatal(err)
	}
	if err := RecoverBatchJobs(ctx); err != nil {
		t.Fatal(err)
	}
	if p.requests != requests {
		t.Fatal("restart bypassed error pause")
	}
	var run BatchRun
	db.First(&run)
	if run.Status != "paused" || run.ErrorCode != 429 {
		t.Fatal(run)
	}
	if err := db.Model(&run).Update("resume_status", "cleanup").Error; err != nil {
		t.Fatal(err)
	}
	if resumed, err := ResumeBatchRun(ctx); err == nil || resumed {
		t.Fatal("bypassed cleanup pause")
	}
	_ = ResumeBatchCleanup(ctx)
	db.First(&run)
	if run.AdmissionEnabled {
		t.Fatal("cleanup resume authorized new work")
	}
}

func reachPreparedRestartBatch(t *testing.T) {
	t.Helper()
	for n := 0; n < 8; n++ {
		var count int64
		if err := db.Model(&Batch{}).Where("phase='prepared'").Count(&count).Error; err != nil {
			t.Fatal(err)
		}
		if count > 0 {
			return
		}
		batchStep(t)
	}
	t.Fatal("batch not prepared")
}

func TestBatchRestartLastImportFinishesWithoutScheduling(t *testing.T) {
	p, _, _ := setupBatchFixture(t)
	ctx := context.Background()
	_ = StartBatchRun(ctx)
	reachSubmittedBatch(t, p)
	var item BatchItem
	if err := db.First(&item).Error; err != nil {
		t.Fatal(err)
	}
	p.output = batchResult(item.CustomID, validBatchAssessment)
	if err := ResetBatchAdmission(ctx); err != nil {
		t.Fatal(err)
	}
	batchStep(t) // Retrieve the completed provider batch.
	if err := db.Model(&Batch{}).Where("run_id=?", item.RunID).Update("next_check_at", time.Now().Add(-time.Second)).Error; err != nil {
		t.Fatal(err)
	}
	// There is no tick handler in this fixture. Trying to schedule would fail.
	if err := ProcessBatchTick(ctx); err != nil {
		t.Fatal("last import tried to schedule idle work", err)
	}
	var run BatchRun
	if err := db.First(&run, item.RunID).Error; err != nil {
		t.Fatal(err)
	}
	if run.ActiveKey != nil || run.Status != "completed" {
		t.Fatal("last import stranded run", run)
	}
}
