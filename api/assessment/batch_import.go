package assessment

import (
	"DataArk/articlevalue"
	"DataArk/config"
	"DataArk/discovery"
	"DataArk/llm"
	"DataArk/material"
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"io"
	"strings"
	"time"
)

type batchUsage struct {
	Prompt     int `json:"prompt_tokens"`
	Completion int `json:"completion_tokens"`
	Total      int `json:"total_tokens"`
	Details    struct {
		Reasoning int `json:"reasoning_tokens"`
	} `json:"completion_tokens_details"`
}
type batchOutput struct {
	CustomID string `json:"custom_id"`
	Response *struct {
		Status int `json:"status_code"`
		Body   struct {
			Choices []struct {
				FinishReason string `json:"finish_reason"`
				Message      struct {
					Content string `json:"content"`
				} `json:"message"`
			} `json:"choices"`
			Usage batchUsage      `json:"usage"`
			Error json.RawMessage `json:"error"`
		} `json:"body"`
	} `json:"response"`
	Error json.RawMessage `json:"error"`
}

func importBatch(ctx context.Context, run *BatchRun, b *Batch, p BatchProvider) error {
	for _, id := range []string{b.OutputFileID, b.ErrorFileID} {
		if id == "" {
			continue
		}
		reader, err := p.Download(ctx, id)
		if err != nil {
			return deferBatch(b, "importing", boundedBatchError(b, err))
		}
		err = importBatchStream(ctx, run, b, reader)
		reader.Close()
		if err != nil {
			return deferBatch(b, "importing", boundedBatchError(b, err))
		}
	}
	var missing []BatchItem
	if err := db.WithContext(ctx).Where("batch_id=? AND active_identity IS NOT NULL", b.ID).Find(&missing).Error; err != nil {
		return err
	}
	for _, item := range missing {
		message := b.Error
		if message == "" {
			message = "provider returned no usable result (" + b.ProviderStatus + ")"
		}
		if err := finishBatchItem(ctx, run, item, "failed", nil, message, nil); err != nil {
			return err
		}
	}
	now := time.Now().UTC()
	return db.Model(b).Updates(map[string]any{"phase": "done", "finished_at": now}).Error
}
func importBatchStream(ctx context.Context, run *BatchRun, b *Batch, reader io.Reader) error {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 65536), 6<<20)
	for scanner.Scan() {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if len(strings.TrimSpace(scanner.Text())) == 0 {
			continue
		}
		var out batchOutput
		if err := json.Unmarshal(scanner.Bytes(), &out); err != nil {
			return fmt.Errorf("invalid Batch result JSONL")
		}
		var item BatchItem
		result := db.WithContext(ctx).Where("batch_id=? AND custom_id=?", b.ID, out.CustomID).Limit(1).Find(&item)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return fmt.Errorf("Batch result contains an unknown custom_id")
		}
		if item.ActiveIdentity == nil {
			continue
		}
		status := "failed"
		assessmentResult, message := decodeBatchOutput(out)
		if assessmentResult != nil {
			status = "succeeded"
		}
		var usage *batchUsage
		if out.Response != nil {
			usage = &out.Response.Body.Usage
			if out.Response.Status != 200 {
				message = fmt.Sprintf("provider request failed (HTTP %d)", out.Response.Status)
			}
		}
		problems := batchProviderProblems(out.Error)
		if out.Response != nil {
			problems = append(problems, batchProviderProblems(out.Response.Body.Error)...)
		}
		if detail := batchProblemSummary(problems); detail != "" {
			message = detail
		}
		if usage == nil {
			usage = &batchUsage{}
		}
		if err := finishBatchItem(ctx, run, item, status, assessmentResult, message, usage); err != nil {
			return err
		}
		if err := batchProblemPause(problems); err != nil {
			return err
		}
		if out.Response != nil && (out.Response.Status == 401 || out.Response.Status == 429) {
			return batchHTTPError{Status: out.Response.Status}
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("Batch result read failed or exceeded line limit")
	}
	return nil
}
func finishBatchItem(ctx context.Context, run *BatchRun, item BatchItem, status string, result *ArticleAssessmentResult, message string, usage *batchUsage) error {
	_, err := finishBatchItemResult(ctx, run, item, status, result, message, usage, false)
	return err
}

func finishBatchItemResult(ctx context.Context, run *BatchRun, item BatchItem, status string, result *ArticleAssessmentResult, message string, usage *batchUsage, recovering bool) (bool, error) {
	applied := false
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var current BatchItem
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&current, item.ID).Error; err != nil {
			return err
		}
		if (!recovering && current.ActiveIdentity == nil) || (recovering && !reimportableBatchItem(current)) {
			return nil
		}
		if err := lockBatchArticle(tx, item.MaterialID); err != nil {
			return err
		}
		c, allowed, err := batchCandidate(tx, item)
		if err != nil {
			return err
		}
		if recovering {
			ok, err := batchReimportAllowed(tx, current, c, allowed)
			if err != nil || !ok {
				return err
			}
		}
		updates := map[string]any{}
		identity := batchIdentity{Model: run.Model, Policy: run.PolicyVersion}
		var row ArticleAssessment
		if result != nil {
			historical := discovery.DiscoveryCandidate{ID: item.CandidateID, MaterialID: item.MaterialID, ContentVersion: item.ContentVersion}
			row, err = persistArticleAssessmentWithDB(tx, historical, identity, *result)
			if err != nil {
				return err
			}
			updates["assessment_id"] = row.ID
		}
		if !allowed {
			status = "skipped"
			message = "article version, representative or blacklist changed"
		} else if result != nil {
			threshold := config.DISCOVERYARTICLEQUALITYTHRESHOLD
			if threshold <= 0 || threshold > 1 {
				threshold = articlevalue.QualityFloor
			}
			eligible := discovery.DiscoveryEligibilityEligible
			reason := "article_quality_passed"
			if result.Quality < threshold {
				eligible = discovery.DiscoveryEligibilityIneligible
				reason = "article_quality_below_threshold"
			}
			if err := tx.Model(&material.ArticleState{}).Where("material_id=? AND content_version=?", item.MaterialID, item.ContentVersion).Updates(map[string]any{"current_assessment_id": row.ID, "assessment_state": discovery.DiscoveryAssessmentReady, "assessment_error": "", "quality_score": clampAssessment(result.Quality), "depth_score": clampAssessment(result.Depth), "eligibility_state": eligible, "eligibility_reasons": reason, "updated_at": time.Now().UTC()}).Error; err != nil {
				return err
			}
			keywords, _ := json.Marshal(result.Keywords)
			if err := tx.Model(&material.Material{}).Where("id=?", item.MaterialID).Updates(map[string]any{"summary": strings.TrimSpace(result.Summary), "topics": string(keywords), "updated_at": time.Now().UTC()}).Error; err != nil {
				return err
			}
		} else if status == "failed" {
			values := map[string]any{"assessment_error": identity.PolicyVersion() + ": " + compactAssessmentError(message), "updated_at": time.Now().UTC()}
			var old ArticleAssessment
			usable := c.CurrentAssessmentID != nil && tx.Where("id=? AND material_id=? AND content_version=?", c.CurrentAssessmentID, item.MaterialID, item.ContentVersion).First(&old).Error == nil && isModelAssessment(old)
			if usable {
				values["assessment_state"] = discovery.DiscoveryAssessmentReady
			} else {
				values["assessment_state"] = discovery.DiscoveryAssessmentReview
				values["eligibility_state"] = discovery.DiscoveryEligibilityReview
				values["eligibility_reasons"] = "article_assessment_failed"
			}
			if err := tx.Model(&material.ArticleState{}).Where("material_id=? AND content_version=?", item.MaterialID, item.ContentVersion).Updates(values).Error; err != nil {
				return err
			}
		}
		now := time.Now().UTC()
		updates["status"] = status
		updates["error"] = compactAssessmentError(message)
		updates["active_identity"] = nil
		updates["finished_at"] = now
		if usage != nil {
			updates["prompt_tokens"] = usage.Prompt
			updates["completion_tokens"] = usage.Completion
			updates["reasoning_tokens"] = usage.Details.Reasoning
			updates["total_tokens"] = usage.Total
			mid := item.MaterialID
			callStatus := "success"
			errorType := ""
			if result == nil {
				callStatus = "failed"
				errorType = "batch_request_failed"
			}
			responseMode := ResponseObjectMode
			if item.BatchID != nil {
				var batch Batch
				if err := tx.Select("id", "response_mode").First(&batch, *item.BatchID).Error; err != nil {
					return err
				}
				responseMode = batch.ResponseMode
			}
			call := LLMCall{BatchItemID: &item.ID, MaterialID: &mid, CandidateID: item.CandidateID, Stage: llm.StageArticleAssessment, Status: callStatus, Attempt: 1, PromptTokens: usage.Prompt, CompletionTokens: usage.Completion, ReasoningTokens: usage.Details.Reasoning, TotalTokens: usage.Total, EvidenceTokens: item.EvidenceTokens, ResponseMode: responseMode, ErrorType: errorType, CreatedAt: now}
			conflict := clause.OnConflict{DoNothing: true}
			if recovering {
				// Reinterpreting an existing response is the same paid call. Keep
				// its ID, creation time and token accounting; correct only outcome.
				conflict = clause.OnConflict{Columns: []clause.Column{{Name: "batch_item_id"}}, DoUpdates: clause.AssignmentColumns([]string{"status", "error_type"})}
			}
			if err := tx.Clauses(conflict).Create(&call).Error; err != nil {
				return err
			}
		}
		if err := tx.Model(&BatchItem{}).Where("id=?", item.ID).Updates(updates).Error; err != nil {
			return err
		}
		applied = true
		return nil
	})
	return applied, err
}
