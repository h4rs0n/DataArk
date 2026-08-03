package recommendation

import (
	"DataArk/discovery"
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestRecommendationV3ScoreInputCannotContainSourcePerformance(t *testing.T) {
	typeOfInput := reflect.TypeOf(recommendationScoreInput{})
	for index := 0; index < typeOfInput.NumField(); index++ {
		name := strings.ToLower(typeOfInput.Field(index).Name)
		for _, forbidden := range []string{"source", "site", "yield", "reputation", "average", "hit", "graph", "operational"} {
			if strings.Contains(name, forbidden) {
				t.Fatalf("recommendation score input leaked source performance field %q", typeOfInput.Field(index).Name)
			}
		}
	}
	now := time.Date(2026, 7, 14, 6, 0, 0, 0, time.UTC)
	ordinary := scoreRecommendationCandidateV3(recommendationScoreInput{Quality: 0.45, Depth: 0.4, Evergreen: 0.3, PublishedAt: &now, Now: now})
	longTailGem := scoreRecommendationCandidateV3(recommendationScoreInput{Quality: 0.92, Depth: 0.85, Evergreen: 0.8, PublishedAt: &now, Now: now})
	if longTailGem <= ordinary {
		t.Fatalf("long-tail article score %v did not beat ordinary article %v", longTailGem, ordinary)
	}
}

type recommendationTestClock struct{ now time.Time }

func (clock *recommendationTestClock) Now() time.Time              { return clock.now }
func (clock *recommendationTestClock) Advance(delta time.Duration) { clock.now = clock.now.Add(delta) }

func useRecommendationTestClock(t *testing.T, now time.Time) *recommendationTestClock {
	t.Helper()
	clock := &recommendationTestClock{now: now}
	old := recommendationClock
	recommendationClock = clock
	t.Cleanup(func() { recommendationClock = old })
	return clock
}

func TestRecommendationV3SelectsTargetWithExplorationAndSoftDiversity(t *testing.T) {
	setupSQLiteDB(t)
	now := time.Date(2026, 7, 14, 6, 0, 0, 0, time.UTC)
	useRecommendationTestClock(t, now)
	settings := DefaultRecommendationSettings(401)
	settings.DailyLimit = 10
	settings.ExplorationRate = 0.15
	settings.Enabled = true
	if _, err := SaveRecommendationSettings(&settings); err != nil {
		t.Fatal(err)
	}
	topics := []string{"Go", "Databases", "Systems", "Security", "Web"}
	for index := 0; index < 16; index++ {
		host := fmt.Sprintf("source-%d.example", index%4)
		candidate := createReadyCandidate(t, fmt.Sprintf("https://%s/post-%d", host, index), fmt.Sprintf("Post %d", index), []string{topics[index%len(topics)]}, fmt.Sprintf("key-%d", index), 0.95-float64(index)*0.01, 0.7)
		if err := db.Model(&candidate).Updates(map[string]interface{}{"author": fmt.Sprintf("Author %d", index%8), "content_version": 1, "published_at": &now}).Error; err != nil {
			t.Fatal(err)
		}
	}

	snapshot, err := GenerateDailyRecommendations(context.Background(), 401, "2026-07-14")
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Day.RequestedCount != 10 || snapshot.Day.ActualCount != 10 || len(snapshot.Items) != 10 {
		t.Fatalf("target/actual snapshot = %#v", snapshot)
	}
	sourceCounts := make(map[string]int)
	topicCounts := make(map[string]int)
	explorationCount := 0
	identities := make(map[string]bool)
	for _, item := range snapshot.Items {
		sourceCounts[item.Candidate.SourceName]++
		for _, topic := range parseStringList(item.Candidate.Topics) {
			topicCounts[topic]++
		}
		if item.PoolType == "exploration" && item.ExplorationReason != "" {
			explorationCount++
		}
		identity := recommendationCandidateIdentity(item.Candidate)
		if identities[identity] {
			t.Fatalf("duplicate identity selected: %s", identity)
		}
		identities[identity] = true
	}
	if explorationCount < 2 {
		t.Fatalf("exploration count = %d, want at least ceil(10*0.15)=2", explorationCount)
	}
	for source, count := range sourceCounts {
		if count > 3 {
			t.Fatalf("source %s count = %d, want <= 3", source, count)
		}
	}
	for topic, count := range topicCounts {
		if count > 4 {
			t.Fatalf("topic %s count = %d, want <= 4", topic, count)
		}
	}
	var audit struct {
		SoftRelaxations []string `json:"softRelaxations"`
	}
	if err := json.Unmarshal([]byte(snapshot.Day.ShortageReasons), &audit); err != nil {
		t.Fatal(err)
	}
	if len(audit.SoftRelaxations) != 0 {
		t.Fatalf("unexpected soft relaxations: %#v", audit.SoftRelaxations)
	}
}

func TestRecommendationV3PublishesOnlyEligibleMAndExplainsShortage(t *testing.T) {
	setupSQLiteDB(t)
	now := time.Date(2026, 7, 14, 7, 0, 0, 0, time.UTC)
	useRecommendationTestClock(t, now)
	settings := DefaultRecommendationSettings(402)
	settings.DailyLimit = 10
	settings.Enabled = true
	if _, err := SaveRecommendationSettings(&settings); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 7; index++ {
		createReadyCandidate(t, fmt.Sprintf("https://eligible-%d.example/post", index), fmt.Sprintf("Eligible %d", index), []string{fmt.Sprintf("Topic %d", index)}, fmt.Sprintf("eligible-%d", index), 0.8, 0.7)
	}
	ineligible := createReadyCandidate(t, "https://invalid.example/post", "Invalid", []string{"Invalid"}, "invalid", 0.99, 0.9)
	if err := db.Model(&ineligible).Updates(map[string]interface{}{"eligibility_state": discovery.DiscoveryEligibilityIneligible, "eligibility_reasons": "article_quality_below_threshold"}).Error; err != nil {
		t.Fatal(err)
	}

	snapshot, err := GenerateDailyRecommendations(context.Background(), 402, "2026-07-14")
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Day.ActualCount != 7 || len(snapshot.Items) != 7 {
		t.Fatalf("shortage snapshot = %#v", snapshot)
	}
	var audit struct {
		Target                   int            `json:"target"`
		Actual                   int            `json:"actual"`
		EligibleAfterHardFilters int            `json:"eligibleAfterHardFilters"`
		Excluded                 map[string]int `json:"excluded"`
	}
	if err := json.Unmarshal([]byte(snapshot.Day.ShortageReasons), &audit); err != nil {
		t.Fatal(err)
	}
	if audit.Target != 10 || audit.Actual != 7 || audit.EligibleAfterHardFilters != 7 || audit.Excluded["article_ineligible"] < 1 {
		t.Fatalf("shortage audit = %#v", audit)
	}
	for _, item := range snapshot.Items {
		if item.CandidateID == ineligible.ID {
			t.Fatal("hard article eligibility was relaxed to fill target")
		}
	}
}

func TestRecommendationV3ExcludesRetainedCandidatesFromBlacklistedDomains(t *testing.T) {
	setupSQLiteDB(t)
	now := time.Date(2026, 8, 3, 7, 0, 0, 0, time.UTC)
	useRecommendationTestClock(t, now)
	settings := DefaultRecommendationSettings(409)
	settings.DailyLimit = 2
	settings.Enabled = true
	if _, err := SaveRecommendationSettings(&settings); err != nil {
		t.Fatal(err)
	}
	blocked := createReadyCandidate(t, "https://news.blocked.example/high", "Blocked", []string{"Security"}, "blocked-key", 0.99, 0.95)
	createReadyCandidate(t, "https://allowed-one.example/post", "Allowed one", []string{"Go"}, "allowed-key-1", 0.8, 0.7)
	createReadyCandidate(t, "https://allowed-two.example/post", "Allowed two", []string{"Systems"}, "allowed-key-2", 0.79, 0.7)
	if _, err := discovery.CreateDiscoveryDomainBlacklist("blocked.example", "fixture"); err != nil {
		t.Fatal(err)
	}
	if err := db.First(&blocked, blocked.ID).Error; err != nil || blocked.ProcessingState != discovery.DiscoveryProcessingReady {
		t.Fatalf("retained candidate evidence changed: %#v, %v", blocked, err)
	}

	snapshot, err := GenerateDailyRecommendations(context.Background(), 409, "2026-08-03")
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Day.ActualCount != 2 || len(snapshot.Items) != 2 {
		t.Fatalf("snapshot = %#v", snapshot)
	}
	for _, item := range snapshot.Items {
		if item.CandidateID == blocked.ID {
			t.Fatalf("blacklisted retained candidate was published: %#v", item)
		}
	}
	var audit struct {
		Excluded map[string]int `json:"excluded"`
	}
	if err := json.Unmarshal([]byte(snapshot.Day.ShortageReasons), &audit); err != nil {
		t.Fatal(err)
	}
	if audit.Excluded["domain_blacklist"] != 1 {
		t.Fatalf("blacklist audit = %#v", audit.Excluded)
	}
}

func TestRecommendationV3CooldownUpdateAndExplicitFeedbackRecurrence(t *testing.T) {
	setupSQLiteDB(t)
	clock := useRecommendationTestClock(t, time.Date(2026, 1, 1, 7, 0, 0, 0, time.UTC))
	settings := DefaultRecommendationSettings(403)
	settings.DailyLimit = 2
	settings.Enabled = true
	if _, err := SaveRecommendationSettings(&settings); err != nil {
		t.Fatal(err)
	}
	updatedCandidate := createReadyCandidate(t, "https://updates.example/post", "Updates", []string{"Go"}, "updates", 0.9, 0.8)
	cooldownCandidate := createReadyCandidate(t, "https://cooldown.example/post", "Cooldown", []string{"Rust"}, "cooldown", 0.85, 0.8)
	if err := db.Model(&DiscoveryCandidate{}).Where("id IN ?", []uint{updatedCandidate.ID, cooldownCandidate.ID}).Update("content_version", 1).Error; err != nil {
		t.Fatal(err)
	}
	first, err := GenerateDailyRecommendations(context.Background(), 403, "2026-01-01")
	if err != nil || len(first.Items) != 2 {
		t.Fatalf("first day = %#v err=%v", first, err)
	}

	clock.Advance(24 * time.Hour)
	if err := db.Model(&updatedCandidate).Update("content_version", 2).Error; err != nil {
		t.Fatal(err)
	}
	second, err := GenerateDailyRecommendations(context.Background(), 403, "2026-01-02")
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Items) != 1 || second.Items[0].CandidateID != updatedCandidate.ID || !second.Items[0].ContentUpdated || second.Items[0].CooldownRepeat {
		t.Fatalf("updated recurrence = %#v", second.Items)
	}
	if _, _, err := RecordRecommendationFeedback(403, second.Items[0].ID, RecommendationFeedbackValuable, nil); err != nil {
		t.Fatal(err)
	}

	clock.Advance(76 * 24 * time.Hour)
	third, err := GenerateDailyRecommendations(context.Background(), 403, "2026-03-19")
	if err != nil {
		t.Fatal(err)
	}
	if len(third.Items) != 1 || third.Items[0].CandidateID != cooldownCandidate.ID || !third.Items[0].CooldownRepeat || third.Items[0].ContentUpdated {
		t.Fatalf("cooldown recurrence = %#v", third.Items)
	}
}

func TestRecommendationV3SoftRelaxationIsAuditedButHardIdentityIsNot(t *testing.T) {
	setupSQLiteDB(t)
	now := time.Date(2026, 7, 14, 8, 0, 0, 0, time.UTC)
	useRecommendationTestClock(t, now)
	settings := DefaultRecommendationSettings(404)
	settings.DailyLimit = 5
	settings.Enabled = true
	if _, err := SaveRecommendationSettings(&settings); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 6; index++ {
		candidate := createReadyCandidate(t, fmt.Sprintf("https://one-source.example/post-%d", index), fmt.Sprintf("Same Source %d", index), []string{"Same Topic"}, fmt.Sprintf("same-%d", index), 0.9-float64(index)*0.01, 0.8)
		if err := db.Model(&candidate).Update("author", "Same Author").Error; err != nil {
			t.Fatal(err)
		}
	}
	snapshot, err := GenerateDailyRecommendations(context.Background(), 404, "2026-07-14")
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Items) != 5 {
		t.Fatalf("soft constraints should relax to fill: %#v", snapshot.Items)
	}
	if !strings.Contains(snapshot.Day.ShortageReasons, "author_limit") || !strings.Contains(snapshot.Day.ShortageReasons, "topic_limit") || !strings.Contains(snapshot.Day.ShortageReasons, "source_limit") {
		t.Fatalf("soft relaxation audit = %s", snapshot.Day.ShortageReasons)
	}

	duplicateA := createReadyCandidate(t, "https://dup-a.example/post", "Duplicate A", []string{"A"}, "duplicate-a", 0.99, 0.9)
	duplicateB := createReadyCandidate(t, "https://dup-b.example/post", "Duplicate B", []string{"B"}, "duplicate-b", 0.98, 0.9)
	if err := db.Model(&DiscoveryCandidate{}).Where("id IN ?", []uint{duplicateA.ID, duplicateB.ID}).Update("duplicate_cluster_id", "hard-cluster").Error; err != nil {
		t.Fatal(err)
	}
	selected, err := selectDailyRecommendationCandidates(context.Background(), 405, DefaultRecommendationSettings(405), &UserRecommendationProfile{UserID: 405}, 10)
	if err != nil {
		t.Fatal(err)
	}
	clusterCount := 0
	for _, item := range selected {
		if item.Candidate.DuplicateClusterID == "hard-cluster" {
			clusterCount++
		}
	}
	if clusterCount != 1 {
		t.Fatalf("hard duplicate cluster count = %d, want 1", clusterCount)
	}
}
