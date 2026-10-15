package assessment

import (
	"DataArk/config"
	"DataArk/jobqueue"
	"DataArk/material"
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

func verifyPostgresBatchReset(t *testing.T) {
	t.Helper()
	oldSize, oldConcurrency := config.LLMBATCHSIZE, config.LLMBATCHCONCURRENCY
	config.LLMBATCHSIZE, config.LLMBATCHCONCURRENCY = 100, 10
	defer func() { config.LLMBATCHSIZE, config.LLMBATCHCONCURRENCY = oldSize, oldConcurrency }()
	ctx := context.Background()
	q, _ := jobqueue.Default()
	for n := 0; n < 3; n++ {
		c := createAssessmentCandidate(t, "Reset", fmt.Sprintf("https://batch.invalid/reset-%d", n), "Reset", strings.Repeat("restore evidence ", 100), time.Now())
		if err := q.EnqueueAssessArticle(ctx, c.MaterialID, "1"); err != nil {
			t.Fatal(err)
		}
	}
	if err := q.EnqueueAssessArticle(ctx, 900000, "0"); err != nil {
		t.Fatal(err)
	}
	_ = StartBatchRun(ctx)
	batchStep(t)
	var run BatchRun
	db.Where("active_key=1").First(&run)
	var items []BatchItem
	db.Where("run_id=?", run.ID).Order("id").Find(&items)
	if len(items) != 4 {
		t.Fatal(items)
	}
	if err := finishBatchItem(ctx, &run, items[0], "failed", nil, "Batch provider HTTP 401", nil); err != nil {
		t.Fatal(err)
	}
	// River retention can remove original cancelled jobs before reset.
	if err := db.Exec("DELETE FROM river_job WHERE id::text IN (?,?)", items[1].JobID, items[3].JobID).Error; err != nil {
		t.Fatal(err)
	}
	// A newer live task with the same target must be reused, including legacy
	// candidate args that map to the same material version.
	if err := db.Exec(`INSERT INTO river_job(kind,queue,args,state,max_attempts) VALUES (?,?,jsonb_build_object('candidate_id',?::bigint,'content_version','1'),'available',1)`, jobqueue.AssessArticleJobKind, jobqueue.AssessmentQueueName, items[2].CandidateID).Error; err != nil {
		t.Fatal(err)
	}
	// A duplicate cancelled task can have no item when admission sees an active
	// identity; the transfer tag must still make it recoverable.
	if err := db.Exec(`INSERT INTO river_job(kind,queue,args,state,max_attempts,finalized_at,metadata) VALUES (?,?,jsonb_build_object('material_id',?::bigint,'content_version','1'),'cancelled',1,now(),jsonb_build_object('batch_run_id',?::bigint))`, jobqueue.AssessArticleJobKind, jobqueue.AssessmentQueueName, items[2].MaterialID, run.ID).Error; err != nil {
		t.Fatal(err)
	}
	var b Batch
	b = Batch{RunID: run.ID, UUID: "reset-preflight", Phase: "submission_unknown", CreatedAt: time.Now(), NextCheckAt: time.Now()}
	if err := db.Create(&b).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := ResetBatchRun(ctx, run.ID, true); err == nil {
		t.Fatal("unknown remote submission accepted")
	}
	if err := db.Model(&b).Update("phase", "prepared").Error; err != nil {
		t.Fatal(err)
	}
	preview, err := ResetBatchRun(ctx, run.ID, true)
	if err != nil || preview.Targets != 5 {
		t.Fatal(preview, err)
	}
	// Force failure after River insertion: both queue and ownership changes must
	// roll back rather than leaving half-restored tasks.
	for _, sql := range []string{
		`CREATE FUNCTION reject_batch_return() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'reset rollback fixture'; END $$`,
		`CREATE TRIGGER reject_batch_return BEFORE UPDATE OF returned_at ON assessment_batch_items FOR EACH ROW EXECUTE FUNCTION reject_batch_return()`,
	} {
		if err := db.Exec(sql).Error; err != nil {
			t.Fatal(err)
		}
	}
	var before, after int64
	db.Table("river_job").Where("kind=? AND state='available'", jobqueue.AssessArticleJobKind).Count(&before)
	if _, err := ResetBatchRun(ctx, run.ID, false); err == nil {
		t.Fatal("reset ignored failing transaction")
	}
	db.Table("river_job").Where("kind=? AND state='available'", jobqueue.AssessArticleJobKind).Count(&after)
	if before != after {
		t.Fatal("River jobs survived failed reset", before, after)
	}
	if err := db.Exec("DROP TRIGGER reject_batch_return ON assessment_batch_items").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("DROP FUNCTION reject_batch_return()").Error; err != nil {
		t.Fatal(err)
	}
	result, err := ResetBatchRun(ctx, run.ID, false)
	if err != nil || result.Restored != 1 || result.Recreated != 2 || result.Reused != 2 {
		t.Fatal(result, err)
	}
	db.First(&run, run.ID)
	if run.Status != "rolled_back" || run.ActiveKey != nil || run.LeaseUntil != nil {
		t.Fatal(run)
	}
	var count int64
	db.Model(&BatchItem{}).Where("run_id=? AND (returned_at IS NULL OR active_identity IS NOT NULL OR restored_job_id='')", run.ID).Count(&count)
	if count != 0 {
		t.Fatal("ownership not released", count)
	}
	var state material.ArticleState
	db.First(&state, "material_id=?", items[0].MaterialID)
	if state.AssessmentState != "pending" || state.AssessmentError != "" {
		t.Fatal(state)
	}
	if err := BatchRunCounts(ctx, &run); err != nil || run.Counts.Returned != 4 || run.Counts.Pending != 0 {
		t.Fatal(run.Counts, err)
	}
	again, err := ResetBatchRun(ctx, run.ID, false)
	if err != nil || !again.AlreadyReturned {
		t.Fatal(again, err)
	}
	db.Table("river_job").Where("kind=? AND state='available'", jobqueue.AssessArticleJobKind).Count(&after)
	if after != before+3 {
		t.Fatal("reset changed unrelated jobs", before, after)
	}
}
