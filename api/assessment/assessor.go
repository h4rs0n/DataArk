// 评估状态机：只跑 LLM 评估，不可变行落库与候选激活。
package assessment

import (
	"DataArk/articlevalue"
	"DataArk/config"
	"DataArk/discovery"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	ArticleQualityPolicyVersion = articlevalue.PolicyVersion
	// RuleArticleAssessorName 只用来识别历史规则行，不再作为评估器实现。
	RuleArticleAssessorName = "deterministic_rules"
)

// ArticleAssessmentInput 只含当前正文版本的身份与纯文本，禁止带入 URL/来源/图谱/反馈。
type ArticleAssessmentInput struct {
	CandidateID uint
	Title       string
	BodyText    string
}

type ArticleAssessmentResult struct {
	Quality    float64
	Depth      float64
	Evergreen  float64
	Confidence float64
	Reasons    []string
	Summary    string
	Keywords   []string
}

type ArticleAssessor interface {
	Name() string
	Version() string
	PolicyVersion() string
	Assess(context.Context, ArticleAssessmentInput) (ArticleAssessmentResult, error)
}

func isModelAssessment(row ArticleAssessment) bool {
	return strings.TrimSpace(row.Assessor) != "" && row.Assessor != RuleArticleAssessorName
}

// AssessCandidate 对已抽取的代表文章强制调用 LLM 评估；失败时不写规则分。
func AssessCandidate(ctx context.Context, candidateID uint, assessor ArticleAssessor) error {
	if db == nil || candidateID == 0 {
		return gorm.ErrRecordNotFound
	}
	var candidate discovery.DiscoveryCandidate
	if err := db.First(&candidate, candidateID).Error; err != nil {
		return err
	}
	return assessMaterialRecord(ctx, candidate, assessor)
}

func assessMaterialRecord(ctx context.Context, candidate discovery.DiscoveryCandidate, assessor ArticleAssessor) error {
	if candidate.ProcessingState != discovery.DiscoveryProcessingReady || candidate.DedupeState != discovery.DiscoveryDedupeReady || candidate.ContentVersion == 0 {
		return nil
	}
	isRepresentative := candidate.RepresentativeID == nil || *candidate.RepresentativeID == candidate.ID
	if !isRepresentative && candidate.EligibilityReasons == "duplicate_non_representative" {
		return updateAssessmentMaterial(candidate, map[string]interface{}{
			"assessment_state": discovery.DiscoveryAssessmentReady, "assessment_error": "",
			"eligibility_state":   discovery.DiscoveryEligibilityIneligible,
			"eligibility_reasons": "duplicate_non_representative", "updated_at": discovery.Timestamp(),
		}).Error
	}
	if assessor == nil {
		return retainCurrentModelOrFail(candidate, ArticleAssessment{}, false, errors.New("article assessment provider is not configured"))
	}
	input := ArticleAssessmentInput{
		CandidateID: candidate.ID, Title: candidate.Title, BodyText: candidate.BodyText,
	}
	currentAssessment, hasCurrent := loadCurrentAssessment(candidate)
	if hasCurrent && !isModelAssessment(currentAssessment) {
		hasCurrent = false
		currentAssessment = ArticleAssessment{}
	}
	result, stored, found, lookupErr := loadPersistedAssessment(candidate, assessor)
	if lookupErr != nil {
		return lookupErr
	}
	if !found {
		var assessErr error
		result, assessErr = assessor.Assess(ctx, input)
		if assessErr != nil {
			return retainCurrentModelOrFail(candidate, currentAssessment, hasCurrent, enhancedAssessmentError(assessor, assessErr))
		}
		if validationErr := validateAssessmentResult(result); validationErr != nil {
			return retainCurrentModelOrFail(candidate, currentAssessment, hasCurrent, enhancedAssessmentError(assessor, validationErr))
		}
		var persistErr error
		stored, persistErr = persistArticleAssessment(candidate, assessor, result)
		if persistErr != nil {
			return persistErr
		}
	} else if validationErr := validateAssessmentResult(result); validationErr != nil {
		return retainCurrentModelOrFail(candidate, currentAssessment, hasCurrent, enhancedAssessmentError(assessor, validationErr))
	}
	if err := applyAssessmentArticleMetadata(candidate, result); err != nil {
		return err
	}
	return activateArticleAssessment(candidate, stored, result, "")
}

func enhancedAssessmentError(assessor ArticleAssessor, err error) error {
	return fmt.Errorf("%s: %w", assessor.PolicyVersion(), err)
}

func loadCurrentAssessment(candidate discovery.DiscoveryCandidate) (ArticleAssessment, bool) {
	if candidate.CurrentAssessmentID == nil || *candidate.CurrentAssessmentID == 0 {
		return ArticleAssessment{}, false
	}
	var row ArticleAssessment
	if err := db.First(&row, *candidate.CurrentAssessmentID).Error; err != nil || row.MaterialID != candidate.MaterialID || row.ContentVersion != candidate.ContentVersion {
		return ArticleAssessment{}, false
	}
	return row, true
}

func loadPersistedAssessment(candidate discovery.DiscoveryCandidate, assessor ArticleAssessor) (ArticleAssessmentResult, ArticleAssessment, bool, error) {
	var row ArticleAssessment
	err := db.Where("material_id = ? AND content_version = ? AND assessor = ? AND assessor_version = ? AND policy_version = ?", candidate.MaterialID, candidate.ContentVersion, assessor.Name(), assessor.Version(), assessor.PolicyVersion()).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ArticleAssessmentResult{}, row, false, nil
	}
	if err != nil {
		return ArticleAssessmentResult{}, row, false, err
	}
	return assessmentResultFromRow(row), row, true, nil
}

func assessmentResultFromRow(row ArticleAssessment) ArticleAssessmentResult {
	var reasons []string
	_ = json.Unmarshal([]byte(row.Reasons), &reasons)
	var keywords []string
	if strings.TrimSpace(row.Keywords) != "" {
		_ = json.Unmarshal([]byte(row.Keywords), &keywords)
	}
	return ArticleAssessmentResult{
		Quality: row.OverallQuality, Depth: row.Depth, Evergreen: row.EvergreenValue,
		Confidence: row.Confidence, Reasons: reasons, Summary: row.Summary, Keywords: keywords,
	}
}

func retainCurrentModelOrFail(candidate discovery.DiscoveryCandidate, current ArticleAssessment, hasCurrent bool, assessmentErr error) error {
	if hasCurrent && isModelAssessment(current) {
		return updateAssessmentStatus(candidate, discovery.DiscoveryAssessmentReady, compactAssessmentError(assessmentErr.Error()))
	}
	if err := markAssessmentReview(candidate, assessmentErr); err != nil {
		return err
	}
	return assessmentErr
}

func updateAssessmentStatus(candidate discovery.DiscoveryCandidate, state string, assessmentError string) error {
	return updateAssessmentMaterial(candidate, map[string]interface{}{
		"assessment_state": state, "assessment_error": assessmentError, "updated_at": discovery.Timestamp(),
	}).Error
}

func persistArticleAssessment(candidate discovery.DiscoveryCandidate, assessor ArticleAssessor, result ArticleAssessmentResult) (ArticleAssessment, error) {
	reasons, _ := json.Marshal(result.Reasons)
	keywordsJSON := ""
	if len(result.Keywords) > 0 {
		encoded, _ := json.Marshal(result.Keywords)
		keywordsJSON = string(encoded)
	}
	row := ArticleAssessment{
		CandidateID: candidate.ID, MaterialID: candidate.MaterialID, ContentVersion: candidate.ContentVersion,
		Assessor: assessor.Name(), AssessorVersion: assessor.Version(), PolicyVersion: assessor.PolicyVersion(),
		Depth: clampAssessment(result.Depth), EvergreenValue: clampAssessment(result.Evergreen), OverallQuality: clampAssessment(result.Quality),
		Confidence: clampAssessment(result.Confidence), Reasons: string(reasons), Summary: strings.TrimSpace(result.Summary),
		Keywords: keywordsJSON, CreatedAt: discovery.Timestamp(),
	}
	err := db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "material_id"}, {Name: "content_version"}, {Name: "assessor"}, {Name: "assessor_version"}, {Name: "policy_version"}},
		DoNothing: true,
	}).Create(&row).Error
	if err != nil {
		return row, err
	}
	if row.ID == 0 {
		err = db.Where("material_id = ? AND content_version = ? AND assessor = ? AND assessor_version = ? AND policy_version = ?", candidate.MaterialID, candidate.ContentVersion, assessor.Name(), assessor.Version(), assessor.PolicyVersion()).First(&row).Error
	}
	return row, err
}

// activateArticleAssessment 把一行模型评估设为当前指针，并按质量门槛更新 eligibility。
func activateArticleAssessment(candidate discovery.DiscoveryCandidate, row ArticleAssessment, result ArticleAssessmentResult, assessmentError string) error {
	threshold := config.DISCOVERYARTICLEQUALITYTHRESHOLD
	if threshold <= 0 || threshold > 1 {
		threshold = articlevalue.QualityFloor
	}
	eligibility := discovery.DiscoveryEligibilityEligible
	reason := "article_quality_passed"
	if result.Quality < threshold {
		eligibility = discovery.DiscoveryEligibilityIneligible
		reason = "article_quality_below_threshold"
	}
	now := discovery.Timestamp()
	return updateAssessmentMaterial(candidate, map[string]interface{}{
		"current_assessment_id": row.ID, "assessment_state": discovery.DiscoveryAssessmentReady,
		"assessment_error": assessmentError, "quality_score": clampAssessment(result.Quality),
		"depth_score": clampAssessment(result.Depth), "eligibility_state": eligibility,
		"eligibility_reasons": reason, "updated_at": now,
	}).Error
}

func markAssessmentReview(candidate discovery.DiscoveryCandidate, assessmentErr error) error {
	return updateAssessmentMaterial(candidate, map[string]interface{}{
		"assessment_state": discovery.DiscoveryAssessmentReview, "assessment_error": compactAssessmentError(assessmentErr.Error()),
		"eligibility_state": discovery.DiscoveryEligibilityReview, "eligibility_reasons": "article_assessment_failed",
		"updated_at": discovery.Timestamp(),
	}).Error
}

func validateAssessmentResult(result ArticleAssessmentResult) error {
	values := []float64{result.Quality, result.Depth, result.Evergreen, result.Confidence}
	for _, value := range values {
		if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value > 1 {
			return fmt.Errorf("article assessor returned out-of-range score %v", value)
		}
	}
	if len(result.Reasons) == 0 || strings.TrimSpace(strings.Join(result.Reasons, "")) == "" {
		return errors.New("article assessor returned no explanation")
	}
	return nil
}

// applyAssessmentArticleMetadata 把摘要与关键词写回候选展示字段，不影响激活指针。
func applyAssessmentArticleMetadata(candidate discovery.DiscoveryCandidate, result ArticleAssessmentResult) error {
	summary := strings.TrimSpace(result.Summary)
	if summary == "" && len(result.Keywords) == 0 {
		return nil
	}
	updates := map[string]interface{}{"updated_at": discovery.Timestamp()}
	if summary != "" {
		updates["summary"] = summary
	}
	if len(result.Keywords) > 0 {
		topics, err := json.Marshal(result.Keywords)
		if err != nil {
			return err
		}
		updates["topics"] = string(topics)
	}
	return updateAssessmentMaterial(candidate, updates).Error
}

func clampAssessment(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 1 {
		return 1
	}
	return value
}

func compactAssessmentError(value string) string {
	value = strings.Join(strings.Fields(value), " ")
	runes := []rune(value)
	if len(runes) > 500 {
		return string(runes[:500])
	}
	return value
}
