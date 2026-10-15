package jobqueue

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"gorm.io/gorm"
	"sort"
	"strconv"
	"time"
)

// AssessmentTransfer moves a fixed queue prefix into a durable async owner.
// accept and River cancellation commit together. Ordinary workers stay paused.
type batchRunContextKey struct{}

// WithBatchRun tags the original River job in the same transfer transaction.
func WithBatchRun(ctx context.Context, runID uint) context.Context {
	return context.WithValue(ctx, batchRunContextKey{}, runID)
}

type AssessmentTransfer interface {
	AssessmentHighWater(context.Context) (int64, error)
	TransferAssessments(context.Context, int64, int64, time.Time, int, func(*gorm.DB, []AssessmentJob) error) (int64, bool, error)
	EnqueueBatchTick(context.Context, time.Time) error
}
type AssessmentJob struct {
	Cursor         int64
	ID             string
	MaterialID     uint
	CandidateID    uint
	ContentVersion string
}

func (q *riverQueue) AssessmentHighWater(ctx context.Context) (int64, error) {
	var maxID int64
	err := q.db.QueryRowContext(ctx, "SELECT COALESCE(MAX(id),0) FROM river_job WHERE kind=$1", AssessArticleJobKind).Scan(&maxID)
	return maxID, err
}
func (q *riverQueue) TransferAssessments(ctx context.Context, highWater, cursor int64, cutoff time.Time, limit int, accept func(*gorm.DB, []AssessmentJob) error) (int64, bool, error) {
	if limit < 1 {
		return cursor, false, fmt.Errorf("assessment transfer limit must be positive")
	}
	next := cursor
	count := 0
	err := q.database.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		sqlTx, ok := tx.Statement.ConnPool.(*sql.Tx)
		if !ok {
			return fmt.Errorf("assessment transfer requires SQL transaction")
		}
		rows, err := sqlTx.QueryContext(ctx, `SELECT id,args FROM river_job WHERE kind=$1 AND id>$2 AND id<=$3 AND state IN ('available','pending','scheduled','retryable') AND scheduled_at<=$4 ORDER BY id LIMIT $5 FOR UPDATE`, AssessArticleJobKind, cursor, highWater, cutoff, limit)
		if err != nil {
			return err
		}
		jobs := []AssessmentJob{}
		for rows.Next() {
			var id int64
			var args []byte
			if err := rows.Scan(&id, &args); err != nil {
				rows.Close()
				return err
			}
			var a AssessArticleArgs
			if err := json.Unmarshal(args, &a); err != nil {
				rows.Close()
				return err
			}
			jobs = append(jobs, AssessmentJob{Cursor: id, ID: strconv.FormatInt(id, 10), MaterialID: a.MaterialID, CandidateID: a.CandidateID, ContentVersion: a.ContentVersion})
			next = id
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		count = len(jobs)
		if err := accept(tx, jobs); err != nil {
			return err
		}
		for _, j := range jobs {
			id, _ := strconv.ParseInt(j.ID, 10, 64)
			if err := tx.Exec(`UPDATE river_job SET metadata=metadata || jsonb_build_object('batch_run_id', ?::bigint) WHERE id=?`, ctx.Value(batchRunContextKey{}), id).Error; err != nil {
				return err
			}
			if _, err := q.client.JobCancelTx(ctx, sqlTx, id); err != nil {
				return err
			}
		}
		return nil
	})
	return next, count == 0, err
}
func (q *MemoryQueue) AssessmentHighWater(context.Context) (int64, error) {
	return time.Now().UnixNano(), nil
}
func (q *MemoryQueue) TransferAssessments(ctx context.Context, highWater, cursor int64, cutoff time.Time, limit int, accept func(*gorm.DB, []AssessmentJob) error) (int64, bool, error) {
	q.store.mu.Lock()
	defer q.store.mu.Unlock()
	jobs := []MemoryJob{}
	for _, j := range q.store.jobs {
		if j.Kind == AssessArticleJobKind && j.Status == MemoryJobPending && j.CreatedAt.UnixNano() > cursor && j.CreatedAt.UnixNano() <= highWater {
			jobs = append(jobs, j)
		}
	}
	sort.Slice(jobs, func(i, j int) bool { return jobs[i].CreatedAt.Before(jobs[j].CreatedAt) })
	if len(jobs) > limit {
		jobs = jobs[:limit]
	}
	if limit < 1 {
		return cursor, false, fmt.Errorf("assessment transfer limit must be positive")
	}
	next := cursor
	targets := []AssessmentJob{}
	for _, j := range jobs {
		targets = append(targets, AssessmentJob{Cursor: j.CreatedAt.UnixNano(), ID: j.Key, MaterialID: j.TargetID, ContentVersion: j.ContentVersion})
		next = j.CreatedAt.UnixNano()
	}
	if q.database == nil {
		return cursor, false, fmt.Errorf("assessment transfer database unavailable")
	}
	if err := q.database.WithContext(ctx).Transaction(func(tx *gorm.DB) error { return accept(tx, targets) }); err != nil {
		return cursor, false, err
	}
	for _, j := range jobs {
		j.Status = "cancelled"
		j.LastError = "transferred to Batch"
		q.store.jobs[j.Key] = j
	}
	return next, len(jobs) == 0, nil
}
