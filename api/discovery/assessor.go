package discovery

import (
	"DataArk/config"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
	"unicode"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	DiscoveryAssessmentReady    = "ready"
	DiscoveryAssessmentReview   = "review"
	RuleArticleAssessorName     = "deterministic_rules"
	RuleArticleAssessorVersion  = "1.0.0"
	ArticleQualityPolicyVersion = "article-quality-v1"
)

// ArticleAssessmentInput intentionally contains only the current article
// version and duplicate relationship. Source history, graph position, yield,
// and user feedback cannot cross this compile-time boundary.
type ArticleAssessmentInput struct {
	Title                string
	BodyText             string
	Author               string
	PublishedAt          *time.Time
	Language             string
	WordCount            int
	DuplicateMatchMethod string
	IsRepresentative     bool
}

type ArticleAssessmentResult struct {
	InformationDensity float64
	Originality        float64
	Completeness       float64
	Evidence           float64
	Readability        float64
	Depth              float64
	EvergreenValue     float64
	OverallQuality     float64
	Confidence         float64
	Reasons            []string
}

type ArticleAssessor interface {
	Name() string
	Version() string
	PolicyVersion() string
	Assess(context.Context, ArticleAssessmentInput) (ArticleAssessmentResult, error)
}

type RuleBasedArticleAssessor struct{}

func (RuleBasedArticleAssessor) Name() string          { return RuleArticleAssessorName }
func (RuleBasedArticleAssessor) Version() string       { return RuleArticleAssessorVersion }
func (RuleBasedArticleAssessor) PolicyVersion() string { return ArticleQualityPolicyVersion }

func (RuleBasedArticleAssessor) Assess(ctx context.Context, input ArticleAssessmentInput) (ArticleAssessmentResult, error) {
	if err := ctx.Err(); err != nil {
		return ArticleAssessmentResult{}, err
	}
	words := normalizedAssessmentWords(input.Title + " " + input.BodyText)
	if len(words) == 0 {
		return ArticleAssessmentResult{}, errors.New("article assessment requires body text")
	}
	wordCount := input.WordCount
	if wordCount <= 0 {
		wordCount = len(words)
	}
	unique := make(map[string]struct{}, len(words))
	for _, word := range words {
		unique[word] = struct{}{}
	}
	lexicalVariety := clampAssessment(float64(len(unique)) / float64(len(words)))
	informationDensity := clampAssessment(0.25 + lexicalVariety*0.75)
	originality := clampAssessment(0.3 + lexicalVariety*0.65)
	completeness := clampAssessment(float64(wordCount)/240 + markerScore(input.BodyText, []string{"conclusion", "summary", "therefore", "因此", "结论"})*0.25)
	evidence := clampAssessment(0.1 + markerScore(input.BodyText, []string{"evidence", "data", "example", "measurement", "experiment", "observation", "source", "证据", "数据", "案例", "实验"})*0.75 + numericDensity(input.BodyText)*0.15)
	readability := assessmentReadability(input.BodyText, wordCount)
	depth := clampAssessment(float64(wordCount)/360 + markerScore(input.BodyText, []string{"because", "tradeoff", "alternative", "counterexample", "hypothesis", "mechanism", "因为", "权衡", "反例", "机制"})*0.55)
	evergreen := clampAssessment(0.25 + markerScore(input.BodyText, []string{"method", "model", "principle", "guide", "reference", "durable", "方法", "模型", "原理", "指南"})*0.65)
	overall := clampAssessment(informationDensity*0.16 + originality*0.14 + completeness*0.16 + evidence*0.17 + readability*0.10 + depth*0.17 + evergreen*0.10)
	confidence := clampAssessment(0.35 + math.Min(float64(wordCount), 500)/1000 + boolAssessmentScore(input.IsRepresentative)*0.1)
	reasons := []string{
		fmt.Sprintf("article-only deterministic policy %s", ArticleQualityPolicyVersion),
		fmt.Sprintf("words=%d lexical_variety=%.3f", wordCount, lexicalVariety),
		fmt.Sprintf("evidence=%.3f depth=%.3f completeness=%.3f", evidence, depth, completeness),
	}
	return ArticleAssessmentResult{
		InformationDensity: informationDensity, Originality: originality, Completeness: completeness,
		Evidence: evidence, Readability: readability, Depth: depth, EvergreenValue: evergreen,
		OverallQuality: overall, Confidence: confidence, Reasons: reasons,
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
		Title: candidate.Title, BodyText: candidate.BodyText, Author: candidate.Author,
		PublishedAt: candidate.PublishedAt, Language: candidate.Language, WordCount: candidate.WordCount,
		DuplicateMatchMethod: duplicateClusterMethod(candidate.DuplicateClusterID), IsRepresentative: true,
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
	activeAssessment := ruleAssessment
	activeResult := ruleResult
	enhancedError := ""
	if enhanced != nil && !(enhanced.Name() == rule.Name() && enhanced.Version() == rule.Version() && enhanced.PolicyVersion() == rule.PolicyVersion()) {
		result, assessErr := enhanced.Assess(ctx, input)
		if assessErr != nil {
			enhancedError = compactProcessingError(assessErr.Error())
		} else if validationErr := validateAssessmentResult(result); validationErr != nil {
			enhancedError = compactProcessingError(validationErr.Error())
		} else {
			assessment, persistErr := persistArticleAssessment(candidate, enhanced, result)
			if persistErr != nil {
				return persistErr
			}
			activeAssessment = assessment
			activeResult = result
		}
	}
	return activateArticleAssessment(candidate, activeAssessment, activeResult, enhancedError)
}

func persistArticleAssessment(candidate DiscoveryCandidate, assessor ArticleAssessor, result ArticleAssessmentResult) (DiscoveryArticleAssessment, error) {
	reasons, _ := json.Marshal(result.Reasons)
	assessment := DiscoveryArticleAssessment{
		CandidateID: candidate.ID, ContentVersion: candidate.ContentVersion,
		Assessor: assessor.Name(), AssessorVersion: assessor.Version(), PolicyVersion: assessor.PolicyVersion(),
		InformationDensity: clampAssessment(result.InformationDensity), Originality: clampAssessment(result.Originality),
		Completeness: clampAssessment(result.Completeness), Evidence: clampAssessment(result.Evidence),
		Readability: clampAssessment(result.Readability), Depth: clampAssessment(result.Depth),
		EvergreenValue: clampAssessment(result.EvergreenValue), OverallQuality: clampAssessment(result.OverallQuality),
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

func activateArticleAssessment(candidate DiscoveryCandidate, assessment DiscoveryArticleAssessment, result ArticleAssessmentResult, assessmentError string) error {
	threshold := config.DISCOVERYARTICLEQUALITYTHRESHOLD
	if threshold <= 0 || threshold > 1 {
		threshold = 0.45
	}
	eligibility := DiscoveryEligibilityEligible
	reason := "article_quality_passed"
	assessmentState := DiscoveryAssessmentReady
	if result.Confidence < 0.35 {
		eligibility = DiscoveryEligibilityReview
		reason = "article_quality_low_confidence"
		assessmentState = DiscoveryAssessmentReview
	} else if result.OverallQuality < threshold {
		eligibility = DiscoveryEligibilityIneligible
		reason = "article_quality_below_threshold"
	}
	now := discoveryClock.Now()
	return db.Model(&candidate).Updates(map[string]interface{}{
		"current_assessment_id": assessment.ID, "assessment_state": assessmentState,
		"assessment_error": assessmentError, "quality_score": clampAssessment(result.OverallQuality),
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

func duplicateClusterMethod(clusterID string) string {
	if db == nil || strings.TrimSpace(clusterID) == "" {
		return DuplicateMatchUnique
	}
	var cluster DiscoveryDuplicateCluster
	if err := db.Select("match_method").First(&cluster, "cluster_id = ?", clusterID).Error; err != nil {
		return DuplicateMatchUnique
	}
	return cluster.MatchMethod
}

func validateAssessmentResult(result ArticleAssessmentResult) error {
	values := []float64{result.InformationDensity, result.Originality, result.Completeness, result.Evidence, result.Readability, result.Depth, result.EvergreenValue, result.OverallQuality, result.Confidence}
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

func normalizedAssessmentWords(value string) []string {
	return strings.FieldsFunc(strings.ToLower(value), func(character rune) bool {
		return !unicode.IsLetter(character) && !unicode.IsNumber(character)
	})
}

func markerScore(value string, markers []string) float64 {
	value = strings.ToLower(value)
	matches := 0
	for _, marker := range markers {
		if strings.Contains(value, marker) {
			matches++
		}
	}
	return clampAssessment(float64(matches) / 3)
}

func numericDensity(value string) float64 {
	var digits, runes int
	for _, character := range value {
		runes++
		if unicode.IsDigit(character) {
			digits++
		}
	}
	if runes == 0 {
		return 0
	}
	return clampAssessment(float64(digits) / float64(runes) * 20)
}

func assessmentReadability(value string, wordCount int) float64 {
	sentences := strings.Count(value, ".") + strings.Count(value, "!") + strings.Count(value, "?") + strings.Count(value, "。") + strings.Count(value, "！") + strings.Count(value, "？")
	if sentences <= 0 {
		sentences = 1
	}
	wordsPerSentence := float64(wordCount) / float64(sentences)
	switch {
	case wordsPerSentence >= 8 && wordsPerSentence <= 28:
		return 0.9
	case wordsPerSentence <= 45:
		return 0.65
	default:
		return 0.4
	}
}

func boolAssessmentScore(value bool) float64 {
	if value {
		return 1
	}
	return 0
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
