package recommendation

import (
	"DataArk/discovery"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

func createDiscoveryFeedCandidates(t *testing.T, count int) []DiscoveryCandidate {
	t.Helper()
	now := time.Now()
	candidates := make([]DiscoveryCandidate, 0, count)
	for index := 0; index < count; index++ {
		candidate := DiscoveryCandidate{
			SourceID: 1, SourceName: fmt.Sprintf("source-%d", index),
			URL: fmt.Sprintf("https://feed.example/articles/%d", index), Title: fmt.Sprintf("Article %02d", index),
			Summary: fmt.Sprintf("Summary %d", index), Topics: fmt.Sprintf("[\"topic-%d\"]", index%6),
			QualityScore: float64(count-index) / float64(count), DepthScore: 0.7,
			Status: discovery.DiscoveryCandidateStatusNew, ProcessingState: discovery.DiscoveryProcessingReady,
			EligibilityState: discovery.DiscoveryEligibilityEligible, DedupeState: discovery.DiscoveryDedupeReady,
			DedupeKey: fmt.Sprintf("feed-%d", index), PublishedAt: &now, LastSeenAt: now,
		}
		if err := db.Create(&candidate).Error; err != nil {
			t.Fatal(err)
		}
		candidates = append(candidates, candidate)
	}
	return candidates
}

func assertUniqueRecommendationSources(t *testing.T, items []RecommendationItem) {
	t.Helper()
	seen := make(map[string]uint, len(items))
	for _, item := range items {
		source := recommendationSourceKeyFromItem(item)
		if previous, exists := seen[source]; exists {
			t.Fatalf("source %q appeared on candidates %d and %d", source, previous, item.CandidateID)
		}
		seen[source] = item.CandidateID
	}
}

func TestDiscoveryFeedCurrentAndRefreshAvoidRecentRepeats(t *testing.T) {
	setupSQLiteDB(t)
	createDiscoveryFeedCandidates(t, 25)

	first, err := RefreshDiscoveryFeed(context.Background(), 1001, 10)
	if err != nil {
		t.Fatal(err)
	}
	if first.Batch == nil || first.Batch.ActualCount != 10 || len(first.Items) != 10 {
		t.Fatalf("first snapshot = %#v", first)
	}
	firstIDs := map[uint]bool{}
	assertUniqueRecommendationSources(t, first.Items)
	for _, item := range first.Items {
		firstIDs[item.CandidateID] = true
		if item.DayID != nil || item.FeedBatchID == nil || *item.FeedBatchID != first.Batch.ID {
			t.Fatalf("invalid feed parent: %#v", item)
		}
	}

	current, err := GetCurrentDiscoveryFeed(1001)
	if err != nil {
		t.Fatal(err)
	}
	if current.Batch == nil || current.Batch.ID != first.Batch.ID || len(current.Items) != 10 {
		t.Fatalf("current snapshot = %#v", current)
	}
	var exposureTotal uint
	var states []discovery.UserCandidateState
	if err := db.Where("user_id = ?", 1001).Find(&states).Error; err != nil {
		t.Fatal(err)
	}
	for _, state := range states {
		exposureTotal += state.ExposureCount
	}
	if exposureTotal != 10 {
		t.Fatalf("GET changed exposure total: %d", exposureTotal)
	}

	second, err := RefreshDiscoveryFeed(context.Background(), 1001, 10)
	if err != nil {
		t.Fatal(err)
	}
	if second.Batch == nil || second.Batch.ID == first.Batch.ID || len(second.Items) != 10 {
		t.Fatalf("second snapshot = %#v", second)
	}
	assertUniqueRecommendationSources(t, second.Items)
	for _, item := range second.Items {
		if firstIDs[item.CandidateID] {
			t.Fatalf("candidate %d repeated before inventory exhaustion", item.CandidateID)
		}
	}
	var active int64
	if err := db.Model(&RecommendationFeedBatch{}).Where("user_id = ? AND status = ?", 1001, RecommendationFeedBatchStatusActive).Count(&active).Error; err != nil || active != 1 {
		t.Fatalf("active batches=%d err=%v", active, err)
	}
}

func TestDiscoveryFeedShortageAndPerUserIsolation(t *testing.T) {
	setupSQLiteDB(t)
	createDiscoveryFeedCandidates(t, 4)

	first, err := RefreshDiscoveryFeed(context.Background(), 2001, 10)
	if err != nil {
		t.Fatal(err)
	}
	if first.Batch == nil || first.Batch.RequestedCount != 10 || first.Batch.ActualCount != 4 || len(first.Items) != 4 {
		t.Fatalf("shortage snapshot = %#v", first)
	}
	other, err := RefreshDiscoveryFeed(context.Background(), 2002, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(other.Items) != 4 {
		t.Fatalf("other user items = %d", len(other.Items))
	}
	for index := range first.Items {
		if first.Items[index].CandidateID != other.Items[index].CandidateID {
			t.Fatalf("user-isolated unseen inventory should be independently selectable")
		}
	}
	depleted, err := RefreshDiscoveryFeed(context.Background(), 2001, 10)
	if err != nil {
		t.Fatal(err)
	}
	if depleted.Batch == nil || depleted.Batch.ActualCount != 0 || len(depleted.Items) != 0 {
		t.Fatalf("exhausted inventory should remain short instead of repeating: %#v", depleted)
	}
	if !strings.Contains(depleted.Batch.ShortageReasons, "newInventoryShortage") || strings.Contains(depleted.Batch.ShortageReasons, "reexposure_cooldown") {
		t.Fatalf("exhausted inventory audit = %s", depleted.Batch.ShortageReasons)
	}
}

func TestDiscoveryFeedRefreshSearchesPastExposedRetrievalPage(t *testing.T) {
	setupSQLiteDB(t)
	candidates := createDiscoveryFeedCandidates(t, 110)
	now := time.Now()
	exposed := make(map[uint]bool, 100)
	for _, candidate := range candidates[:100] {
		if err := discovery.RecordUserCandidateExposure(db, 4001, candidate.ID, now); err != nil {
			t.Fatal(err)
		}
		exposed[candidate.ID] = true
	}

	snapshot, err := RefreshDiscoveryFeed(context.Background(), 4001, 10)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Batch == nil || snapshot.Batch.ActualCount != 10 || len(snapshot.Items) != 10 {
		t.Fatalf("deep unseen inventory snapshot = %#v", snapshot)
	}
	for _, item := range snapshot.Items {
		if exposed[item.CandidateID] {
			t.Fatalf("refresh repeated top-page candidate %d despite unseen inventory", item.CandidateID)
		}
		if item.CooldownRepeat {
			t.Fatalf("deep unseen candidate %d was marked as a cooldown repeat", item.CandidateID)
		}
	}
	if strings.Contains(snapshot.Batch.ShortageReasons, "reexposure_cooldown") || strings.Contains(snapshot.Batch.ShortageReasons, "newInventoryShortage\":true") {
		t.Fatalf("deep unseen inventory audit = %s", snapshot.Batch.ShortageReasons)
	}
}

func TestDiscoveryFeedRefreshSkipsBlockedHighScorePage(t *testing.T) {
	setupSQLiteDB(t)
	now := time.Now()
	for index := 0; index < 100; index++ {
		candidate := DiscoveryCandidate{
			SourceID: 1, SourceName: "Hugo",
			URL: fmt.Sprintf("https://blocked.example/high-%d", index), Title: fmt.Sprintf("Blocked %d", index),
			QualityScore: 1, DepthScore: 1, Status: discovery.DiscoveryCandidateStatusNew,
			ProcessingState: discovery.DiscoveryProcessingReady, EligibilityState: discovery.DiscoveryEligibilityEligible,
			DedupeState: discovery.DiscoveryDedupeReady, DedupeKey: fmt.Sprintf("blocked-%d", index),
			PublishedAt: &now, LastSeenAt: now,
		}
		if err := db.Create(&candidate).Error; err != nil {
			t.Fatal(err)
		}
	}
	unblockedIDs := make(map[uint]bool, 12)
	for index := 0; index < 12; index++ {
		candidate := DiscoveryCandidate{
			SourceID: 2, SourceName: fmt.Sprintf("ok-source-%d", index),
			URL: fmt.Sprintf("https://ok.example/post-%d", index), Title: fmt.Sprintf("OK %d", index),
			QualityScore: 0.5, DepthScore: 0.5, Status: discovery.DiscoveryCandidateStatusNew,
			ProcessingState: discovery.DiscoveryProcessingReady, EligibilityState: discovery.DiscoveryEligibilityEligible,
			DedupeState: discovery.DiscoveryDedupeReady, DedupeKey: fmt.Sprintf("ok-%d", index),
			PublishedAt: &now, LastSeenAt: now,
		}
		if err := db.Create(&candidate).Error; err != nil {
			t.Fatal(err)
		}
		unblockedIDs[candidate.ID] = true
	}
	if err := db.Create(&UserBlockRule{UserID: 6001, RuleType: UserBlockRuleSource, RuleValue: "Hugo", Active: true, CreatedAt: now, UpdatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}

	snapshot, err := RefreshDiscoveryFeed(context.Background(), 6001, 10)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Batch == nil || snapshot.Batch.ActualCount != 10 || len(snapshot.Items) != 10 {
		t.Fatalf("blocked high-score page should not empty the feed: %#v", snapshot)
	}
	for _, item := range snapshot.Items {
		if !unblockedIDs[item.CandidateID] {
			t.Fatalf("feed selected blocked or unexpected candidate %d (%s)", item.CandidateID, item.SnapshotSource)
		}
	}
	if strings.Contains(snapshot.Batch.ShortageReasons, "newInventoryShortage\":true") {
		t.Fatalf("unblocked inventory should fill the batch: %s", snapshot.Batch.ShortageReasons)
	}
}

func TestDiscoveryFeedRefreshAllowsMaterialContentUpdate(t *testing.T) {
	setupSQLiteDB(t)
	candidate := createDiscoveryFeedCandidates(t, 1)[0]
	first, err := RefreshDiscoveryFeed(context.Background(), 5001, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Items) != 1 {
		t.Fatalf("first feed items = %#v", first.Items)
	}
	if err := discovery.UpdateCandidates(db.Model(&candidate), map[string]interface{}{"content_version": 1}).Error; err != nil {
		t.Fatal(err)
	}

	updated, err := RefreshDiscoveryFeed(context.Background(), 5001, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(updated.Items) != 1 || updated.Items[0].CandidateID != candidate.ID || !updated.Items[0].ContentUpdated || updated.Items[0].CooldownRepeat {
		t.Fatalf("updated feed items = %#v", updated.Items)
	}
}

func TestDiscoveryFeedHidesRetainedCandidatesForActiveDomainBlacklist(t *testing.T) {
	setupSQLiteDB(t)
	createDiscoveryFeedCandidates(t, 4)

	initial, err := RefreshDiscoveryFeed(context.Background(), 2101, 10)
	if err != nil || len(initial.Items) != 4 {
		t.Fatalf("initial feed = %#v, %v", initial, err)
	}
	rule, err := discovery.CreateDiscoveryDomainBlacklist("feed.example", "fixture")
	if err != nil || rule.AffectedCandidates != 4 {
		t.Fatalf("blacklist mutation = %#v, %v", rule, err)
	}
	hidden, err := GetCurrentDiscoveryFeed(2101)
	if err != nil || hidden.Batch == nil || hidden.Batch.ActualCount != 0 || len(hidden.Items) != 0 {
		t.Fatalf("blacklisted current feed = %#v, %v", hidden, err)
	}
	if _, err := discovery.DeleteDiscoveryDomainBlacklist(rule.Entry.ID); err != nil {
		t.Fatal(err)
	}
	restored, err := GetCurrentDiscoveryFeed(2101)
	if err != nil || restored.Batch == nil || restored.Batch.ActualCount != 4 || len(restored.Items) != 4 {
		t.Fatalf("restored current feed = %#v, %v", restored, err)
	}
}

func TestDiscoveryFeedItemSupportsFeedbackAndContext(t *testing.T) {
	setupSQLiteDB(t)
	createDiscoveryFeedCandidates(t, 1)
	snapshot, err := RefreshDiscoveryFeed(context.Background(), 3001, 10)
	if err != nil {
		t.Fatal(err)
	}
	item := snapshot.Items[0]
	feedback, _, err := RecordRecommendationFeedback(3001, item.ID, RecommendationFeedbackValuable, nil)
	if err != nil {
		t.Fatal(err)
	}
	if feedback.CandidateID != item.CandidateID {
		t.Fatalf("feedback = %#v", feedback)
	}
	contextView, err := GetRecommendationItemContext(3001, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if contextView.Item.ID != item.ID || contextView.Feedback == nil {
		t.Fatalf("context = %#v", contextView)
	}
	if err := RevertRecommendationFeedback(3001, item.ID); err != nil {
		t.Fatal(err)
	}
	if current, _ := GetCurrentRecommendationFeedback(3001, item.ID); current != nil {
		t.Fatalf("feedback should be reverted: %#v", current)
	}
}

func TestDiscoveryFeedRefreshUsesRerankOrder(t *testing.T) {
	setupSQLiteDB(t)
	now := time.Now()
	lab := DiscoveryCandidate{
		SourceID: 1, SourceName: "Security Lab",
		URL: "https://securitylab.example/post-1", Title: "Lab high",
		QualityScore: 1, DepthScore: 1, Status: discovery.DiscoveryCandidateStatusNew,
		ProcessingState: discovery.DiscoveryProcessingReady, EligibilityState: discovery.DiscoveryEligibilityEligible,
		DedupeState: discovery.DiscoveryDedupeReady, DedupeKey: "lab-1",
		PublishedAt: &now, LastSeenAt: now,
	}
	other := DiscoveryCandidate{
		SourceID: 2, SourceName: "other-0",
		URL: "https://other-0.example/post", Title: "Other",
		QualityScore: 0.4, DepthScore: 0.4, Status: discovery.DiscoveryCandidateStatusNew,
		ProcessingState: discovery.DiscoveryProcessingReady, EligibilityState: discovery.DiscoveryEligibilityEligible,
		DedupeState: discovery.DiscoveryDedupeReady, DedupeKey: "other-0",
		PublishedAt: &now, LastSeenAt: now,
	}
	if err := db.Create(&lab).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&other).Error; err != nil {
		t.Fatal(err)
	}
	old := configuredReranker
	configuredReranker = func() RerankProvider {
		return fakeReranker{result: RerankResult{
			Model: "feed-order", PromptVersion: "v1",
			Items: []RerankItem{
				{CandidateID: other.ID, Rank: 1, Reason: "模型选择其他来源", Confidence: 0.9},
				{CandidateID: lab.ID, Rank: 2, Reason: "模型把高分放到第二", Confidence: 0.2},
			},
		}}
	}
	t.Cleanup(func() { configuredReranker = old })

	snapshot, err := RefreshDiscoveryFeed(context.Background(), 7001, 2)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Batch == nil || len(snapshot.Items) != 2 || snapshot.Items[0].CandidateID != other.ID {
		t.Fatalf("rerank order was rewritten: %#v", snapshot)
	}
}

func TestDiscoveryFeedRefreshFailsWithoutWritingBatch(t *testing.T) {
	setupSQLiteDB(t)
	createDiscoveryFeedCandidates(t, 3)
	old := configuredReranker
	configuredReranker = func() RerankProvider { return fakeReranker{err: errors.New("reranker down")} }
	t.Cleanup(func() { configuredReranker = old })

	if _, err := RefreshDiscoveryFeed(context.Background(), 7002, 10); err == nil {
		t.Fatal("rerank failure should not write a feed batch")
	}
	current, err := GetCurrentDiscoveryFeed(7002)
	if err != nil {
		t.Fatal(err)
	}
	if current.Batch != nil || len(current.Items) != 0 {
		t.Fatalf("failed refresh wrote a batch: %#v", current)
	}
}
