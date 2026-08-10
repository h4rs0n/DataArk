package discovery

import (
	"DataArk/articlevalue"
	"DataArk/config"
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
	DiscoveryAssessmentReady    = "ready"
	DiscoveryAssessmentReview   = "review"
	DiscoveryAssessmentDegraded = "degraded"
	RuleArticleAssessorName     = "deterministic_rules"
	RuleArticleAssessorVersion  = "2.0.0"
	ArticleQualityPolicyVersion = articlevalue.PolicyVersion
)

// ArticleAssessmentInput intentionally contains only the current article
// version's identity and clean text. Raw HTML, URL, author, date, source
// history, graph position, yield, and user feedback cannot cross this boundary.
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
}

type ArticleAssessor interface {
	Name() string
	Version() string
	PolicyVersion() string
	Assess(context.Context, ArticleAssessmentInput) (ArticleAssessmentResult, error)
}

type articleAssessmentActivator interface {
	ShouldActivateAssessment() bool
}

type RuleBasedArticleAssessor struct{}

func (RuleBasedArticleAssessor) Name() string          { return RuleArticleAssessorName }
func (RuleBasedArticleAssessor) Version() string       { return RuleArticleAssessorVersion }
func (RuleBasedArticleAssessor) PolicyVersion() string { return ArticleQualityPolicyVersion }

func (RuleBasedArticleAssessor) Assess(ctx context.Context, input ArticleAssessmentInput) (ArticleAssessmentResult, error) {
	if err := ctx.Err(); err != nil {
		return ArticleAssessmentResult{}, err
	}
	content := strings.TrimSpace(input.Title + " " + input.BodyText)
	if content == "" {
		return ArticleAssessmentResult{}, errors.New("article assessment requires body text")
	}
	tokens := articlevalue.EstimateTokens(content)
	scores := articlevalue.FallbackScores(tokens)
	reasons := []string{
		fmt.Sprintf("semantic model unavailable; conservative policy %s applied", ArticleQualityPolicyVersion),
		fmt.Sprintf("assessment evidence is approximately %d tokens", tokens),
	}
	return ArticleAssessmentResult{
		Quality: scores.Quality, Depth: scores.Depth, Evergreen: scores.Evergreen,
		Confidence: 0.25, Reasons: reasons,
	}, nil
}

func AssessCandidate(ctx context.Context, candidateID uint, enhanced ArticleAssessor) error {
	if db == nil || candidateID == 0 {
		return gorm.ErrRecordNotFound
	}
	var candidate DiscoveryCandidate
	if err := db.First(&candidate, candidateID).Error; err != nil {
		return err
	}
	if candidate.ProcessingState != DiscoveryProcessingReady || candidate.DedupeState != DiscoveryDedupeReady || candidate.ContentVersion == 0 {
		return nil
	}
	isRepresentative := candidate.RepresentativeID == nil || *candidate.RepresentativeID == candidate.ID
	if !isRepresentative {
		return db.Model(&candidate).Updates(map[string]interface{}{
			"assessment_state": DiscoveryAssessmentReady, "assessment_error": "",
			"eligibility_state":   DiscoveryEligibilityIneligible,
			"eligibility_reasons": "duplicate_non_representative", "updated_at": discoveryClock.Now(),
		}).Error
	}
	input := ArticleAssessmentInput{
		CandidateID: candidate.ID, Title: candidate.Title, BodyText: candidate.BodyText,
	}
	rule := RuleBasedArticleAssessor{}
	ruleResult, err := rule.Assess(ctx, input)
	if err != nil {
		return markAssessmentReview(candidate, err)
	}
	ruleAssessment, err := persistArticleAssessment(candidate, rule, ruleResult)
	if err != nil {
		return err
	}
	currentAssessment, hasCurrent := loadCurrentAssessment(candidate)
	if enhanced != nil && !(enhanced.Name() == rule.Name() && enhanced.Version() == rule.Version() && enhanced.PolicyVersion() == rule.PolicyVersion()) {
		result, stored, found, lookupErr := loadPersistedAssessment(candidate, enhanced)
		if lookupErr != nil {
			return lookupErr
		}
		var assessErr error
		if !found {
			result, assessErr = enhanced.Assess(ctx, input)
		}
		if assessErr != nil {
			return retainOrActivateFallback(candidate, currentAssessment, hasCurrent, ruleAssessment, ruleResult, enhancedAssessmentError(enhanced, assessErr))
		} else if validationErr := validateAssessmentResult(result); validationErr != nil {
			return retainOrActivateFallback(candidate, currentAssessment, hasCurrent, ruleAssessment, ruleResult, enhancedAssessmentError(enhanced, validationErr))
		}
		if !found {
			var persistErr error
			stored, persistErr = persistArticleAssessment(candidate, enhanced, result)
			if persistErr != nil {
				return persistErr
			}
		}
		if activator, ok := enhanced.(articleAssessmentActivator); ok && !activator.ShouldActivateAssessment() {
			if hasCurrent {
				return updateAssessmentStatus(candidate, DiscoveryAssessmentReady, "")
			}
			return activateArticleAssessment(candidate, ruleAssessment, ruleResult, "", false)
		}
		return activateArticleAssessment(candidate, stored, result, "", false)
	}
	if hasCurrent {
		return updateAssessmentStatus(candidate, DiscoveryAssessmentReady, "")
	}
	return activateArticleAssessment(candidate, ruleAssessment, ruleResult, "", false)
}

func enhancedAssessmentError(assessor ArticleAssessor, err error) error {
	return fmt.Errorf("%s: %w", assessor.PolicyVersion(), err)
}

func loadCurrentAssessment(candidate DiscoveryCandidate) (DiscoveryArticleAssessment, bool) {
	if candidate.CurrentAssessmentID == nil || *candidate.CurrentAssessmentID == 0 {
		return DiscoveryArticleAssessment{}, false
	}
	var assessment DiscoveryArticleAssessment
	if err := db.First(&assessment, *candidate.CurrentAssessmentID).Error; err != nil || assessment.CandidateID != candidate.ID || assessment.ContentVersion != candidate.ContentVersion {
		return DiscoveryArticleAssessment{}, false
	}
	return assessment, true
}

func loadPersistedAssessment(candidate DiscoveryCandidate, assessor ArticleAssessor) (ArticleAssessmentResult, DiscoveryArticleAssessment, bool, error) {
	var assessment DiscoveryArticleAssessment
	err := db.Where("candidate_id = ? AND content_version = ? AND assessor = ? AND assessor_version = ? AND policy_version = ?", candidate.ID, candidate.ContentVersion, assessor.Name(), assessor.Version(), assessor.PolicyVersion()).First(&assessment).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ArticleAssessmentResult{}, assessment, false, nil
	}
	if err != nil {
		return ArticleAssessmentResult{}, assessment, false, err
	}
	return assessmentResultFromRow(assessment), assessment, true, nil
}

func assessmentResultFromRow(assessment DiscoveryArticleAssessment) ArticleAssessmentResult {
	var reasons []string
	_ = json.Unmarshal([]byte(assessment.Reasons), &reasons)
	return ArticleAssessmentResult{
		Quality: assessment.OverallQuality, Depth: assessment.Depth, Evergreen: assessment.EvergreenValue,
		Confidence: assessment.Confidence, Reasons: reasons,
	}
}

func retainOrActivateFallback(candidate DiscoveryCandidate, current DiscoveryArticleAssessment, hasCurrent bool, fallback DiscoveryArticleAssessment, fallbackResult ArticleAssessmentResult, assessmentErr error) error {
	message := compactProcessingError(assessmentErr.Error())
	if hasCurrent {
		return updateAssessmentStatus(candidate, DiscoveryAssessmentReady, message)
	}
	return activateArticleAssessment(candidate, fallback, fallbackResult, message, true)
}

func updateAssessmentStatus(candidate DiscoveryCandidate, state string, assessmentError string) error {
	return db.Model(&candidate).Updates(map[string]interface{}{
		"assessment_state": state, "assessment_error": assessmentError, "updated_at": discoveryClock.Now(),
	}).Error
}

func persistArticleAssessment(candidate DiscoveryCandidate, assessor ArticleAssessor, result ArticleAssessmentResult) (DiscoveryArticleAssessment, error) {
	reasons, _ := json.Marshal(result.Reasons)
	assessment := DiscoveryArticleAssessment{
		CandidateID: candidate.ID, ContentVersion: candidate.ContentVersion,
		Assessor: assessor.Name(), AssessorVersion: assessor.Version(), PolicyVersion: assessor.PolicyVersion(),
		Depth: clampAssessment(result.Depth), EvergreenValue: clampAssessment(result.Evergreen), OverallQuality: clampAssessment(result.Quality),
		Confidence: clampAssessment(result.Confidence), Reasons: string(reasons), CreatedAt: discoveryClock.Now(),
	}
	err := db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "candidate_id"}, {Name: "content_version"}, {Name: "assessor"}, {Name: "assessor_version"}, {Name: "policy_version"}},
		DoNothing: true,
	}).Create(&assessment).Error
	if err != nil {
		return assessment, err
	}
	if assessment.ID == 0 {
		err = db.Where("candidate_id = ? AND content_version = ? AND assessor = ? AND assessor_version = ? AND policy_version = ?", candidate.ID, candidate.ContentVersion, assessor.Name(), assessor.Version(), assessor.PolicyVersion()).First(&assessment).Error
	}
	return assessment, err
}

func activateArticleAssessment(candidate DiscoveryCandidate, assessment DiscoveryArticleAssessment, result ArticleAssessmentResult, assessmentError string, degraded bool) error {
	threshold := config.DISCOVERYARTICLEQUALITYTHRESHOLD
	if threshold <= 0 || threshold > 1 {
		threshold = articlevalue.QualityFloor
	}
	eligibility := DiscoveryEligibilityEligible
	reason := "article_quality_passed"
	assessmentState := DiscoveryAssessmentReady
	if degraded {
		assessmentState = DiscoveryAssessmentDegraded
	}
	if result.Quality < threshold {
		eligibility = DiscoveryEligibilityIneligible
		reason = "article_quality_below_threshold"
	}
	now := discoveryClock.Now()
	return db.Model(&candidate).Updates(map[string]interface{}{
		"current_assessment_id": assessment.ID, "assessment_state": assessmentState,
		"assessment_error": assessmentError, "quality_score": clampAssessment(result.Quality),
		"depth_score": clampAssessment(result.Depth), "eligibility_state": eligibility,
		"eligibility_reasons": reason, "updated_at": now,
	}).Error
}

func markAssessmentReview(candidate DiscoveryCandidate, assessmentErr error) error {
	return db.Model(&candidate).Updates(map[string]interface{}{
		"assessment_state": DiscoveryAssessmentReview, "assessment_error": compactProcessingError(assessmentErr.Error()),
		"eligibility_state": DiscoveryEligibilityReview, "eligibility_reasons": "article_assessment_failed",
		"updated_at": discoveryClock.Now(),
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

func clampAssessment(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 1 {
		return 1
	}
	return value
}
