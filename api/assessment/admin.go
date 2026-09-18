package assessment

import (
	"DataArk/discovery"
	"context"
	"errors"
	"fmt"
	"strconv"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const MaxArticleAssessmentBatchSize = 250

type ArticleAssessmentBatchOptions struct {
	Limit         int  `json:"limit"`
	DryRun        bool `json:"dryRun"`
	RetryFailures bool `json:"retryFailures"`
}

type ArticleAssessmentBatchResult struct {
	PolicyVersion string `json:"policyVersion"`
	Selected      int    `json:"selected"`
	Enqueued      int    `json:"enqueued"`
	Reactivated   int    `json:"reactivated"`
	Skipped       int    `json:"skipped"`
	DryRun        bool   `json:"dryRun"`
}

// PrepareArticleAssessmentBackfill 为缺失的模型评估行入队，或在 active 模式下激活已有行。
func PrepareArticleAssessmentBackfill(ctx context.Context, assessor ArticleAssessor, queue ArticleJobEnqueuer, options ArticleAssessmentBatchOptions) (ArticleAssessmentBatchResult, error) {
	result := ArticleAssessmentBatchResult{DryRun: options.DryRun}
	if db == nil {
		return result, errors.New("assessment database is unavailable")
	}
	if assessor == nil {
		return result, errors.New("article assessment provider is not configured")
	}
	result.PolicyVersion = assessor.PolicyVersion()
	if !options.DryRun && queue == nil {
		return result, errors.New("article assessment queue is unavailable")
	}

	limit := normalizeArticleAssessmentBatchLimit(options.Limit)
	query := db.WithContext(ctx).Model(&discovery.DiscoveryCandidate{}).
		Where("processing_state = ? AND dedupe_state = ? AND content_version > 0", discovery.DiscoveryProcessingReady, discovery.DiscoveryDedupeReady).
		Where("representative_id IS NULL OR representative_id = id").
		Where(`NOT EXISTS (
SELECT 1 FROM discovery_article_assessments active
WHERE active.id = discovery_candidates.current_assessment_id
  AND active.candidate_id = discovery_candidates.id
  AND active.content_version = discovery_candidates.content_version
  AND active.assessor = ?
  AND active.assessor_version = ?
  AND active.policy_version = ?
)`, assessor.Name(), assessor.Version(), assessor.PolicyVersion())
	if !options.RetryFailures {
		query = query.Where("assessment_error IS NULL OR assessment_error NOT LIKE ?", assessor.PolicyVersion()+":%")
	}
	var candidates []discovery.DiscoveryCandidate
	if err := query.Order("id").Limit(limit).Find(&candidates).Error; err != nil {
		return result, err
	}
	result.Selected = len(candidates)
	var enqueueErrors []error
	for _, candidate := range candidates {
		storedResult, stored, found, lookupErr := loadPersistedAssessment(candidate, assessor)
		if lookupErr != nil {
			enqueueErrors = append(enqueueErrors, fmt.Errorf("load candidate %d assessment: %w", candidate.ID, lookupErr))
			continue
		}
		if found {
			if validationErr := validateAssessmentResult(storedResult); validationErr != nil {
				enqueueErrors = append(enqueueErrors, fmt.Errorf("validate candidate %d assessment: %w", candidate.ID, validationErr))
				continue
			}
			if !options.DryRun {
				if err := activateArticleAssessment(candidate, stored, storedResult, ""); err != nil {
					enqueueErrors = append(enqueueErrors, fmt.Errorf("reactivate candidate %d assessment: %w", candidate.ID, err))
					continue
				}
				if err := applyAssessmentArticleMetadata(candidate, storedResult); err != nil {
					enqueueErrors = append(enqueueErrors, fmt.Errorf("write candidate %d assessment metadata: %w", candidate.ID, err))
					continue
				}
			}
			result.Reactivated++
			continue
		}
		if options.DryRun {
			continue
		}
		if err := db.WithContext(ctx).Model(&candidate).Updates(map[string]interface{}{
			"assessment_state": discovery.DiscoveryAssessmentPending,
			"assessment_error": "",
			"updated_at":       discovery.Timestamp(),
		}).Error; err != nil {
			enqueueErrors = append(enqueueErrors, fmt.Errorf("mark candidate %d assessment pending: %w", candidate.ID, err))
			continue
		}
		if err := queue.EnqueueAssessArticle(ctx, candidate.ID, strconv.FormatUint(uint64(candidate.ContentVersion), 10)); err != nil {
			enqueueErrors = append(enqueueErrors, fmt.Errorf("enqueue candidate %d assessment: %w", candidate.ID, err))
			continue
		}
		result.Enqueued++
	}
	return result, errors.Join(enqueueErrors...)
}

// RollbackArticleAssessment 把当前模型评估指针退回到更早的非规则模型行。
func RollbackArticleAssessment(ctx context.Context, assessor ArticleAssessor, options ArticleAssessmentBatchOptions) (ArticleAssessmentBatchResult, error) {
	result := ArticleAssessmentBatchResult{DryRun: options.DryRun}
	if db == nil {
		return result, errors.New("assessment database is unavailable")
	}
	if assessor == nil {
		return result, errors.New("article assessment provider is not configured")
	}
	result.PolicyVersion = assessor.PolicyVersion()
	limit := normalizeArticleAssessmentBatchLimit(options.Limit)
	var candidates []discovery.DiscoveryCandidate
	if err := db.WithContext(ctx).
		Joins("JOIN discovery_article_assessments active ON active.id = discovery_candidates.current_assessment_id").
		Where("active.assessor = ? AND active.assessor_version = ? AND active.policy_version = ?", assessor.Name(), assessor.Version(), assessor.PolicyVersion()).
		Order("discovery_candidates.id").Limit(limit).Find(&candidates).Error; err != nil {
		return result, err
	}
	result.Selected = len(candidates)
	for _, candidate := range candidates {
		var target ArticleAssessment
		err := db.WithContext(ctx).
			Where("candidate_id = ? AND content_version = ? AND id <> ? AND assessor <> ?", candidate.ID, candidate.ContentVersion, *candidate.CurrentAssessmentID, RuleArticleAssessorName).
			Order(clause.Expr{SQL: "CASE WHEN policy_version <> ? THEN 0 ELSE 1 END", Vars: []interface{}{assessor.PolicyVersion()}}).
			Order("id DESC").
			First(&target).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			result.Skipped++
			continue
		}
		if err != nil {
			return result, err
		}
		if options.DryRun {
			result.Reactivated++
			continue
		}
		if err := activateArticleAssessment(candidate, target, assessmentResultFromRow(target), ""); err != nil {
			return result, err
		}
		result.Reactivated++
	}
	return result, nil
}

func normalizeArticleAssessmentBatchLimit(value int) int {
	if value <= 0 || value > MaxArticleAssessmentBatchSize {
		return MaxArticleAssessmentBatchSize
	}
	return value
}
