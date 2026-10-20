package assessment

import (
	"DataArk/config"
	"DataArk/jobqueue"
	"context"
	"errors"
	"fmt"
	"gorm.io/gorm"
	"time"
)

type QueueSnapshot struct {
	*jobqueue.CrawlQueueSnapshot
	ExecutionMode string             `json:"executionMode"`
	BatchRun      *BatchRun          `json:"batchRun,omitempty"`
	BatchCleanup  *BatchCleanupPause `json:"batchCleanup,omitempty"`
}

func BatchRunCounts(ctx context.Context, run *BatchRun) error {
	run.Active = run.ActiveKey != nil
	run.Counts = BatchCounts{}
	type count struct {
		Status string
		N      int64
	}
	var rows []count
	if err := db.WithContext(ctx).Model(&BatchItem{}).Select("CASE WHEN returned_at IS NOT NULL THEN 'returned' ELSE status END AS status,COUNT(*) AS n").Where("run_id=?", run.ID).Group("CASE WHEN returned_at IS NOT NULL THEN 'returned' ELSE status END").Scan(&rows).Error; err != nil {
		return err
	}
	for _, r := range rows {
		run.Counts.Total += r.N
		switch r.Status {
		case "returned":
			run.Counts.Returned += r.N
		case "pending":
			run.Counts.Pending += r.N
		case "processing":
			run.Counts.Processing += r.N
		case "succeeded":
			run.Counts.Succeeded += r.N
		case "failed":
			run.Counts.Failed += r.N
		case "reused":
			run.Counts.Reused += r.N
		case "skipped":
			run.Counts.Skipped += r.N
		}
	}
	return nil
}
func DecorateQueue(ctx context.Context, snapshot *jobqueue.CrawlQueueSnapshot) (*QueueSnapshot, error) {
	mode := "realtime"
	if config.BatchEnabled() {
		mode = "batch"
	}
	out := &QueueSnapshot{CrawlQueueSnapshot: snapshot, ExecutionMode: mode}
	if db == nil || !db.Migrator().HasTable(&BatchRun{}) {
		return out, nil
	}
	cleanup, err := pausedBatchCleanup(ctx)
	if err != nil {
		return nil, err
	}
	out.BatchCleanup = cleanup
	if cleanup != nil && config.BatchEnabled() {
		snapshot.State = "paused"
		snapshot.CanRun = false
	}
	var run BatchRun
	err = db.WithContext(ctx).Where("status<>'rolled_back'").Order("CASE WHEN active_key=1 THEN 0 ELSE 1 END,id DESC").First(&run).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	if err := BatchRunCounts(ctx, &run); err != nil {
		return nil, err
	}
	out.BatchRun = &run
	if config.BatchEnabled() {
		snapshot.Counts.Succeeded24h = int(run.Counts.Succeeded + run.Counts.Reused)
		snapshot.Counts.Failed24h = int(run.Counts.Failed)
	}
	if run.ActiveKey != nil && config.BatchEnabled() {
		snapshot.State = "running"
		snapshot.CanRun = false
		if run.Status == "paused" || !run.AdmissionEnabled {
			snapshot.State = "paused"
			snapshot.CanRun = true
		}
		snapshot.RunStartedAt = &run.CreatedAt
		snapshot.Counts.Running += int(run.Counts.Processing + run.Counts.Pending)
	}
	if cleanup != nil && config.BatchEnabled() {
		snapshot.State = "paused"
		snapshot.CanRun = false
	}
	return out, nil
}
func BatchMetrics(ctx context.Context, now time.Time) (*Metrics, bool, error) {
	if !config.BatchEnabled() {
		return nil, false, nil
	}
	controller, ok := jobqueue.AssessmentControl()
	if !ok {
		return &Metrics{GeneratedAt: now}, true, nil
	}
	snapshot, err := controller.Snapshot(ctx, 1)
	if err != nil {
		return nil, true, err
	}
	out, err := DecorateQueue(ctx, snapshot)
	if err != nil {
		return nil, true, err
	}
	metrics := &Metrics{GeneratedAt: now, PendingQueue: int64(snapshot.Counts.Pending)}
	if out.BatchRun != nil {
		r := out.BatchRun
		metrics.CurrentRun = MetricsWindow{Success: r.Counts.Succeeded + r.Counts.Reused, Failure: r.Counts.Failed}
		var totals MetricsTokens
		err = db.WithContext(ctx).Model(&BatchItem{}).Where("run_id=?", r.ID).Select("COALESCE(SUM(prompt_tokens),0) AS prompt,COALESCE(SUM(completion_tokens),0) AS completion,COALESCE(SUM(reasoning_tokens),0) AS reasoning,COALESCE(SUM(total_tokens),0) AS total").Scan(&totals).Error
		metrics.TokenTotals = totals
	}
	return metrics, true, err
}
func ListBatches(ctx context.Context, limit int, before uint) ([]Batch, error) {
	if db == nil {
		return nil, fmt.Errorf("assessment database unavailable")
	}
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	q := db.WithContext(ctx).Omit("input_jsonl").Where("run_id IN (SELECT id FROM assessment_batch_runs WHERE status<>'rolled_back')").Order("id DESC").Limit(limit)
	if before > 0 {
		q = q.Where("id<?", before)
	}
	rows := []Batch{}
	err := q.Find(&rows).Error
	return rows, err
}

type BatchDetail struct {
	Batch  Batch       `json:"batch"`
	Items  []BatchItem `json:"items"`
	NextID uint        `json:"nextId"`
}

func GetBatchDetail(ctx context.Context, id uint, limit int, after uint) (*BatchDetail, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	out := &BatchDetail{Items: []BatchItem{}}
	if err := db.WithContext(ctx).Omit("input_jsonl").Where("run_id IN (SELECT id FROM assessment_batch_runs WHERE status<>'rolled_back')").First(&out.Batch, id).Error; err != nil {
		return nil, err
	}
	if err := db.WithContext(ctx).Where("batch_id=? AND id>? AND status='failed' AND returned_at IS NULL", id, after).Order("id").Limit(limit).Find(&out.Items).Error; err != nil {
		return nil, err
	}
	if len(out.Items) == limit {
		out.NextID = out.Items[len(out.Items)-1].ID
	}
	return out, nil
}
