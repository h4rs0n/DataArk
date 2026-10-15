package jobqueue

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
)

const DedupeJobKind = "article_dedupe"
const DedupeQueueName = "article_dedupe"

type DedupeArgs struct {
	RunID            uint   `json:"run_id,omitempty"`
	RepresentationID uint   `json:"representation_id,omitempty"`
	Algorithm        string `json:"algorithm"`
	Phase            string `json:"phase,omitempty"`
	Cursor           uint   `json:"cursor,omitempty"`
	// AttemptKey permits repair after a completed task when its coverage is invalidated.
	AttemptKey string `json:"attempt_key,omitempty"`
}

func (DedupeArgs) Kind() string { return DedupeJobKind }
func (DedupeArgs) InsertOpts() river.InsertOpts {
	opts := uniqueByArgsOpts()
	opts.Queue = DedupeQueueName
	opts.UniqueOpts.ByState = []rivertype.JobState{rivertype.JobStateAvailable, rivertype.JobStatePending, rivertype.JobStateRunning, rivertype.JobStateRetryable, rivertype.JobStateScheduled}
	return opts
}

type DedupeEnqueuer interface {
	EnqueueDedupe(context.Context, DedupeArgs) error
}
type AssessmentPruner interface {
	PruneAssessments(context.Context, func([]AssessmentTarget) (map[uint]bool, error)) (int64, error)
}

func (q *riverQueue) EnqueueDedupe(ctx context.Context, args DedupeArgs) error {
	_, err := q.client.Insert(ctx, args, nil)
	return err
}

type dedupeWorker struct {
	river.WorkerDefaults[DedupeArgs]
	handler func(context.Context, DedupeArgs) error
}

func (w *dedupeWorker) Timeout(*river.Job[DedupeArgs]) time.Duration { return 10 * time.Minute }
func (w *dedupeWorker) Work(ctx context.Context, job *river.Job[DedupeArgs]) error {
	if w.handler == nil {
		return fmt.Errorf("%w: %s", ErrHandlerUnavailable, DedupeJobKind)
	}
	return w.handler(ctx, job.Args)
}
func (q *riverQueue) PruneAssessments(ctx context.Context, skip func([]AssessmentTarget) (map[uint]bool, error)) (int64, error) {
	var total int64
	var cursor int64
	for {
		rows, err := q.db.QueryContext(ctx, `SELECT id,args FROM river_job WHERE id>$1 AND kind=$2 AND state IN ('available','pending','scheduled','retryable') ORDER BY id LIMIT 250`, cursor, AssessArticleJobKind)
		if err != nil {
			return total, err
		}
		type target struct {
			id   int64
			args AssessArticleArgs
		}
		batch := []target{}
		for rows.Next() {
			var id int64
			var encoded []byte
			if err := rows.Scan(&id, &encoded); err != nil {
				rows.Close()
				return total, err
			}
			var args AssessArticleArgs
			if err := json.Unmarshal(encoded, &args); err != nil {
				rows.Close()
				return total, err
			}
			batch = append(batch, target{id, args})
			cursor = id
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return total, err
		}
		if len(batch) == 0 {
			return total, nil
		}
		targets := make([]AssessmentTarget, 0, len(batch))
		for _, target := range batch {
			if target.args.MaterialID != 0 {
				targets = append(targets, AssessmentTarget{MaterialID: target.args.MaterialID, ContentVersion: target.args.ContentVersion})
			}
		}
		suppressed, err := skip(targets)
		if err != nil {
			return total, err
		}
		for _, target := range batch {
			if !suppressed[target.args.MaterialID] {
				continue
			}
			cancelled, err := q.cancelSupersededAssessment(ctx, target.id)
			if err != nil {
				return total, err
			}
			if cancelled {
				total++
			}

		}
	}
}
func (q *MemoryQueue) EnqueueDedupe(ctx context.Context, args DedupeArgs) error {
	encoded, _ := json.Marshal(args)
	key := fmt.Sprintf("%s:%s", DedupeJobKind, encoded)
	q.store.mu.Lock()
	existing, ok := q.store.jobs[key]
	if ok && (existing.Status == MemoryJobRunning || existing.Status == MemoryJobPending) {
		q.store.mu.Unlock()
		return nil
	}
	now := time.Now()
	job := MemoryJob{Key: key, Kind: DedupeJobKind, TargetType: "representation", TargetID: args.RepresentationID, Status: MemoryJobRunning, Attempts: existing.Attempts + 1, CreatedAt: now, UpdatedAt: now}
	q.store.jobs[key] = job
	q.store.mu.Unlock()
	go func() {
		var err error
		if q.handlers.Dedupe == nil {
			err = ErrHandlerUnavailable
		} else {
			err = q.handlers.Dedupe(context.Background(), args)
		}
		q.finish(key, err)
	}()
	return nil
}
func (q *MemoryQueue) PruneAssessments(ctx context.Context, skip func([]AssessmentTarget) (map[uint]bool, error)) (int64, error) {
	var count int64
	jobs := q.store.Snapshot()
	for start := 0; start < len(jobs); start += 250 {
		end := min(start+250, len(jobs))
		targets := []AssessmentTarget{}
		for _, job := range jobs[start:end] {
			if job.Kind == AssessArticleJobKind && job.Status == MemoryJobPending && job.TargetID != 0 {
				targets = append(targets, AssessmentTarget{MaterialID: job.TargetID, ContentVersion: job.ContentVersion})
			}
		}
		if len(targets) == 0 {
			continue
		}
		suppressed, err := skip(targets)
		if err != nil {
			return count, err
		}
		q.store.mu.Lock()
		for _, job := range jobs[start:end] {
			if job.Kind != AssessArticleJobKind || job.Status != MemoryJobPending || !suppressed[job.TargetID] {
				continue
			}
			current := q.store.jobs[job.Key]
			if current.Status == MemoryJobPending {
				current.Status = "dedupe_skipped"
				now := time.Now()
				current.FinishedAt = &now
				q.store.jobs[job.Key] = current
				count++
			}
		}
		q.store.mu.Unlock()
	}
	return count, nil
}

func (q *riverQueue) cancelSupersededAssessment(ctx context.Context, id int64) (bool, error) {
	tx, err := q.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var state string
	if err := tx.QueryRowContext(ctx, "SELECT state FROM river_job WHERE id=$1 FOR UPDATE", id).Scan(&state); err != nil {
		return false, err
	}
	if state != "available" && state != "pending" && state != "scheduled" && state != "retryable" {
		return false, nil
	}
	if _, err := tx.ExecContext(ctx, `UPDATE river_job SET metadata=metadata || '{"dedupe_superseded":true}'::jsonb WHERE id=$1`, id); err != nil {
		return false, err
	}
	job, err := q.client.JobCancelTx(ctx, tx, id)
	if err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return string(job.State) == "cancelled", nil
}

// Restrict live-target lookup to this insertion chunk instead of repeatedly reading
// the entire assessment backlog during paginated recovery.
func liveAssessmentTargetsFor(ctx context.Context, database *sql.DB, targets []AssessmentTarget) (map[AssessmentTarget]bool, error) {
	parameters := []interface{}{AssessArticleJobKind}
	placeholders := make([]string, 0, len(targets))
	for _, target := range targets {
		parameters = append(parameters, fmt.Sprint(target.MaterialID))
		placeholders = append(placeholders, fmt.Sprintf("$%d", len(parameters)))
	}
	rows, err := database.QueryContext(ctx, `SELECT COALESCE((args->>'material_id')::bigint,0),args->>'content_version' FROM river_job WHERE kind=$1
 AND COALESCE((args->>'candidate_id')::bigint,0)=0 AND state IN ('available','pending','scheduled','retryable','running')
 AND args->>'material_id' IN (`+strings.Join(placeholders, ",")+`)`, parameters...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := map[AssessmentTarget]bool{}
	for rows.Next() {
		var id uint
		var version sql.NullString
		if err := rows.Scan(&id, &version); err != nil {
			return nil, err
		}
		result[AssessmentTarget{MaterialID: id, ContentVersion: version.String}] = true
	}
	return result, rows.Err()
}
