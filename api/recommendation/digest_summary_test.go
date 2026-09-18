package recommendation

import (
	"context"
	"errors"
	"fmt"
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

	read, err := GetRecommendationDaySummary(context.Background(), 901, "2026-06-01")
	if err != nil {
		t.Fatal(err)
	}
	if generator.calls != 1 {
		t.Fatalf("GET read path called generator: %d", generator.calls)
	}
	if read.Overview != first.Overview || !read.Available {
		t.Fatalf("read summary = %#v", read)
	}
}

func TestDigestSummaryUnavailableBeforeGeneration(t *testing.T) {
	setupSQLiteDB(t)
	useRecommendationTestClock(t, time.Date(2026, 6, 1, 8, 0, 0, 0, time.UTC))
	summary, err := GetRecommendationDaySummary(context.Background(), 902, "2026-06-01")
	if err != nil {
		t.Fatal(err)
	}
	if summary.Available || summary.Reason == "" {
		t.Fatalf("summary = %#v", summary)
	}
	var count int64
	if err := db.Model(&RecommendationDay{}).Where("summary_text <> ''").Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("summary persisted for missing day: %d", count)
	}
}

func TestDigestSummaryGetDoesNotGenerate(t *testing.T) {
	seedDigestSummaryDay(t, 906, 2)
	generator := &fakeDigestSummaryGenerator{output: DigestSummaryOutput{Overview: "不应被 GET 触发。"}}
	summary, err := GetRecommendationDaySummary(context.Background(), 906, "2026-06-01")
	if err != nil {
		t.Fatal(err)
	}
	if summary.Available || summary.Reason != "日报总结生成中" || generator.calls != 0 {
		t.Fatalf("summary = %#v calls = %d", summary, generator.calls)
	}
}

func TestDigestSummaryFailsWithoutPersisting(t *testing.T) {
	seedDigestSummaryDay(t, 903, 2)
	generator := &fakeDigestSummaryGenerator{err: errors.New("provider down")}
	summary, err := GetRecommendationDaySummaryWithGenerator(context.Background(), 903, "2026-06-01", generator)
	if err == nil || summary != nil {
		t.Fatalf("expected generator error, got summary=%#v err=%v", summary, err)
	}
	if generator.calls != 1 {
		t.Fatalf("calls = %d", generator.calls)
	}
	read, err := GetRecommendationDaySummary(context.Background(), 903, "2026-06-01")
	if err != nil {
		t.Fatal(err)
	}
	if read.Available || read.Reason != "日报总结生成中" {
		t.Fatalf("failed summary should not persist: %#v", read)
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
	read, err := GetRecommendationDaySummary(context.Background(), 904, "2026-06-01")
	if err != nil {
		t.Fatal(err)
	}
	if read.Available || generator.calls != 1 {
		t.Fatalf("GET regenerated after count change: %#v calls=%d", read, generator.calls)
	}
	if _, err := GetRecommendationDaySummaryWithGenerator(context.Background(), 904, "2026-06-01", generator); err != nil {
		t.Fatal(err)
	}
	if generator.calls != 2 {
		t.Fatalf("expected regeneration after count change, calls = %d", generator.calls)
	}
}
