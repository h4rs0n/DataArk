package assessment

import (
	"DataArk/jobqueue"
	"DataArk/material"
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type BatchResetResult struct {
	RunID           uint `json:"runId"`
	DryRun          bool `json:"dryRun"`
	Targets         int  `json:"targets"`
	AlreadyReturned bool `json:"alreadyReturned"`
	jobqueue.AssessmentRestoreCounts
}

// ResetBatchRun requires the API/workers to be stopped. It is an operations
// action, not the assessment-result rollback endpoint.
func ResetBatchRun(ctx context.Context, runID uint, dryRun bool) (BatchResetResult, error) {
	result := BatchResetResult{RunID: runID, DryRun: dryRun}
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if tx.Dialector.Name() != "postgres" {
			return fmt.Errorf("Batch reset requires PostgreSQL")
		}
		if err := tx.Exec("SELECT pg_advisory_xact_lock(7845041)").Error; err != nil {
			return err
		}
		var run BatchRun
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&run, runID).Error; err != nil {
			return err
		}
		if run.Status == "rolled_back" {
			result.AlreadyReturned = true
			return nil
		}
		if run.ActiveKey == nil {
			return fmt.Errorf("only an active Batch run can be reset")
		}
		var unsafe int64
		if err := tx.Model(&Batch{}).Where("run_id=? AND (remote_id<>'' OR submitted_at IS NOT NULL OR phase IN ('upload_unknown','submission_unknown','submitted','importing'))", run.ID).Count(&unsafe).Error; err != nil {
			return err
		}
		if unsafe != 0 {
			return fmt.Errorf("Batch reset blocked: %d remotely submitted or uncertain batches", unsafe)
		}
		if err := tx.Table("river_job").Where("state='running' AND kind IN (?,?)", jobqueue.AssessArticleJobKind, jobqueue.BatchTickArgs{}.Kind()).Count(&unsafe).Error; err != nil {
			return err
		}
		if unsafe != 0 {
			return fmt.Errorf("stop assessment workers before Batch reset")
		}
		var items []BatchItem
		if err := tx.Where("run_id=? AND returned_at IS NULL", run.ID).Order("id").Find(&items).Error; err != nil {
			return err
		}
		targets := []jobqueue.AssessmentRestoreTarget{}
		seen := map[string]bool{}
		for _, i := range items {
			if i.MaterialID == 0 && i.CandidateID == 0 {
				return fmt.Errorf("item %d has no reconstructable target", i.ID)
			}
			if seen[i.JobID] {
				continue
			}
			seen[i.JobID] = true
			a := jobqueue.AssessArticleArgs{MaterialID: i.MaterialID, ContentVersion: strconv.FormatUint(uint64(i.ContentVersion), 10)}
			if i.MaterialID == 0 {
				a.CandidateID = i.CandidateID
			}
			targets = append(targets, jobqueue.AssessmentRestoreTarget{OriginalJobID: i.JobID, Args: a})
		}
		// Include jobs suppressed by an already occupied identity, which can have
		// no BatchItem. Older untagged cancelled jobs require manual provenance.
		var extra []struct {
			ID   int64
			Args []byte
		}
		if err := tx.Raw("SELECT id,args FROM river_job WHERE kind=? AND metadata->>'batch_run_id'=? AND state='cancelled' ORDER BY id", jobqueue.AssessArticleJobKind, strconv.FormatUint(uint64(run.ID), 10)).Scan(&extra).Error; err != nil {
			return err
		}
		for _, j := range extra {
			id := strconv.FormatInt(j.ID, 10)
			if seen[id] {
				continue
			}
			var a jobqueue.AssessArticleArgs
			if err := json.Unmarshal(j.Args, &a); err != nil {
				return err
			}
			targets = append(targets, jobqueue.AssessmentRestoreTarget{OriginalJobID: id, Args: a})
		}
		result.Targets = len(targets)
		if dryRun {
			return nil
		}
		counts, err := jobqueue.RestoreAssessmentJobsTx(ctx, db, tx, run.ID, targets)
		if err != nil {
			return err
		}
		result.AssessmentRestoreCounts = counts
		now := time.Now().UTC()
		for start := 0; start < len(targets); start += 500 {
			end := start + 500
			if end > len(targets) {
				end = len(targets)
			}
			values := make([]string, 0, end-start)
			args := []any{now}
			for _, target := range targets[start:end] {
				values = append(values, "(?::text,?::text)")
				args = append(args, target.OriginalJobID, target.RestoredJobID)
			}
			args = append(args, run.ID)
			if err := tx.Exec("UPDATE assessment_batch_items i SET active_identity=NULL,returned_at=?,restored_job_id=r.restored FROM (VALUES "+strings.Join(values, ",")+") AS r(original,restored) WHERE i.run_id=? AND i.job_id=r.original", args...).Error; err != nil {
				return err
			}
		}
		if err := tx.Model(&material.ArticleState{}).Where(`(material_id,content_version) IN (SELECT material_id,content_version FROM assessment_batch_items WHERE run_id=? AND status='failed') AND current_assessment_id IS NULL AND eligibility_reasons='article_assessment_failed'`, run.ID).Updates(map[string]any{"assessment_state": "pending", "assessment_error": "", "eligibility_state": "unknown", "eligibility_reasons": "assessment_pending", "updated_at": now}).Error; err != nil {
			return err
		}
		if err := tx.Model(&Batch{}).Where("run_id=? AND phase<>'done'", run.ID).Updates(map[string]any{"phase": "done", "error": "returned to River before submission", "finished_at": now, "cleaned_at": now, "input_jsonl": nil}).Error; err != nil {
			return err
		}
		return tx.Model(&BatchRun{}).Where("id=?", run.ID).Updates(map[string]any{"status": "rolled_back", "active_key": nil, "lease_until": nil, "finished_at": now, "paused_at": nil, "resume_status": "", "error_code": 0, "error": "all audited tasks returned to River"}).Error
	})
	return result, err
}
