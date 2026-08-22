package recommendation

import (
	"DataArk/assessment"
	"DataArk/discovery"
	"context"
	"strings"
	"testing"
	"time"
)

type capturingAssessmentProvider struct {
	input ArticleAssessmentInput
}

func (provider *capturingAssessmentProvider) AssessArticle(_ context.Context, input ArticleAssessmentInput) (ArticleAssessmentResult, error) {
	provider.input = input
	return ArticleAssessmentResult{
		QualityScore: 91, DepthScore: 84, EvergreenScore: 79, Reasons: []string{"Strong evidence", "Useful depth"},
		Summary: "The fixture article explains a durable method with measurements.", Keywords: []string{"testing", "evidence", "methods"},
		Model: "fixture-llm", PromptVersion: "assessment-v2",
	}, nil
}

func TestOpenAICompatibleArticleAssessorPersistsEnhancedVersionWithoutSourceInput(t *testing.T) {
	setupSQLiteDB(t)
	now := time.Date(2026, 7, 14, 6, 0, 0, 0, time.UTC)
	body := strings.Repeat("Evidence and data explain this durable method with alternatives, counterexamples, measurements, and a reproducible conclusion. ", 12)
	candidate := DiscoveryCandidate{
		SourceID: 44, SourceName: "must-not-cross-boundary", URL: "https://example.com/article",
		Title: "Article-only assessment", BodyText: body, Language: "en", WordCount: len(strings.Fields(body)),
		ContentHash: discovery.ContentHash(body), ContentVersion: 1, Status: DiscoveryCandidateStatusNew,
		ProcessingState: discovery.DiscoveryProcessingReady, DedupeState: discovery.DiscoveryDedupeReady,
		AssessmentState: discovery.DiscoveryAssessmentPending, EligibilityState: discovery.DiscoveryEligibilityUnknown,
		LastSeenAt: now,
	}
	if err := db.Create(&candidate).Error; err != nil {
		t.Fatal(err)
	}
	provider := &capturingAssessmentProvider{}
	assessor := assessment.EnrichmentArticleAssessor{Provider: provider, Model: "fixture-llm", Mode: "active"}
	if err := assessment.AssessCandidate(context.Background(), candidate.ID, assessor); err != nil {
		t.Fatal(err)
	}
	if provider.input.CandidateID != candidate.ID || provider.input.Title != candidate.Title || provider.input.BodyText != body {
		t.Fatalf("enhanced assessor input = %#v", provider.input)
	}
	var rows []assessment.ArticleAssessment
	if err := db.Where("candidate_id = ?", candidate.ID).Order("id").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].Assessor != assessment.RuleArticleAssessorName || rows[1].Assessor != "openai_compatible" || rows[1].AssessorVersion != "fixture-llm" {
		t.Fatalf("rule/enhanced assessments = %#v", rows)
	}
	if rows[1].Reasons != `["Strong evidence","Useful depth"]` {
		t.Fatalf("enhanced assessment reasons = %s", rows[1].Reasons)
	}
	if rows[1].Summary != "The fixture article explains a durable method with measurements." || rows[1].Keywords != `["testing","evidence","methods"]` {
		t.Fatalf("enhanced assessment metadata = summary=%q keywords=%s", rows[1].Summary, rows[1].Keywords)
	}
	if err := db.First(&candidate, candidate.ID).Error; err != nil {
		t.Fatal(err)
	}
	if candidate.Summary != rows[1].Summary || candidate.Topics != rows[1].Keywords {
		t.Fatalf("candidate metadata write-back = summary=%q topics=%s", candidate.Summary, candidate.Topics)
	}
	if candidate.CurrentAssessmentID == nil || *candidate.CurrentAssessmentID != rows[1].ID || candidate.QualityScore != 0.70 || candidate.DepthScore != 0.65 || candidate.AssessmentState != discovery.DiscoveryAssessmentReady {
		t.Fatalf("active enhanced assessment = %#v", candidate)
	}
	if err := db.Model(&candidate).Updates(map[string]interface{}{"published_at": now}).Error; err != nil {
		t.Fatal(err)
	}
	settings := DefaultRecommendationSettings(20)
	settings.DailyLimit = 1
	settings.Enabled = true
	if _, err := SaveRecommendationSettings(&settings); err != nil {
		t.Fatal(err)
	}
	snapshot, err := GenerateDailyRecommendations(context.Background(), 20, "2026-07-14")
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Items) != 1 || snapshot.Items[0].AssessmentID == nil || *snapshot.Items[0].AssessmentID != rows[1].ID {
		t.Fatalf("recommendation assessment snapshot = %#v", snapshot.Items)
	}
}
