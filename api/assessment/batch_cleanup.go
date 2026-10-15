package assessment

import (
	"context"
	"fmt"
	"log"
	"time"

	"gorm.io/gorm"
)

type BatchCleanupPause struct {
	RunID     uint       `json:"runId"`
	ErrorCode int        `json:"errorCode"`
	Error     string     `json:"error"`
	PausedAt  *time.Time `json:"pausedAt"`
}

func pausedBatchCleanup(ctx context.Context) (*BatchCleanupPause, error) {
	if db == nil {
		return nil, fmt.Errorf("assessment database unavailable")
	}
	var run BatchRun
	q := db.WithContext(ctx).Where("resume_status='cleanup' AND paused_at IS NOT NULL").Order("id DESC").Limit(1).Find(&run)
	if q.Error != nil || q.RowsAffected == 0 {
		return nil, q.Error
	}
	return &BatchCleanupPause{RunID: run.ID, ErrorCode: run.ErrorCode, Error: run.Error, PausedAt: run.PausedAt}, nil
}

func pauseBatchCleanup(ctx context.Context, run *BatchRun, b *Batch, cause error) error {
	now := time.Now().UTC()
	message := safeBatchError(cause) + "; file cleanup paused until manually resumed"
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Preserve the inference error on the batch; cleanup diagnostics live on
		// the run so a failed delete cannot overwrite result-validation details.
		return tx.Model(&BatchRun{}).Where("id=?", run.ID).Updates(map[string]any{"resume_status": "cleanup", "paused_at": now, "error_code": batchPauseCode(cause), "error": message}).Error
	})
	if err == nil {
		log.Printf("assessment Batch cleanup for run %d batch %d paused: %s", run.ID, b.ID, message)
	}
	return err
}

// ResumeBatchCleanup schedules file deletion only. It never starts a new inference run
// or changes the final status of a completed/returned run.
func ResumeBatchCleanup(ctx context.Context) error {
	if db == nil {
		return fmt.Errorf("assessment database unavailable")
	}
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return tx.Model(&BatchRun{}).Where("resume_status='cleanup' AND paused_at IS NOT NULL").Updates(map[string]any{"resume_status": "", "paused_at": nil, "error_code": 0, "error": ""}).Error
	})
	if err != nil {
		return err
	}
	t, err := batchTransfer()
	if err != nil {
		return err
	}
	return t.EnqueueBatchTick(ctx, time.Now())
}
