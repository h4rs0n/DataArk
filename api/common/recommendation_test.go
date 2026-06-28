package common

import (
	"DataArk/recommendation"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

type failingEnrichmentProvider struct{}

func (failingEnrichmentProvider) Enrich(context.Context, recommendation.EnrichmentInput) (recommendation.EnrichmentResult, error) {
	return recommendation.EnrichmentResult{}, errors.New("llm unavailable")
}

func TestRecommendationSettingsDefaultAndSave(t *testing.T) {
	setupSQLiteDB(t)

	settings, err := GetRecommendationSettings(7)
	if err != nil {
		t.Fatal(err)
	}
	if settings.UserID != 7 || settings.DailyLimit != 10 || settings.Timezone == "" {
		t.Fatalf("unexpected default settings: %#v", settings)
	}

	settings.DailyLimit = 15
	settings.ExplorationRate = 2
	saved, err := SaveRecommendationSettings(settings)
	if err != nil {
		t.Fatal(err)
	}
	if saved.DailyLimit != 15 || saved.ExplorationRate != 1 {
		t.Fatalf("saved settings = %#v", saved)
	}
}

func TestRecommendationDayAndItemDeduplication(t *testing.T) {
	setupSQLiteDB(t)

	day, err := CreateRecommendationDay(3, "2026-06-28", 10)
	if err != nil {
		t.Fatal(err)
	}
	if day.UserID != 3 || day.RecommendationDate != "2026-06-28" {
		t.Fatalf("day = %#v", day)
	}
	reused, err := CreateRecommendationDay(3, "2026-06-28", 10)
	if err != nil {
		t.Fatal(err)
	}
	if reused.ID != day.ID {
		t.Fatalf("duplicate day created: first=%d second=%d", day.ID, reused.ID)
	}

	if _, err := AddRecommendationItem(&RecommendationItem{DayID: day.ID, UserID: 3, CandidateID: 11, DedupeKey: "hash-1", Rank: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := AddRecommendationItem(&RecommendationItem{DayID: day.ID, UserID: 3, CandidateID: 11, DedupeKey: "hash-2", Rank: 2}); !errors.Is(err, ErrDuplicateRecommendationItem) {
		t.Fatalf("duplicate candidate err = %v, want ErrDuplicateRecommendationItem", err)
	}
	if _, err := AddRecommendationItem(&RecommendationItem{DayID: day.ID, UserID: 3, CandidateID: 12, DedupeKey: "hash-1", Rank: 2}); !errors.Is(err, ErrDuplicateRecommendationItem) {
		t.Fatalf("duplicate dedupe key err = %v, want ErrDuplicateRecommendationItem", err)
	}

	snapshot, err := GetRecommendationDaySnapshot(3, "2026-06-28")
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Day.ID != day.ID || len(snapshot.Items) != 1 || snapshot.Items[0].CandidateID != 11 {
		t.Fatalf("snapshot = %#v", snapshot)
	}
}

func TestRecommendationFeedbackAndBlockRules(t *testing.T) {
	setupSQLiteDB(t)

	day, err := CreateRecommendationDay(5, "2026-06-28", 10)
	if err != nil {
		t.Fatal(err)
	}
	item, err := AddRecommendationItem(&RecommendationItem{DayID: day.ID, UserID: 5, CandidateID: 21, DedupeKey: "topic-1", Rank: 1})
	if err != nil {
		t.Fatal(err)
	}

	if _, _, err := RecordRecommendationFeedback(5, item.ID, "bad", nil); !errors.Is(err, ErrInvalidRecommendationFeedback) {
		t.Fatalf("invalid feedback err = %v, want ErrInvalidRecommendationFeedback", err)
	}
	feedback, rules, err := RecordRecommendationFeedback(5, item.ID, RecommendationFeedbackBlock, []RecommendationBlockTarget{
		{Type: UserBlockRuleTopic, Value: "PostgreSQL"},
		{Type: UserBlockRuleSource, Value: "example.com"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if feedback.Action != RecommendationFeedbackBlock || len(rules) != 2 {
		t.Fatalf("feedback=%#v rules=%#v", feedback, rules)
	}
	activeRules, err := ListUserBlockRules(5, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(activeRules) != 2 {
		t.Fatalf("active rules = %#v", activeRules)
	}
	if err := DeleteUserBlockRule(5, activeRules[0].ID); err != nil {
		t.Fatal(err)
	}
	activeRules, err = ListUserBlockRules(5, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(activeRules) != 1 {
		t.Fatalf("active rules after delete = %#v", activeRules)
	}
	if err := RevertRecommendationFeedback(5, item.ID); err != nil {
		t.Fatal(err)
	}
}

func TestEnrichDiscoveryCandidateUpdatesStructuredFields(t *testing.T) {
	setupSQLiteDB(t)
	candidate := DiscoveryCandidate{
		SourceID:         1,
		SourceName:       "Feed",
		URL:              "https://example.com/post?utm_source=newsletter",
		Title:            "PostgreSQL pgvector Guide",
		Summary:          "A tutorial about PostgreSQL vector search",
		BodyText:         strings.Repeat("This PostgreSQL pgvector tutorial explains embedding search. ", 40),
		Status:           DiscoveryCandidateStatusNew,
		EnrichmentStatus: RecommendationEnrichmentStatusPending,
	}
	if err := db.Create(&candidate).Error; err != nil {
		t.Fatal(err)
	}

	enriched, err := EnrichDiscoveryCandidate(context.Background(), candidate.ID, recommendation.RuleBasedEnrichmentProvider{})
	if err != nil {
		t.Fatal(err)
	}
	if enriched.EnrichmentStatus != RecommendationEnrichmentStatusReady || enriched.ContentHash == "" || enriched.DedupeKey != enriched.ContentHash {
		t.Fatalf("enriched identity fields = %#v", enriched)
	}
	if enriched.NormalizedURL != "https://example.com/post" || enriched.CanonicalURL != "https://example.com/post" {
		t.Fatalf("normalized urls = %#v", enriched)
	}
	if !strings.Contains(enriched.Topics, "PostgreSQL") || !strings.Contains(enriched.Entities, "pgvector") {
		t.Fatalf("topics/entities not updated: topics=%q entities=%q", enriched.Topics, enriched.Entities)
	}
	if enriched.QualityScore <= 0 || enriched.DepthScore <= 0 || enriched.LLMModel != recommendation.RuleBasedProviderModel {
		t.Fatalf("scores/model not updated: %#v", enriched)
	}
}

func TestEnrichDiscoveryCandidateRecordsFailure(t *testing.T) {
	setupSQLiteDB(t)
	candidate := DiscoveryCandidate{
		SourceID:         1,
		SourceName:       "Feed",
		URL:              "https://example.com/post",
		Title:            "Post",
		Status:           DiscoveryCandidateStatusNew,
		EnrichmentStatus: RecommendationEnrichmentStatusPending,
	}
	if err := db.Create(&candidate).Error; err != nil {
		t.Fatal(err)
	}

	if _, err := EnrichDiscoveryCandidate(context.Background(), candidate.ID, failingEnrichmentProvider{}); err == nil {
		t.Fatal("expected enrichment error")
	}
	var got DiscoveryCandidate
	if err := db.First(&got, candidate.ID).Error; err != nil {
		t.Fatal(err)
	}
	if got.EnrichmentStatus != RecommendationEnrichmentStatusFailed || got.EnrichmentError != "llm unavailable" {
		t.Fatalf("failure fields = %#v", got)
	}
}

func TestGenerateDailyRecommendationsFiltersHistoryAndBlocks(t *testing.T) {
	setupSQLiteDB(t)
	settings := DefaultRecommendationSettings(9)
	settings.DailyLimit = 2
	settings.Enabled = true
	if _, err := SaveRecommendationSettings(&settings); err != nil {
		t.Fatal(err)
	}
	dupe := createReadyCandidate(t, "https://example.com/dupe", "Duplicate", []string{"Go"}, "dupe-key", 0.9, 0.4)
	blocked := createReadyCandidate(t, "https://blocked.example/post", "Blocked", []string{"Kubernetes"}, "blocked-key", 0.95, 0.5)
	first := createReadyCandidate(t, "https://go.example/post", "Go Guide", []string{"Go"}, "go-key", 0.8, 0.8)
	second := createReadyCandidate(t, "https://pg.example/post", "PostgreSQL Guide", []string{"PostgreSQL"}, "pg-key", 0.7, 0.9)
	oldDay, err := CreateRecommendationDay(9, "2026-06-27", 2)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := AddRecommendationItem(&RecommendationItem{DayID: oldDay.ID, UserID: 9, CandidateID: dupe.ID, DedupeKey: dupe.DedupeKey, Rank: 1}); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&UserBlockRule{UserID: 9, RuleType: UserBlockRuleTopic, RuleValue: "Kubernetes", Active: true}).Error; err != nil {
		t.Fatal(err)
	}

	snapshot, err := GenerateDailyRecommendations(context.Background(), 9, "2026-06-28")
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Day.ActualCount != 2 || len(snapshot.Items) != 2 {
		t.Fatalf("snapshot = %#v", snapshot)
	}
	gotIDs := map[uint]bool{}
	for _, item := range snapshot.Items {
		gotIDs[item.CandidateID] = true
	}
	if gotIDs[dupe.ID] || gotIDs[blocked.ID] || !gotIDs[first.ID] || !gotIDs[second.ID] {
		t.Fatalf("generated ids = %#v, dupe=%d blocked=%d first=%d second=%d", gotIDs, dupe.ID, blocked.ID, first.ID, second.ID)
	}
	again, err := GenerateDailyRecommendations(context.Background(), 9, "2026-06-28")
	if err != nil {
		t.Fatal(err)
	}
	if len(again.Items) != 2 || again.Items[0].ID != snapshot.Items[0].ID {
		t.Fatalf("daily snapshot changed: first=%#v second=%#v", snapshot.Items, again.Items)
	}
}

func TestGenerateDailyRecommendationsUsesFeedbackProfile(t *testing.T) {
	setupSQLiteDB(t)
	settings := DefaultRecommendationSettings(10)
	settings.DailyLimit = 2
	settings.Enabled = true
	if _, err := SaveRecommendationSettings(&settings); err != nil {
		t.Fatal(err)
	}
	liked := createReadyCandidate(t, "https://old.example/postgres", "Old PostgreSQL", []string{"PostgreSQL"}, "old-pg", 0.6, 0.8)
	oldDay, err := CreateRecommendationDay(10, "2026-06-27", 1)
	if err != nil {
		t.Fatal(err)
	}
	item, err := AddRecommendationItem(&RecommendationItem{DayID: oldDay.ID, UserID: 10, CandidateID: liked.ID, DedupeKey: liked.DedupeKey, Rank: 1})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := RecordRecommendationFeedback(10, item.ID, RecommendationFeedbackDeepRead, nil); err != nil {
		t.Fatal(err)
	}
	postgres := createReadyCandidate(t, "https://new.example/postgres", "New PostgreSQL", []string{"PostgreSQL"}, "new-pg", 0.45, 0.6)
	generic := createReadyCandidate(t, "https://new.example/generic", "Generic", []string{"Release"}, "generic", 0.65, 0.4)

	snapshot, err := GenerateDailyRecommendations(context.Background(), 10, "2026-06-28")
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Items) != 2 {
		t.Fatalf("items = %#v", snapshot.Items)
	}
	if snapshot.Items[0].CandidateID != postgres.ID || snapshot.Items[1].CandidateID != generic.ID {
		t.Fatalf("ranked items = %#v, want postgres %d before generic %d", snapshot.Items, postgres.ID, generic.ID)
	}
}

func createReadyCandidate(t *testing.T, rawURL string, title string, topics []string, dedupeKey string, quality float64, depth float64) DiscoveryCandidate {
	t.Helper()
	topicBytes, _ := json.Marshal(topics)
	now := time.Now()
	candidate := DiscoveryCandidate{
		SourceID:         1,
		SourceName:       sourceHost(rawURL),
		URL:              rawURL,
		NormalizedURL:    rawURL,
		CanonicalURL:     rawURL,
		Title:            title,
		Summary:          title,
		Topics:           string(topicBytes),
		ContentType:      "article",
		ContentStyle:     "technical_deep_dive",
		QualityScore:     quality,
		DepthScore:       depth,
		EnrichmentStatus: RecommendationEnrichmentStatusReady,
		Status:           DiscoveryCandidateStatusNew,
		DedupeKey:        dedupeKey,
		PublishedAt:      &now,
		LastSeenAt:       now,
	}
	if err := db.Create(&candidate).Error; err != nil {
		t.Fatal(err)
	}
	return candidate
}
