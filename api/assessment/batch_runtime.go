package assessment

import (
	"DataArk/config"
	"context"
	"sync"

	"gorm.io/gorm"
)

var batchRuntime struct {
	sync.Mutex
	database       *gorm.DB
	admissionReset bool
}

// InitializeBatchRuntime is called before starting workers. A failed reset
// blocks only Batch and is retried by its guarded entry points.
func InitializeBatchRuntime(ctx context.Context) error {
	batchRuntime.Lock()
	batchRuntime.database, batchRuntime.admissionReset = db, false
	batchRuntime.Unlock()
	return BatchRuntimeReady(ctx)
}

func BatchRuntimeReady(ctx context.Context) error {
	batchRuntime.Lock()
	if batchRuntime.database == db && !batchRuntime.admissionReset {
		if err := ResetBatchAdmission(ctx); err != nil {
			batchRuntime.Unlock()
			return err
		}
		batchRuntime.admissionReset = true
	}
	batchRuntime.Unlock()
	if err := config.ValidateBatchLLM(); err != nil {
		return err
	}
	return ValidateBatchRecovery(ctx)
}
