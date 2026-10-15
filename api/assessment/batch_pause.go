package assessment

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func batchPauseCode(err error) int {
	var stop batchStopError
	if errors.As(err, &stop) {
		return stop.Status
	}
	var h batchHTTPError
	if errors.As(err, &h) && (h.Status == 401 || h.Status == 429) {
		return h.Status
	}
	return 0
}

func pauseBatchRun(ctx context.Context, run *BatchRun, b *Batch, phase string, code int, cause error) error {
	now := time.Now().UTC()
	message := safeBatchError(cause) + "; assessment paused until manually resumed"
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&Batch{}).Where("id=?", b.ID).Updates(map[string]any{"phase": phase, "error": message}).Error; err != nil {
			return err
		}
		return tx.Model(&BatchRun{}).Where("id=? AND active_key=1", run.ID).Updates(map[string]any{"status": "paused", "resume_status": run.Status, "paused_at": now, "error_code": code, "error": message}).Error
	})
	if err == nil {
		run.ResumeStatus, run.Status, run.PausedAt, run.ErrorCode, run.Error = run.Status, "paused", &now, code, message
		log.Printf("assessment Batch run %d paused: %s", run.ID, message)
	}
	return err
}

// ResumeBatchRun is only called by the owner's explicit queue/run request.
// Recovery and deferred dedupe release use StartBatchRun, which cannot resume.
func ResumeBatchRun(ctx context.Context) (bool, error) {
	var resumed bool
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var run BatchRun
		q := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("active_key=1 AND status='paused'").Limit(1).Find(&run)
		if q.Error != nil || q.RowsAffected == 0 {
			return q.Error
		}
		if err := ValidateBatchRecovery(ctx); err != nil {
			return err
		}
		status := run.ResumeStatus
		if status != "preparing" && status != "processing" {
			return fmt.Errorf("invalid Batch resume status")
		}
		if err := tx.Model(&run).Updates(map[string]any{"status": status, "resume_status": "", "paused_at": nil, "error_code": 0, "error": ""}).Error; err != nil {
			return err
		}
		if err := tx.Model(&Batch{}).Where("run_id=? AND phase<>'done'", run.ID).Updates(map[string]any{"failure_count": 0, "next_check_at": time.Now().UTC()}).Error; err != nil {
			return err
		}
		resumed = true
		return nil
	})
	if err != nil || !resumed {
		return resumed, err
	}
	t, err := batchTransfer()
	if err != nil {
		return true, err
	}
	return true, t.EnqueueBatchTick(context.Background(), time.Now())
}
