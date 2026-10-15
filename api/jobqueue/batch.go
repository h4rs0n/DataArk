package jobqueue

import (
	"context"
	"fmt"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
	"time"
)

const BatchQueueName = "assessment_batch"

// Different wake-up times allow a running worker to schedule its successor.
// River requires running in the uniqueness states. Reusing empty args would
// silently reuse the current job instead of creating the next wake-up.
type BatchTickArgs struct {
	At int64 `json:"at"`
}

func (BatchTickArgs) Kind() string { return "assessment_batch_tick" }
func (BatchTickArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: BatchQueueName, MaxAttempts: 25, UniqueOpts: river.UniqueOpts{ByArgs: true, ByState: []rivertype.JobState{rivertype.JobStateAvailable, rivertype.JobStatePending, rivertype.JobStateScheduled, rivertype.JobStateRetryable, rivertype.JobStateRunning}}}
}

type batchTickWorker struct {
	river.WorkerDefaults[BatchTickArgs]
	handler func(context.Context) error
}

func (w *batchTickWorker) Timeout(*river.Job[BatchTickArgs]) time.Duration { return 5 * time.Minute }
func (w *batchTickWorker) Work(ctx context.Context, _ *river.Job[BatchTickArgs]) error {
	if w.handler == nil {
		return ErrHandlerUnavailable
	}
	return w.handler(ctx)
}
func (q *riverQueue) EnqueueBatchTick(ctx context.Context, at time.Time) error {
	_, err := q.client.Insert(ctx, BatchTickArgs{At: at.Unix()}, &river.InsertOpts{ScheduledAt: at})
	return err
}
func (q *MemoryQueue) EnqueueBatchTick(ctx context.Context, at time.Time) error {
	if q.handlers.BatchTick == nil {
		return fmt.Errorf("%w: Batch", ErrHandlerUnavailable)
	}
	go func() {
		timer := time.NewTimer(time.Until(at))
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		q.store.mu.Lock()
		stopped := q.stopped
		q.store.mu.Unlock()
		if !stopped {
			_ = q.handlers.BatchTick(ctx)
		}
	}()
	return nil
}
