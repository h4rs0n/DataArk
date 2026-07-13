package recommendation

import (
	"DataArk/discovery"
	"context"
	"strings"
	"testing"
	"time"
)

type capturingAssessmentEnricher struct {
	input EnrichmentInput
}

func (provider *capturingAssessmentEnricher) Enrich(_ context.Context, input EnrichmentInput) (EnrichmentResult, error) {
	provider.input = input
	return EnrichmentResult{
		QualityScore: 0.91, DepthScore: 0.84, Model: "fixture-llm", PromptVersion: "assessment-v1",
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
	provider := &capturingAssessmentEnricher{}
	assessor := EnrichmentArticleAssessor{Provider: provider, Model: "fixture-llm"}
	if err := discovery.AssessCandidate(context.Background(), candidate.ID, assessor); err != nil {
		t.Fatal(err)
	}
	if provider.input.CandidateID != 0 || provider.input.URL != "" || provider.input.Title != candidate.Title || provider.input.BodyText != body {
		t.Fatalf("enhanced assessor input leaked identity/source fields: %#v", provider.input)
	}
	var assessments []discovery.DiscoveryArticleAssessment
	if err := db.Where("candidate_id = ?", candidate.ID).Order("id").Find(&assessments).Error; err != nil {
		t.Fatal(err)
	}
	if len(assessments) != 2 || assessments[0].Assessor != discovery.RuleArticleAssessorName || assessments[1].Assessor != "openai_compatible" || assessments[1].AssessorVersion != "fixture-llm" {
		t.Fatalf("rule/enhanced assessments = %#v", assessments)
	}
	if err := db.First(&candidate, candidate.ID).Error; err != nil {
		t.Fatal(err)
	}
	if candidate.CurrentAssessmentID == nil || *candidate.CurrentAssessmentID != assessments[1].ID || candidate.QualityScore != 0.91 || candidate.AssessmentState != discovery.DiscoveryAssessmentReady {
		t.Fatalf("active enhanced assessment = %#v", candidate)
	}
	if err := db.Model(&candidate).Updates(map[string]interface{}{"enrichment_status": RecommendationEnrichmentStatusReady, "published_at": now}).Error; err != nil {
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
	if len(snapshot.Items) != 1 || snapshot.Items[0].AssessmentID == nil || *snapshot.Items[0].AssessmentID != assessments[1].ID {
		t.Fatalf("recommendation assessment snapshot = %#v", snapshot.Items)
	}
}
