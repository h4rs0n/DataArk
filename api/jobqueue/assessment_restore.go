package jobqueue

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverdatabasesql"
	"gorm.io/gorm"
)

type AssessmentRestoreTarget struct {
	OriginalJobID string
	Args          AssessArticleArgs
	RestoredJobID string
}

type AssessmentRestoreCounts struct {
	Restored  int `json:"restored"`
	Recreated int `json:"recreated"`
	Reused    int `json:"reused"`
}

func assessmentRestoreKey(material, candidate uint, version string) string {
	if material != 0 {
		return fmt.Sprintf("m:%d:%s", material, version)
	}
	return fmt.Sprintf("c:%d:%s", candidate, version)
}

// RestoreAssessmentJobsTx reuses live identities, restores surviving original
// jobs, and recreates jobs removed by River retention. It never starts workers.
func RestoreAssessmentJobsTx(ctx context.Context, database, tx *gorm.DB, runID uint, targets []AssessmentRestoreTarget) (AssessmentRestoreCounts, error) {
	var counts AssessmentRestoreCounts
	sqlTx, ok := tx.Statement.ConnPool.(*sql.Tx)
	if !ok {
		return counts, fmt.Errorf("assessment restore requires PostgreSQL transaction")
	}
	sqlDB, err := database.DB()
	if err != nil {
		return counts, err
	}
	client, err := river.NewClient(riverdatabasesql.New(sqlDB), &river.Config{})
	if err != nil {
		return counts, err
	}
	type activeJob struct {
		ID             int64
		MaterialID     uint
		CandidateID    uint
		ContentVersion string
	}
	var live []activeJob
	err = tx.Raw(`SELECT j.id, COALESCE(NULLIF((j.args->>'material_id')::bigint,0),cv.material_id,0) AS material_id,
		COALESCE((j.args->>'candidate_id')::bigint,0) AS candidate_id, j.args->>'content_version' AS content_version
		FROM river_job j LEFT JOIN material_candidate_versions cv ON cv.candidate_id=(j.args->>'candidate_id')::bigint
		AND cv.content_version=(j.args->>'content_version')::bigint
		WHERE j.kind=? AND j.state IN ('available','pending','scheduled','retryable','running')`, AssessArticleJobKind).Scan(&live).Error
	if err != nil {
		return counts, err
	}
	active := make(map[string]string, len(live))
	for _, j := range live {
		active[assessmentRestoreKey(j.MaterialID, j.CandidateID, j.ContentVersion)] = strconv.FormatInt(j.ID, 10)
	}
	type originalJob struct {
		ID    int64
		Kind  string
		State string
		Args  []byte
	}
	ids := make([]int64, 0, len(targets))
	for _, target := range targets {
		id, err := strconv.ParseInt(target.OriginalJobID, 10, 64)
		if err != nil {
			return counts, fmt.Errorf("invalid original River job ID")
		}
		ids = append(ids, id)
	}
	originals := map[int64]originalJob{}
	for start := 0; start < len(ids); start += 500 {
		end := start + 500
		if end > len(ids) {
			end = len(ids)
		}
		var rows []originalJob
		if err := tx.Raw("SELECT id,kind,state,args FROM river_job WHERE id IN ? FOR UPDATE", ids[start:end]).Scan(&rows).Error; err != nil {
			return counts, err
		}
		for _, row := range rows {
			originals[row.ID] = row
		}
	}
	for n := range targets {
		t := &targets[n]
		key := assessmentRestoreKey(t.Args.MaterialID, t.Args.CandidateID, t.Args.ContentVersion)
		if id, found := active[key]; found {
			t.RestoredJobID = id
			counts.Reused++
			continue
		}
		id := ids[n]
		original := originals[id]
		if original.ID != 0 {
			if original.Kind != AssessArticleJobKind || original.State != "cancelled" {
				return counts, fmt.Errorf("original River job %d cannot be restored", id)
			}
			var args AssessArticleArgs
			if err := json.Unmarshal(original.Args, &args); err != nil {
				return counts, err
			}
			if args.ContentVersion != t.Args.ContentVersion {
				return counts, fmt.Errorf("original job version mismatch")
			}
			if err := tx.Exec(`UPDATE river_job SET metadata=(metadata-'cancel_attempted_at') || jsonb_build_object('batch_restored_run_id',?::bigint), queue=? WHERE id=?`, runID, AssessmentQueueName, id).Error; err != nil {
				return counts, err
			}
			job, err := client.JobRetryTx(ctx, sqlTx, id)
			if err != nil {
				return counts, err
			}
			t.RestoredJobID = strconv.FormatInt(job.ID, 10)
			counts.Restored++
		} else {
			metadata, _ := json.Marshal(map[string]any{"batch_restored_run_id": runID, "batch_original_job_id": t.OriginalJobID})
			job, err := client.InsertTx(ctx, sqlTx, t.Args, &river.InsertOpts{Metadata: metadata})
			if err != nil {
				return counts, err
			}
			t.RestoredJobID = strconv.FormatInt(job.Job.ID, 10)
			if job.UniqueSkippedAsDuplicate {
				counts.Reused++
			} else {
				counts.Recreated++
			}
		}
		active[key] = t.RestoredJobID
	}
	return counts, nil
}
