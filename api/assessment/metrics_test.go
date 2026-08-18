package assessment

import (
	"DataArk/discovery"
	"DataArk/llm"
	"testing"
	"time"
)

func TestGetMetricsAggregatesQueueAndSafeTokenRows(t *testing.T) {
	setupAssessmentDB(t)
	now := time.Date(2026, 8, 18, 18, 0, 0, 0, time.UTC)
	pending := discovery.DiscoveryCandidate{
		SourceID: 1, SourceName: "Feed", URL: "https://metrics.example/pending",
		Status: discovery.DiscoveryCandidateStatusNew, ProcessingState: discovery.DiscoveryProcessingReady,
		DedupeState: discovery.DiscoveryDedupeReady, AssessmentState: discovery.DiscoveryAssessmentPending,
		ContentVersion: 1, LastSeenAt: now,
	}
	if err := db.Create(&pending).Error; err != nil {
		t.Fatal(err)
	}
	rows := []LLMCall{
		{CandidateID: 7, Stage: llm.StageArticleAssessment, Status: "success", Attempt: 1, DurationMS: 100, PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15, CreatedAt: now.Add(-time.Hour)},
		{CandidateID: 8, Stage: llm.StageArticleAssessment, Status: "success", Attempt: 2, DurationMS: 400, PromptTokens: 20, CompletionTokens: 6, TotalTokens: 26, CreatedAt: now.Add(-time.Hour)},
		{CandidateID: 9, Stage: llm.StageArticleAssessment, Status: "failed", Attempt: 1, DurationMS: 50, ErrorType: "invalid_output", CreatedAt: now.Add(-time.Hour)},
		{CandidateID: 10, Stage: llm.StageRecommendationRerank, Status: "success", Attempt: 1, DurationMS: 10, PromptTokens: 99, CreatedAt: now.Add(-time.Hour)},
	}
	for _, row := range rows {
		if err := db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	metrics, err := GetMetrics(now)
	if err != nil {
		t.Fatal(err)
	}
	if metrics.PendingQueue != 1 {
		t.Fatalf("pendingQueue = %d", metrics.PendingQueue)
	}
	if metrics.Last24h.Success != 2 || metrics.Last24h.Failure != 1 {
		t.Fatalf("last24h = %#v", metrics.Last24h)
	}
	if metrics.TokenTotals.Prompt != 30 || metrics.TokenTotals.Total != 41 {
		t.Fatalf("tokens = %#v", metrics.TokenTotals)
	}
	if metrics.Duration.P50 != 100 || metrics.Duration.P95 != 400 {
		t.Fatalf("duration = %#v", metrics.Duration)
	}
	if metrics.SchemaRetryRate < 0.3 || metrics.SchemaRetryRate > 0.4 {
		t.Fatalf("schemaRetryRate = %v", metrics.SchemaRetryRate)
	}
}
