package recommendation

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

type fakeDigestSummaryGenerator struct {
	output DigestSummaryOutput
	err    error
	calls  int
}

func (generator *fakeDigestSummaryGenerator) GenerateDigestSummary(_ context.Context, _ DigestSummaryInput) (DigestSummaryOutput, error) {
	generator.calls++
	if generator.err != nil {
		return DigestSummaryOutput{}, generator.err
	}
	return generator.output, nil
}

func seedDigestSummaryDay(t *testing.T, userID uint, candidateCount int) *RecommendationDaySnapshot {
	t.Helper()
	setupSQLiteDB(t)
	useRecommendationTestClock(t, time.Date(2026, 6, 1, 8, 0, 0, 0, time.UTC))
	settings := DefaultRecommendationSettings(userID)
	settings.DailyLimit = candidateCount
	if _, err := SaveRecommendationSettings(&settings); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < candidateCount; index++ {
		createReadyCandidate(t,
			fmt.Sprintf("https://digest-%d.example/article", index),
			"Digest article "+string(rune('a'+index)),
			[]string{"go", "systems"},
			"digest-"+string(rune('a'+index)),
			0.9, 0.8)
	}
	snapshot, err := GenerateDailyRecommendations(context.Background(), userID, "2026-06-01")
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Day.Status != RecommendationDayStatusPublished || len(snapshot.Items) != candidateCount {
		t.Fatalf("seeded snapshot = %#v", snapshot.Day)
	}
	return snapshot
}

func TestDigestSummaryIsCachedPerDay(t *testing.T) {
	snapshot := seedDigestSummaryDay(t, 901, 2)
	generator := &fakeDigestSummaryGenerator{output: DigestSummaryOutput{
		Overview: "今日两篇系统类文章。", Highlights: []string{"看点一"}, Topics: []string{"go"},
		Model: "fake-model", PromptVersion: "fake-v1",
	}}
	first, err := GetRecommendationDaySummaryWithGenerator(context.Background(), 901, "2026-06-01", generator)
	if err != nil {
		t.Fatal(err)
	}
	if generator.calls != 1 {
		t.Fatalf("calls after first request = %d", generator.calls)
	}
	if !first.Available || first.Overview != "今日两篇系统类文章。" || first.Model != "fake-model" || first.GeneratedAt == nil {
		t.Fatalf("first summary = %#v", first)
	}

	var stored RecommendationDay
	if err := db.Where("id = ?", snapshot.Day.ID).First(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if stored.SummaryText != first.Overview || stored.SummaryModel != "fake-model" || stored.SummaryActualCount != stored.ActualCount {
		t.Fatalf("persisted day = %#v", stored)
	}

	second, err := GetRecommendationDaySummaryWithGenerator(context.Background(), 901, "2026-06-01", generator)
	if err != nil {
		t.Fatal(err)
	}
	if generator.calls != 1 {
		t.Fatalf("generator called again on cache hit: %d", generator.calls)
	}
	if second.Overview != first.Overview || second.Model != first.Model {
		t.Fatalf("cached summary changed: %#v", second)
	}
}

func TestDigestSummaryUnavailableBeforeGeneration(t *testing.T) {
	setupSQLiteDB(t)
	useRecommendationTestClock(t, time.Date(2026, 6, 1, 8, 0, 0, 0, time.UTC))
	generator := &fakeDigestSummaryGenerator{}
	summary, err := GetRecommendationDaySummaryWithGenerator(context.Background(), 902, "2026-06-01", generator)
	if err != nil {
		t.Fatal(err)
	}
	if summary.Available || summary.Reason == "" || generator.calls != 0 {
		t.Fatalf("summary = %#v calls = %d", summary, generator.calls)
	}
	var count int64
	if err := db.Model(&RecommendationDay{}).Where("summary_text <> ''").Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("summary persisted for missing day: %d", count)
	}
}

func TestDigestSummaryFallsBackToRuleGenerator(t *testing.T) {
	seedDigestSummaryDay(t, 903, 2)
	generator := &fakeDigestSummaryGenerator{err: errors.New("provider down")}
	summary, err := GetRecommendationDaySummaryWithGenerator(context.Background(), 903, "2026-06-01", generator)
	if err != nil {
		t.Fatal(err)
	}
	if generator.calls != 1 {
		t.Fatalf("calls = %d", generator.calls)
	}
	if !summary.Available || summary.Model != RuleBasedProviderModel || !strings.Contains(summary.Overview, "共推荐 2 篇") {
		t.Fatalf("summary = %#v", summary)
	}

	second, err := GetRecommendationDaySummaryWithGenerator(context.Background(), 903, "2026-06-01", generator)
	if err != nil {
		t.Fatal(err)
	}
	if generator.calls != 1 {
		t.Fatalf("degraded summary was not persisted, calls = %d", generator.calls)
	}
	if second.Model != RuleBasedProviderModel {
		t.Fatalf("second summary = %#v", second)
	}
}

func TestDigestSummaryRegeneratesAfterSupplement(t *testing.T) {
	snapshot := seedDigestSummaryDay(t, 904, 1)
	generator := &fakeDigestSummaryGenerator{output: DigestSummaryOutput{Overview: "一篇。", Highlights: []string{}, Topics: []string{}}}
	if _, err := GetRecommendationDaySummaryWithGenerator(context.Background(), 904, "2026-06-01", generator); err != nil {
		t.Fatal(err)
	}
	if generator.calls != 1 {
		t.Fatalf("calls after first request = %d", generator.calls)
	}
	if err := db.Model(&RecommendationDay{}).Where("id = ?", snapshot.Day.ID).
		Update("actual_count", snapshot.Day.ActualCount+1).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := GetRecommendationDaySummaryWithGenerator(context.Background(), 904, "2026-06-01", generator); err != nil {
		t.Fatal(err)
	}
	if generator.calls != 2 {
		t.Fatalf("expected regeneration after count change, calls = %d", generator.calls)
	}
}

func TestRuleBasedDigestSummaryGenerator(t *testing.T) {
	input := DigestSummaryInput{
		Date: "2026-06-01",
		Items: []DigestSummaryItem{
			{Rank: 1, Title: "Go concurrency", Source: "go.example", Topics: []string{"go", "systems"}},
			{Rank: 2, Title: "Rust async", Source: "rust.example", Topics: []string{"rust"}},
			{Rank: 3, Title: "Kernel notes", Source: "go.example", Topics: []string{"systems", "kernel"}},
		},
	}
	first, err := RuleBasedDigestSummaryGenerator{}.GenerateDigestSummary(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if first.Model != RuleBasedProviderModel || first.PromptVersion == "" {
		t.Fatalf("rule output identity = %#v", first)
	}
	if !strings.Contains(first.Overview, "共推荐 3 篇") || !strings.Contains(first.Overview, "覆盖 2 个来源") || !strings.Contains(first.Overview, "systems") {
		t.Fatalf("overview = %q", first.Overview)
	}
	if len(first.Highlights) != 3 || !strings.Contains(first.Highlights[0], "《Go concurrency》") || !strings.Contains(first.Highlights[0], "go.example") {
		t.Fatalf("highlights = %#v", first.Highlights)
	}
	if len(first.Topics) != 3 || first.Topics[0] != "systems" {
		t.Fatalf("topics = %#v", first.Topics)
	}
	second, err := RuleBasedDigestSummaryGenerator{}.GenerateDigestSummary(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if second.Overview != first.Overview {
		t.Fatalf("rule generator not deterministic: %q vs %q", second.Overview, first.Overview)
	}
}
