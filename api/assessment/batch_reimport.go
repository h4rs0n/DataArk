package assessment

import (
	"DataArk/config"
	"DataArk/discovery"
	"DataArk/jobqueue"
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"

	"gorm.io/gorm"
)

type BatchReimportResult struct {
	RunID       uint           `json:"runId"`
	DryRun      bool           `json:"dryRun"`
	Batches     int            `json:"batches"`
	Examined    int            `json:"examined"`
	Recoverable int            `json:"recoverable"`
	Restored    int            `json:"restored"`
	Invalid     int            `json:"invalid"`
	Skipped     int            `json:"skipped"`
	Errors      map[string]int `json:"errors"`
}

func reimportableBatchItem(item BatchItem) bool {
	return item.Status == "failed" && item.ReturnedAt == nil && item.ActiveIdentity == nil && item.AssessmentID == nil && strings.HasPrefix(item.Error, "model output")
}

// The material lock held by the caller orders restoration against fresh
// assessments, content updates and dedupe changes.
func batchReimportAllowed(tx *gorm.DB, item BatchItem, candidate discovery.DiscoveryCandidate, allowed bool) (bool, error) {
	if !allowed {
		return false, nil
	}
	var count int64
	if err := tx.Model(&BatchItem{}).Where("material_id=? AND content_version=? AND active_identity IS NOT NULL", item.MaterialID, item.ContentVersion).Count(&count).Error; err != nil || count != 0 {
		return false, err
	}
	if candidate.CurrentAssessmentID != nil {
		var assessment ArticleAssessment
		q := tx.Where("id=? AND material_id=? AND content_version=?", *candidate.CurrentAssessmentID, item.MaterialID, item.ContentVersion).Limit(1).Find(&assessment)
		if q.Error != nil {
			return false, q.Error
		}
		if q.RowsAffected > 0 && isModelAssessment(assessment) {
			return false, nil
		}
	}
	if tx.Dialector.Name() == "postgres" && tx.Migrator().HasTable("river_job") {
		version := strconv.FormatUint(uint64(item.ContentVersion), 10)
		target, _ := json.Marshal(map[string]any{"material_id": item.MaterialID, "content_version": version})
		// Use River's existing args GIN index. An OR with a correlated legacy
		// lookup would scan the whole waiting queue once for every restored row.
		err := tx.Raw(`SELECT sum(n) FROM (
		 SELECT count(*) AS n FROM river_job j WHERE j.kind=?
		 AND j.state='running' AND j.args @> ?::jsonb
		 UNION ALL
		 SELECT count(*) AS n FROM discovery_candidates c
		 JOIN material_candidate_versions cv ON cv.candidate_id=c.id AND cv.content_version=? AND cv.material_id=?
		 JOIN river_job j ON j.args @> jsonb_build_object('candidate_id',cv.candidate_id,'content_version',?::text)
		 WHERE c.material_id=? AND j.kind=? AND j.state='running'
		 ) live`, jobqueue.AssessArticleJobKind, string(target), item.ContentVersion, item.MaterialID, version, item.MaterialID, jobqueue.AssessArticleJobKind).Scan(&count).Error
		if err != nil || count != 0 {
			return false, err
		}
	}
	return true, nil
}

// ReimportBatchRun reinterprets existing completed outputs only. It never
// uploads, creates a provider task, requeues articles or starts a Worker.
func ReimportBatchRun(ctx context.Context, runID uint, dryRun bool) (BatchReimportResult, error) {
	stats := BatchReimportResult{RunID: runID, DryRun: dryRun, Errors: map[string]int{}}
	if db == nil || runID == 0 {
		return stats, fmt.Errorf("assessment database and explicit run ID are required")
	}
	var run BatchRun
	if err := db.WithContext(ctx).First(&run, runID).Error; err != nil {
		return stats, err
	}
	if run.Status == "rolled_back" || run.Status == "paused" {
		return stats, fmt.Errorf("returned or paused Batch runs cannot be reimported")
	}
	cleanup, err := pausedBatchCleanup(ctx)
	if err != nil {
		return stats, err
	}
	if cleanup != nil {
		return stats, fmt.Errorf("Batch file cleanup is paused")
	}
	if strings.TrimRight(config.LLMBATCHBASEURL, "/") != run.BaseURL || strings.TrimSpace(config.LLMBATCHAPIKEY) == "" {
		return stats, fmt.Errorf("reimport requires the original Batch base URL and account key")
	}
	var batches []Batch
	err = db.WithContext(ctx).Omit("input_jsonl").Where(`run_id=? AND phase='done' AND output_file_id<>'' AND id IN
	 (SELECT batch_id FROM assessment_batch_items WHERE run_id=? AND status='failed' AND returned_at IS NULL
	 AND active_identity IS NULL AND assessment_id IS NULL AND error LIKE 'model output%')`, runID, runID).Order("id").Find(&batches).Error
	if err != nil {
		return stats, err
	}
	p := batchProviderFactory(run.BaseURL)
	for _, batch := range batches {
		// Respect a pause recorded by the running API between file downloads.
		var current BatchRun
		if err := db.WithContext(ctx).First(&current, run.ID).Error; err != nil {
			return stats, err
		}
		if current.Status == "paused" || current.Status == "rolled_back" {
			return stats, fmt.Errorf("Batch reimport stopped because the run is paused or returned")
		}
		cleanup, err := pausedBatchCleanup(ctx)
		if err != nil {
			return stats, err
		}
		if cleanup != nil {
			return stats, fmt.Errorf("Batch reimport stopped because cleanup is paused")
		}
		reader, err := p.Download(ctx, batch.OutputFileID)
		if err != nil {
			if !dryRun && current.ActiveKey != nil && batchPauseCode(err) != 0 {
				if pauseErr := pauseBatchRun(ctx, &current, &batch, batch.Phase, batchPauseCode(err), err); pauseErr != nil {
					return stats, pauseErr
				}
			}
			return stats, fmt.Errorf("batch %d output download failed: %s", batch.ID, safeBatchError(err))
		}
		err = reimportBatchStream(ctx, &run, &batch, reader, &stats)
		reader.Close()
		if err != nil {
			if !dryRun && current.ActiveKey != nil && batchPauseCode(err) != 0 {
				if pauseErr := pauseBatchRun(ctx, &current, &batch, batch.Phase, batchPauseCode(err), err); pauseErr != nil {
					return stats, pauseErr
				}
			}
			return stats, err
		}
		stats.Batches++
	}
	return stats, nil
}

func reimportBatchStream(ctx context.Context, run *BatchRun, batch *Batch, reader io.Reader, stats *BatchReimportResult) error {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 65536), 6<<20)
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return err
		}
		if len(strings.TrimSpace(scanner.Text())) == 0 {
			continue
		}
		var out batchOutput
		if json.Unmarshal(scanner.Bytes(), &out) != nil {
			return fmt.Errorf("invalid Batch result JSONL")
		}
		var item BatchItem
		q := db.WithContext(ctx).Where("batch_id=? AND custom_id=?", batch.ID, out.CustomID).Limit(1).Find(&item)
		if q.Error != nil {
			return q.Error
		}
		if q.RowsAffected == 0 {
			return fmt.Errorf("Batch result contains an unknown custom_id")
		}
		if !reimportableBatchItem(item) {
			continue
		}
		stats.Examined++
		problems := batchProviderProblems(out.Error)
		if out.Response != nil {
			problems = append(problems, batchProviderProblems(out.Response.Body.Error)...)
			if out.Response.Status == 401 || out.Response.Status == 429 {
				return batchHTTPError{Status: out.Response.Status}
			}
		}
		if err := batchProblemPause(problems); err != nil {
			return err
		}
		result, message := decodeBatchOutput(out)
		if result == nil {
			stats.Invalid++
			stats.Errors[message]++
			if !stats.DryRun {
				// Preserve failure and the original paid-call accounting, but show
				// the precise validation cause instead of the old generic message.
				if _, err := finishBatchItemResult(ctx, run, item, "failed", nil, message, nil, true); err != nil {
					return err
				}
			}
			continue
		}
		if stats.DryRun {
			candidate, allowed, err := batchCandidate(db.WithContext(ctx), item)
			if err != nil {
				return err
			}
			ok, err := batchReimportAllowed(db.WithContext(ctx), item, candidate, allowed)
			if err != nil {
				return err
			}
			if !ok {
				stats.Skipped++
				continue
			}
			stats.Recoverable++
			continue
		}
		applied, err := finishBatchItemResult(ctx, run, item, "succeeded", result, "", &out.Response.Body.Usage, true)
		if err != nil {
			return err
		}
		if applied {
			stats.Restored++
		} else {
			stats.Skipped++
		}
	}
	if scanner.Err() != nil {
		return fmt.Errorf("Batch result read failed or exceeded line limit")
	}
	return nil
}
