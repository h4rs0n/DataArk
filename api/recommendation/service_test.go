package recommendation

import (
	"DataArk/discovery"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

type fakeReranker struct {
	result RerankResult
	err    error
}

func (provider fakeReranker) Rerank(context.Context, RerankInput) (RerankResult, error) {
	return provider.result, provider.err
}

type fakeEmbeddingProvider struct {
	vectors [][]float32
	err     error
}

func (provider fakeEmbeddingProvider) Embed(context.Context, []string) ([][]float32, error) {
	return provider.vectors, provider.err
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

	day, err := CreateRecommendationDay(3, "2026-06-28T00:00:00Z", 10)
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

	if _, err := AddRecommendationItem(&RecommendationItem{DayID: uintPointer(day.ID), UserID: 3, CandidateID: 11, DedupeKey: "hash-1", Rank: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := AddRecommendationItem(&RecommendationItem{DayID: uintPointer(day.ID), UserID: 3, CandidateID: 11, DedupeKey: "hash-2", Rank: 2}); !errors.Is(err, ErrDuplicateRecommendationItem) {
		t.Fatalf("duplicate candidate err = %v, want ErrDuplicateRecommendationItem", err)
	}
	if _, err := AddRecommendationItem(&RecommendationItem{DayID: uintPointer(day.ID), UserID: 3, CandidateID: 12, DedupeKey: "hash-1", Rank: 2}); !errors.Is(err, ErrDuplicateRecommendationItem) {
		t.Fatalf("duplicate dedupe key err = %v, want ErrDuplicateRecommendationItem", err)
	}

	snapshot, err := GetRecommendationDaySnapshot(3, "2026-06-28")
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Day.ID != day.ID || len(snapshot.Items) != 1 || snapshot.Items[0].CandidateID != 11 {
		t.Fatalf("snapshot = %#v", snapshot)
	}
	if snapshot.Day.RecommendationDate != "2026-06-28" {
		t.Fatalf("snapshot date = %q", snapshot.Day.RecommendationDate)
	}
	days, err := ListRecommendationDays(3, "2026-06-28T00:00:00Z", "", 1, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(days) != 1 || days[0].RecommendationDate != "2026-06-28" {
		t.Fatalf("days = %#v", days)
	}
}

func TestRecommendationFeedbackAndBlockRules(t *testing.T) {
	setupSQLiteDB(t)

	day, err := CreateRecommendationDay(5, "2026-06-28", 10)
	if err != nil {
		t.Fatal(err)
	}
	item, err := AddRecommendationItem(&RecommendationItem{DayID: uintPointer(day.ID), UserID: 5, CandidateID: 21, DedupeKey: "topic-1", Rank: 1})
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
	if _, err := AddRecommendationItem(&RecommendationItem{DayID: uintPointer(oldDay.ID), UserID: 9, CandidateID: dupe.ID, DedupeKey: dupe.DedupeKey, Rank: 1}); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&RecommendationDay{}).Where("id = ?", oldDay.ID).Updates(map[string]interface{}{
		"status": RecommendationDayStatusPublished, "actual_count": 1,
	}).Error; err != nil {
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

func TestRegenerateDailyRecommendationsPreservesPublishedDayAndFeedback(t *testing.T) {
	setupSQLiteDB(t)
	settings := DefaultRecommendationSettings(15)
	settings.DailyLimit = 1
	settings.Enabled = false
	if _, err := SaveRecommendationSettings(&settings); err != nil {
		t.Fatal(err)
	}
	first := createReadyCandidate(t, "https://first.example/manual", "First Manual", []string{"Go"}, "manual-first", 0.7, 0.5)
	initial, err := RegenerateDailyRecommendations(context.Background(), 15, "2026-06-28")
	if err != nil {
		t.Fatal(err)
	}
	if len(initial.Items) != 1 || initial.Items[0].CandidateID != first.ID {
		t.Fatalf("initial snapshot = %#v", initial)
	}
	if _, _, err := RecordRecommendationFeedback(15, initial.Items[0].ID, RecommendationFeedbackValuable, nil); err != nil {
		t.Fatal(err)
	}
	createReadyCandidate(t, "https://second.example/manual", "Second Manual", []string{"Go"}, "manual-second", 0.95, 0.9)
	idempotent, err := GenerateDailyRecommendations(context.Background(), 15, "2026-06-28")
	if err != nil {
		t.Fatal(err)
	}
	if len(idempotent.Items) != 1 || idempotent.Items[0].CandidateID != first.ID {
		t.Fatalf("regular generation should keep existing day: %#v", idempotent.Items)
	}

	regenerated, err := RegenerateDailyRecommendations(context.Background(), 15, "2026-06-28")
	if err != nil {
		t.Fatal(err)
	}
	if len(regenerated.Items) != 1 || regenerated.Items[0].ID != initial.Items[0].ID || regenerated.Items[0].CandidateID != first.ID {
		t.Fatalf("retry changed published snapshot = %#v", regenerated.Items)
	}
	var itemCount int64
	if err := db.Model(&RecommendationItem{}).Where("user_id = ? AND day_id = ?", 15, regenerated.Day.ID).Count(&itemCount).Error; err != nil {
		t.Fatal(err)
	}
	if itemCount != 1 {
		t.Fatalf("item count = %d, want 1", itemCount)
	}
	var oldItemCount int64
	if err := db.Model(&RecommendationItem{}).Where("id = ?", initial.Items[0].ID).Count(&oldItemCount).Error; err != nil {
		t.Fatal(err)
	}
	if oldItemCount != 1 {
		t.Fatalf("old item count = %d, want 1", oldItemCount)
	}
	var feedbackCount int64
	if err := db.Model(&RecommendationFeedback{}).Where("recommendation_item_id = ?", initial.Items[0].ID).Count(&feedbackCount).Error; err != nil {
		t.Fatal(err)
	}
	if feedbackCount != 1 {
		t.Fatalf("feedback count = %d, want 1", feedbackCount)
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
	item, err := AddRecommendationItem(&RecommendationItem{DayID: uintPointer(oldDay.ID), UserID: 10, CandidateID: liked.ID, DedupeKey: liked.DedupeKey, Rank: 1})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := RecordRecommendationFeedback(10, item.ID, RecommendationFeedbackDeepRead, nil); err != nil {
		t.Fatal(err)
	}
	postgres := createReadyCandidate(t, "https://new.example/postgres", "New PostgreSQL", []string{"PostgreSQL"}, "new-pg", 0.45, 0.6)
	generic := createReadyCandidate(t, "https://generic.example/generic", "Generic", []string{"Release"}, "generic", 0.65, 0.4)

	snapshot, err := GenerateDailyRecommendations(context.Background(), 10, "2026-06-28")
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Items) != 2 {
		t.Fatalf("items = %#v", snapshot.Items)
	}
	gotIDs := map[uint]bool{snapshot.Items[0].CandidateID: true, snapshot.Items[1].CandidateID: true}
	if !gotIDs[postgres.ID] || !gotIDs[generic.ID] {
		t.Fatalf("ranked items = %#v, want postgres %d and generic %d", snapshot.Items, postgres.ID, generic.ID)
	}
}

func TestGenerateDailyRecommendationsDoesNotMutateCandidateOnPublishPath(t *testing.T) {
	setupSQLiteDB(t)
	settings := DefaultRecommendationSettings(13)
	settings.DailyLimit = 1
	settings.Enabled = true
	if _, err := SaveRecommendationSettings(&settings); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	candidate := DiscoveryCandidate{
		SourceID:         1,
		SourceName:       "pending.example",
		URL:              "https://pending.example/post?utm_source=test",
		Title:            "Go RSS tutorial",
		Summary:          "A Go RSS tutorial from a feed",
		Status:           DiscoveryCandidateStatusNew,
		EnrichmentStatus: RecommendationEnrichmentStatusPending,
		ProcessingState:  discovery.DiscoveryProcessingReady,
		EligibilityState: discovery.DiscoveryEligibilityEligible,
		DedupeState:      discovery.DiscoveryDedupeReady,
		BodyText:         strings.Repeat("A complete locally extracted article body with enough information for deterministic enrichment. ", 4),
		LastSeenAt:       now,
		PublishedAt:      &now,
	}
	if err := db.Create(&candidate).Error; err != nil {
		t.Fatal(err)
	}
	summaryOnly := DiscoveryCandidate{
		SourceID: 2, SourceName: "summary.example", URL: "https://summary.example/post",
		Title: "Summary only", Summary: "A feed summary without an extracted body",
		Status: DiscoveryCandidateStatusNew, EnrichmentStatus: RecommendationEnrichmentStatusPending,
		ProcessingState: discovery.DiscoveryProcessingFetchPending, EligibilityState: discovery.DiscoveryEligibilityUnknown,
		LastSeenAt: now, PublishedAt: &now,
	}
	if err := db.Create(&summaryOnly).Error; err != nil {
		t.Fatal(err)
	}
	snapshot, err := GenerateDailyRecommendations(context.Background(), 13, "2026-06-28")
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Items) != 1 || snapshot.Items[0].CandidateID != candidate.ID {
		t.Fatalf("snapshot = %#v", snapshot)
	}
	var enriched DiscoveryCandidate
	if err := db.First(&enriched, candidate.ID).Error; err != nil {
		t.Fatal(err)
	}
	if enriched.Topics != "" || enriched.Entities != "" || enriched.DedupeKey != "" {
		t.Fatalf("publish path mutated candidate metadata = %#v", enriched)
	}
	if err := db.First(&summaryOnly, summaryOnly.ID).Error; err != nil {
		t.Fatal(err)
	}
	if summaryOnly.Topics != "" || summaryOnly.Summary != "A feed summary without an extracted body" {
		t.Fatalf("summary-only candidate was mutated: %#v", summaryOnly)
	}
}

func TestGenerateDailyRecommendationsRerankerValidationAndFailure(t *testing.T) {
	setupSQLiteDB(t)
	settings := DefaultRecommendationSettings(11)
	settings.DailyLimit = 2
	settings.Enabled = true
	if _, err := SaveRecommendationSettings(&settings); err != nil {
		t.Fatal(err)
	}
	first := createReadyCandidate(t, "https://first.example/post", "First", []string{"Go"}, "first", 0.9, 0.7)
	second := createReadyCandidate(t, "https://second.example/post", "Second", []string{"PostgreSQL"}, "second", 0.6, 0.5)
	reranker := fakeReranker{result: RerankResult{
		Model:         "test-reranker",
		PromptVersion: "test-v1",
		Items: []RerankItem{
			{CandidateID: 9999, Rank: 1, Reason: "invalid", Confidence: 1},
			{CandidateID: second.ID, Rank: 2, Reason: "reranked second", Confidence: 0.9},
			{CandidateID: first.ID, Rank: 3, Reason: "reranked first", Confidence: 0.7},
		},
	}}
	snapshot, err := GenerateDailyRecommendationsWithReranker(context.Background(), 11, "2026-06-28", reranker)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Day.LLMModel != "test-reranker" || snapshot.Day.PromptVersion != "test-v1" {
		t.Fatalf("rerank metadata day = %#v", snapshot.Day)
	}
	if len(snapshot.Items) != 2 || snapshot.Items[0].CandidateID != second.ID || snapshot.Items[0].Reason != "reranked second" || snapshot.Items[0].RerankScore != 0.9 {
		t.Fatalf("reranked items = %#v", snapshot.Items)
	}
	if snapshot.Items[0].Candidate.ID != second.ID || snapshot.Items[0].Candidate.Title != "Second" {
		t.Fatalf("snapshot candidate not attached: %#v", snapshot.Items[0])
	}

	if err := db.Model(&DiscoveryCandidate{}).Where("id IN ?", []uint{first.ID, second.ID}).Update("eligibility_state", discovery.DiscoveryEligibilityIneligible).Error; err != nil {
		t.Fatal(err)
	}
	settings = DefaultRecommendationSettings(12)
	settings.DailyLimit = 2
	settings.Enabled = true
	if _, err := SaveRecommendationSettings(&settings); err != nil {
		t.Fatal(err)
	}
	fallbackFirst := createReadyCandidate(t, "https://fallback-a.example/post", "Fallback First", []string{"Go"}, "fallback-first", 0.9, 0.7)
	createReadyCandidate(t, "https://fallback-b.example/post", "Fallback Second", []string{"PostgreSQL"}, "fallback-second", 0.5, 0.5)
	failed, err := GenerateDailyRecommendationsWithReranker(context.Background(), 12, "2026-06-28", fakeReranker{err: errors.New("reranker unavailable")})
	if err == nil {
		t.Fatal("reranker failure should not publish")
	}
	if failed != nil {
		t.Fatalf("unexpected snapshot = %#v", failed)
	}
	snapshot, snapErr := GetRecommendationDaySnapshot(12, "2026-06-28")
	if snapErr != nil {
		t.Fatal(snapErr)
	}
	if snapshot.Day.Status != RecommendationDayStatusFailed || snapshot.Day.Degraded || len(snapshot.Items) != 0 {
		t.Fatalf("failed day = %#v", snapshot)
	}
	_ = fallbackFirst
}

func TestRecommendationGenerationDueUsesSettingsTime(t *testing.T) {
	settings := RecommendationSettings{UserID: 1, Timezone: "Asia/Shanghai", GenerationTime: "07:30"}
	before := time.Date(2026, 6, 28, 7, 29, 0, 0, time.FixedZone("CST", 8*60*60))
	after := time.Date(2026, 6, 28, 7, 30, 0, 0, time.FixedZone("CST", 8*60*60))
	if recommendationGenerationDue(settings, before) {
		t.Fatal("generation should not be due before configured time")
	}
	if !recommendationGenerationDue(settings, after) {
		t.Fatal("generation should be due at configured time")
	}
	if got := recommendationDateForSettings(settings, after); got != "2026-06-28" {
		t.Fatalf("recommendation date = %q", got)
	}
}

func TestRecommendationVectorHelpers(t *testing.T) {
	literal, err := FormatPGVector([]float32{0.1, -2, 3.5})
	if err != nil {
		t.Fatal(err)
	}
	if literal != "[0.1,-2,3.5]" {
		t.Fatalf("literal = %q", literal)
	}
	parsed, err := ParsePGVector(literal)
	if err != nil {
		t.Fatal(err)
	}
	if len(parsed) != 3 || parsed[1] != -2 {
		t.Fatalf("parsed = %#v", parsed)
	}
	average := averageVectors([][]float32{{1, 2}, {3, 4}, {9}})
	if len(average) != 2 || average[0] != 2 || average[1] != 3 {
		t.Fatalf("average = %#v", average)
	}
	jsonVector, err := parseFloat32JSONVector(marshalVector([]float32{0.25, 0.75}))
	if err != nil {
		t.Fatal(err)
	}
	if len(jsonVector) != 2 || jsonVector[0] != 0.25 {
		t.Fatalf("json vector = %#v", jsonVector)
	}
}

func TestEmbedDiscoveryCandidateStoresModelWithoutPostgresVector(t *testing.T) {
	setupSQLiteDB(t)
	candidate := createReadyCandidate(t, "https://embed.example/post", "Embedding Post", []string{"Go"}, "embed", 0.8, 0.7)
	err := EmbedDiscoveryCandidate(context.Background(), candidate.ID, fakeEmbeddingProvider{vectors: [][]float32{{0.1, 0.2}}}, "embedding-model")
	if err != nil {
		t.Fatal(err)
	}
	var got DiscoveryCandidate
	if err := db.First(&got, candidate.ID).Error; err != nil {
		t.Fatal(err)
	}
	if got.EmbeddingModel != "embedding-model" {
		t.Fatalf("embedding model = %q", got.EmbeddingModel)
	}
}

func TestEmbedReadyDiscoveryCandidatesIgnoresLegacyEnrichmentStatus(t *testing.T) {
	setupSQLiteDB(t)
	now := time.Now()
	pending := DiscoveryCandidate{
		SourceID: 1, SourceName: "pending.example", URL: "https://pending.example/post",
		Title: "Eligible pending enrichment", Status: DiscoveryCandidateStatusNew,
		EnrichmentStatus: RecommendationEnrichmentStatusPending,
		ProcessingState:  discovery.DiscoveryProcessingReady,
		EligibilityState: discovery.DiscoveryEligibilityEligible,
		DedupeState:      discovery.DiscoveryDedupeReady,
		LastSeenAt:       now, PublishedAt: &now,
	}
	if err := db.Create(&pending).Error; err != nil {
		t.Fatal(err)
	}
	ineligible := DiscoveryCandidate{
		SourceID: 2, SourceName: "skip.example", URL: "https://skip.example/post",
		Title: "Not eligible", Status: DiscoveryCandidateStatusNew,
		EnrichmentStatus: RecommendationEnrichmentStatusReady,
		ProcessingState:  discovery.DiscoveryProcessingFetchPending,
		EligibilityState: discovery.DiscoveryEligibilityUnknown,
		LastSeenAt:       now, PublishedAt: &now,
	}
	if err := db.Create(&ineligible).Error; err != nil {
		t.Fatal(err)
	}
	count, err := EmbedReadyDiscoveryCandidates(context.Background(), 10, fakeEmbeddingProvider{vectors: [][]float32{{0.1, 0.2}}}, "embedding-model")
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("embedded count = %d", count)
	}
	var got DiscoveryCandidate
	if err := db.First(&got, pending.ID).Error; err != nil {
		t.Fatal(err)
	}
	if got.EmbeddingModel != "embedding-model" {
		t.Fatalf("pending candidate embedding model = %q", got.EmbeddingModel)
	}
	if err := db.First(&ineligible, ineligible.ID).Error; err != nil {
		t.Fatal(err)
	}
	if ineligible.EmbeddingModel != "" {
		t.Fatalf("ineligible candidate was embedded: %#v", ineligible)
	}
}

func TestRunGenerateDailyRecommendationJob(t *testing.T) {
	setupSQLiteDB(t)
	settings := DefaultRecommendationSettings(14)
	settings.DailyLimit = 1
	settings.Enabled = true
	if _, err := SaveRecommendationSettings(&settings); err != nil {
		t.Fatal(err)
	}
	candidate := createReadyCandidate(t, "https://worker.example/post", "Worker Post", []string{"Go"}, "worker", 0.8, 0.7)
	if err := RunGenerateDailyRecommendationJob(context.Background(), 14, "2026-06-28"); err != nil {
		t.Fatal(err)
	}
	snapshot, err := GetRecommendationDaySnapshot(14, "2026-06-28")
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Items) != 1 || snapshot.Items[0].CandidateID != candidate.ID {
		t.Fatalf("snapshot = %#v", snapshot)
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
		ProcessingState:  discovery.DiscoveryProcessingReady,
		EligibilityState: discovery.DiscoveryEligibilityEligible,
		DedupeState:      discovery.DiscoveryDedupeReady,
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
