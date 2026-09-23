package recommendation

import (
	"DataArk/discovery"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestDailyDigestM14UsesUserLocalDateAndPreservesHistoricalTimezone(t *testing.T) {
	setupSQLiteDB(t)
	now := time.Date(2026, 1, 1, 16, 30, 0, 0, time.UTC)
	useRecommendationTestClock(t, now)
	shanghai := DefaultRecommendationSettings(801)
	shanghai.Timezone = "Asia/Shanghai"
	losAngeles := DefaultRecommendationSettings(802)
	losAngeles.Timezone = "America/Los_Angeles"
	for _, settings := range []*RecommendationSettings{&shanghai, &losAngeles} {
		if _, err := SaveRecommendationSettings(settings); err != nil {
			t.Fatal(err)
		}
	}
	shanghaiDay, err := CreateRecommendationDay(801, "", 10)
	if err != nil {
		t.Fatal(err)
	}
	losAngelesDay, err := CreateRecommendationDay(802, "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if shanghaiDay.RecommendationDate != "2026-01-02" || losAngelesDay.RecommendationDate != "2026-01-01" {
		t.Fatalf("local dates = %q/%q", shanghaiDay.RecommendationDate, losAngelesDay.RecommendationDate)
	}
	shanghai.Timezone = "UTC"
	if _, err := SaveRecommendationSettings(&shanghai); err != nil {
		t.Fatal(err)
	}
	stored, err := GetRecommendationDaySnapshot(801, "2026-01-02")
	if err != nil {
		t.Fatal(err)
	}
	if stored.Day.Timezone != "Asia/Shanghai" {
		t.Fatalf("historical timezone changed: %#v", stored.Day)
	}
}

func TestDailyDigestM14PublishedSnapshotIsImmutableAndByteStable(t *testing.T) {
	setupSQLiteDB(t)
	useRecommendationTestClock(t, time.Date(2026, 2, 1, 8, 0, 0, 0, time.UTC))
	settings := DefaultRecommendationSettings(803)
	settings.DailyLimit = 1
	if _, err := SaveRecommendationSettings(&settings); err != nil {
		t.Fatal(err)
	}
	candidate := createReadyCandidate(t, "https://immutable.example/one", "Frozen title", []string{"systems"}, "immutable-one", 0.9, 0.8)
	first, err := GenerateDailyRecommendations(context.Background(), 803, "2026-02-01")
	if err != nil {
		t.Fatal(err)
	}
	firstJSON, _ := json.Marshal(first)
	if first.Day.Status != RecommendationDayStatusPublished || first.Day.PublishedAt == nil || len(first.Items) != 1 {
		t.Fatalf("published snapshot = %#v", first)
	}
	if err := discovery.UpdateCandidates(db.Model(&DiscoveryCandidate{}).Where("id = ?", candidate.ID), map[string]interface{}{
		"title": "Changed live title", "summary": "Changed live summary", "source_name": "changed.example", "quality_score": 0.1,
	}).Error; err != nil {
		t.Fatal(err)
	}
	retried, err := RegenerateDailyRecommendations(context.Background(), 803, "2026-02-01")
	if err != nil {
		t.Fatal(err)
	}
	retriedJSON, _ := json.Marshal(retried)
	if !bytes.Equal(firstJSON, retriedJSON) || retried.Items[0].Candidate.Title != "Frozen title" {
		t.Fatalf("snapshot changed:\nfirst=%s\nretry=%s", firstJSON, retriedJSON)
	}
	if _, err := AddRecommendationItem(&RecommendationItem{DayID: uintPointer(first.Day.ID), UserID: 803, CandidateID: candidate.ID + 100, Rank: 2}); !errors.Is(err, ErrRecommendationDayImmutable) {
		t.Fatalf("published append error = %v", err)
	}
}

func TestDailyDigestM14SupplementOnlyAppendsMissingItems(t *testing.T) {
	setupSQLiteDB(t)
	clock := useRecommendationTestClock(t, time.Date(2026, 3, 1, 8, 0, 0, 0, time.UTC))
	settings := DefaultRecommendationSettings(804)
	settings.DailyLimit = 2
	if _, err := SaveRecommendationSettings(&settings); err != nil {
		t.Fatal(err)
	}
	createReadyCandidate(t, "https://supplement.example/first", "First", []string{"go"}, "supplement-first", 0.9, 0.8)
	initial, err := GenerateDailyRecommendations(context.Background(), 804, "2026-03-01")
	if err != nil {
		t.Fatal(err)
	}
	if initial.Day.ActualCount != 1 || initial.Day.Status != RecommendationDayStatusPublished {
		t.Fatalf("initial = %#v", initial)
	}
	firstID := initial.Items[0].ID
	publishedAt := *initial.Day.PublishedAt
	clock.Advance(time.Hour)
	createReadyCandidate(t, "https://supplement-two.example/second", "Second", []string{"rust"}, "supplement-second", 0.85, 0.75)
	supplemented, err := SupplementDailyRecommendations(context.Background(), 804, "2026-03-01")
	if err != nil {
		t.Fatal(err)
	}
	if supplemented.Day.Status != RecommendationDayStatusSupplemented || supplemented.Day.ActualCount != 2 || supplemented.Day.SupplementedAt == nil || !supplemented.Day.PublishedAt.Equal(publishedAt) {
		t.Fatalf("supplemented day = %#v", supplemented.Day)
	}
	if len(supplemented.Items) != 2 || supplemented.Items[0].ID != firstID || supplemented.Items[0].Supplemental || !supplemented.Items[1].Supplemental || supplemented.Items[1].Rank != 2 {
		t.Fatalf("supplemented items = %#v", supplemented.Items)
	}
	again, err := SupplementDailyRecommendations(context.Background(), 804, "2026-03-01")
	if err != nil {
		t.Fatal(err)
	}
	if len(again.Items) != 2 || again.Items[0].ID != firstID || again.Items[1].ID != supplemented.Items[1].ID {
		t.Fatalf("repeat supplement changed items: %#v", again.Items)
	}
}

func TestDailyDigestM14SupplementFillsFromSameSourceWhenShort(t *testing.T) {
	setupSQLiteDB(t)
	clock := useRecommendationTestClock(t, time.Date(2026, 3, 2, 8, 0, 0, 0, time.UTC))
	settings := DefaultRecommendationSettings(806)
	settings.DailyLimit = 2
	if _, err := SaveRecommendationSettings(&settings); err != nil {
		t.Fatal(err)
	}
	createReadyCandidate(t, "https://same-source.example/first", "First", []string{"go"}, "same-source-first", 0.9, 0.8)
	initial, err := GenerateDailyRecommendations(context.Background(), 806, "2026-03-02")
	if err != nil {
		t.Fatal(err)
	}
	if initial.Day.ActualCount != 1 {
		t.Fatalf("initial = %#v", initial)
	}
	clock.Advance(time.Hour)
	second := createReadyCandidate(t, "https://same-source.example/second", "Second", []string{"rust"}, "same-source-second", 0.95, 0.75)
	supplemented, err := SupplementDailyRecommendations(context.Background(), 806, "2026-03-02")
	if err != nil {
		t.Fatal(err)
	}
	if supplemented.Day.ActualCount != 2 || len(supplemented.Items) != 2 || supplemented.Items[1].CandidateID != second.ID {
		t.Fatalf("same-source supplement should fill the shortage: %#v", supplemented.Items)
	}
}

func TestDailyDigestM14FailureIsRetryableAndRerankerFailureDoesNotPublish(t *testing.T) {
	setupSQLiteDB(t)
	useRecommendationTestClock(t, time.Date(2026, 4, 1, 8, 0, 0, 0, time.UTC))
	settings := DefaultRecommendationSettings(805)
	settings.DailyLimit = 1
	if _, err := SaveRecommendationSettings(&settings); err != nil {
		t.Fatal(err)
	}
	createReadyCandidate(t, "https://retry.example/one", "Retry", []string{"ops"}, "retry-one", 0.9, 0.8)
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := GenerateDailyRecommendations(cancelled, 805, "2026-04-01"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled generation error = %v", err)
	}
	failed, err := GetRecommendationDaySnapshot(805, "2026-04-01")
	if err != nil {
		t.Fatal(err)
	}
	if failed.Day.Status != RecommendationDayStatusFailed || failed.Day.FailureReason == "" || len(failed.Items) != 0 {
		t.Fatalf("failed day = %#v", failed)
	}
	failedRetry, err := GenerateDailyRecommendationsWithReranker(context.Background(), 805, "2026-04-01", fakeReranker{err: errors.New("local model down")})
	if err == nil {
		t.Fatal("reranker failure should not publish")
	}
	if failedRetry != nil {
		t.Fatalf("unexpected snapshot = %#v", failedRetry)
	}
	stillFailed, err := GetRecommendationDaySnapshot(805, "2026-04-01")
	if err != nil {
		t.Fatal(err)
	}
	if stillFailed.Day.Status != RecommendationDayStatusFailed || stillFailed.Day.Degraded || stillFailed.Day.FailureReason == "" || len(stillFailed.Items) != 0 {
		t.Fatalf("failed day = %#v", stillFailed)
	}
}
