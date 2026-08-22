package recommendation

import (
	"DataArk/discovery"
	"math"
	"testing"
	"time"
)

func createFeedbackItem(t *testing.T, userID uint, url string, topics []string, style string) RecommendationItem {
	t.Helper()
	candidate := createReadyCandidate(t, url, "Feedback article", topics, "feedback-"+url, 0.8, 0.7)
	candidate.ContentStyle = style
	if err := db.Save(&candidate).Error; err != nil {
		t.Fatal(err)
	}
	day, err := CreateRecommendationDay(userID, "2026-01-01", 10)
	if err != nil {
		t.Fatal(err)
	}
	item, err := AddRecommendationItem(&RecommendationItem{DayID: uintPointer(day.ID), UserID: userID, CandidateID: candidate.ID, Rank: 1})
	if err != nil {
		t.Fatal(err)
	}
	return *item
}

func TestFeedbackM13IsIdempotentReplaceableAndRevertible(t *testing.T) {
	setupSQLiteDB(t)
	clock := useRecommendationTestClock(t, time.Date(2026, 1, 1, 8, 0, 0, 0, time.UTC))
	item := createFeedbackItem(t, 701, "https://b.example/one", []string{"databases"}, "tutorial")

	first, _, err := RecordRecommendationFeedback(701, item.ID, RecommendationFeedbackValuable, nil)
	if err != nil {
		t.Fatal(err)
	}
	profileOnce, err := RebuildUserRecommendationProfile(701)
	if err != nil {
		t.Fatal(err)
	}
	clock.Advance(6 * time.Hour)
	second, _, err := RecordRecommendationFeedback(701, item.ID, RecommendationFeedbackValuable, nil)
	if err != nil {
		t.Fatal(err)
	}
	profileTwice, err := RebuildUserRecommendationProfile(701)
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID || profileOnce.TopicWeights != profileTwice.TopicWeights || profileOnce.ProfileVersion != profileTwice.ProfileVersion {
		t.Fatalf("repeat changed state: first=%#v second=%#v profiles=%#v/%#v", first, second, profileOnce, profileTwice)
	}

	replacement, _, err := RecordRecommendationFeedback(701, item.ID, RecommendationFeedbackNotInterested, nil)
	if err != nil {
		t.Fatal(err)
	}
	current, err := GetCurrentRecommendationFeedback(701, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	history, err := ListRecommendationFeedbackHistory(701, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current == nil || current.ID != replacement.ID || replacement.SupersedesID == nil || *replacement.SupersedesID != first.ID || len(history) != 2 || history[0].ClosedReason != "superseded" {
		t.Fatalf("replacement current=%#v history=%#v", current, history)
	}
	if err := RevertRecommendationFeedback(701, item.ID); err != nil {
		t.Fatal(err)
	}
	current, err = GetCurrentRecommendationFeedback(701, item.ID)
	if err != nil || current != nil {
		t.Fatalf("current after revert = %#v, %v", current, err)
	}
	history, _ = ListRecommendationFeedbackHistory(701, item.ID)
	if len(history) != 2 || history[1].ClosedReason != "reverted" {
		t.Fatalf("history after revert = %#v", history)
	}
}

func TestFeedbackM13DoesNotCreateSourcePreferenceAndDecays(t *testing.T) {
	setupSQLiteDB(t)
	clock := useRecommendationTestClock(t, time.Date(2026, 1, 1, 8, 0, 0, 0, time.UTC))
	item := createFeedbackItem(t, 702, "https://b.example/disliked", []string{"databases"}, "tutorial")
	if _, _, err := RecordRecommendationFeedback(702, item.ID, RecommendationFeedbackNotInterested, nil); err != nil {
		t.Fatal(err)
	}
	initial, err := RebuildUserRecommendationProfile(702)
	if err != nil {
		t.Fatal(err)
	}
	if len(parseWeightMap(initial.SourceWeights)) != 0 {
		t.Fatalf("article feedback leaked into source weights: %s", initial.SourceWeights)
	}
	initialWeight := parseWeightMap(initial.TopicWeights)["databases"]
	clock.Advance(90 * 24 * time.Hour)
	decayed, err := RebuildUserRecommendationProfile(702)
	if err != nil {
		t.Fatal(err)
	}
	if got := parseWeightMap(decayed.TopicWeights)["databases"]; math.Abs(got-initialWeight/2) > 0.000001 {
		t.Fatalf("decayed weight = %f, want %f", got, initialWeight/2)
	}
}

func TestFeedbackM13LowValueIsArticleOnly(t *testing.T) {
	setupSQLiteDB(t)
	now := time.Date(2026, 1, 1, 8, 0, 0, 0, time.UTC)
	useRecommendationTestClock(t, now)
	item := createFeedbackItem(t, 710, "https://quality.example/low", []string{"databases"}, "tutorial")

	var before discovery.DiscoveryCandidate
	if err := db.First(&before, item.CandidateID).Error; err != nil {
		t.Fatal(err)
	}
	feedback, _, err := RecordRecommendationFeedback(710, item.ID, RecommendationFeedbackLowValue, nil)
	if err != nil {
		t.Fatal(err)
	}
	if feedback.Action != RecommendationFeedbackLowValue {
		t.Fatalf("feedback action = %q", feedback.Action)
	}

	var state discovery.UserCandidateState
	if err := db.Where("user_id = ? AND candidate_id = ?", 710, item.CandidateID).First(&state).Error; err != nil {
		t.Fatal(err)
	}
	if state.CurrentFeedback != RecommendationFeedbackLowValue || state.OpenedAt != nil || state.ReadAt != nil || state.DeepReadAt != nil {
		t.Fatalf("low-value state = %#v", state)
	}
	if !userCandidateStateExcludesRecommendation(state) {
		t.Fatal("low-value article should be excluded from future recommendations for this user")
	}

	profile, err := RebuildUserRecommendationProfile(710)
	if err != nil {
		t.Fatal(err)
	}
	if len(parseWeightMap(profile.TopicWeights)) != 0 || len(parseWeightMap(profile.StyleWeights)) != 0 || len(parseWeightMap(profile.SourceWeights)) != 0 || profile.DepthPreference != 0.5 {
		t.Fatalf("low-value feedback changed personal preferences: %#v", profile)
	}

	var after discovery.DiscoveryCandidate
	if err := db.First(&after, item.CandidateID).Error; err != nil {
		t.Fatal(err)
	}
	if after.QualityScore != before.QualityScore || after.EligibilityState != before.EligibilityState {
		t.Fatalf("low-value feedback changed shared candidate: before=%#v after=%#v", before, after)
	}

	metrics, err := GetAdminProductMetrics(now)
	if err != nil {
		t.Fatal(err)
	}
	if metrics.Feedback.ByAction[RecommendationFeedbackLowValue] != 1 || metrics.Feedback.PositiveArticles != 0 {
		t.Fatalf("low-value metrics = %#v", metrics.Feedback)
	}
}

func TestFeedbackM13DuplicateIsClusterSignalAndBlockIsExplicitPerUser(t *testing.T) {
	setupSQLiteDB(t)
	useRecommendationTestClock(t, time.Date(2026, 1, 1, 8, 0, 0, 0, time.UTC))
	duplicateItem := createFeedbackItem(t, 703, "https://b.example/repeated", []string{"go"}, "news")
	if _, _, err := RecordRecommendationFeedback(703, duplicateItem.ID, RecommendationFeedbackTooRepetitive, nil); err != nil {
		t.Fatal(err)
	}
	profile, err := RebuildUserRecommendationProfile(703)
	if err != nil {
		t.Fatal(err)
	}
	if len(parseWeightMap(profile.TopicWeights)) != 0 || len(parseWeightMap(profile.StyleWeights)) != 0 || len(parseWeightMap(profile.SourceWeights)) != 0 {
		t.Fatalf("duplicate changed preferences: %#v", profile)
	}
	var signals int64
	if err := db.Model(&discovery.DiscoveryDuplicateReviewSignal{}).Where("reporter_user_id = ?", 703).Count(&signals).Error; err != nil || signals != 1 {
		t.Fatalf("duplicate signals = %d, err=%v", signals, err)
	}

	blockItem := createFeedbackItem(t, 704, "https://b.example/block", []string{"go"}, "news")
	if _, _, err := RecordRecommendationFeedback(704, blockItem.ID, RecommendationFeedbackBlockSource, []RecommendationBlockTarget{{Type: UserBlockRuleTopic, Value: "go"}}); err != ErrInvalidBlockRule {
		t.Fatalf("block_source accepted a non-source target: %v", err)
	}
	feedback, rules, err := RecordRecommendationFeedback(704, blockItem.ID, RecommendationFeedbackBlockSource, []RecommendationBlockTarget{{Type: UserBlockRuleSource, Value: "b.example"}})
	if err != nil {
		t.Fatal(err)
	}
	if feedback.Action != RecommendationFeedbackBlock || len(rules) != 1 || rules[0].FeedbackID == nil {
		t.Fatalf("explicit block = %#v rules=%#v", feedback, rules)
	}
	otherRules, err := ListUserBlockRules(705, true)
	if err != nil || len(otherRules) != 0 {
		t.Fatalf("other user rules = %#v, err=%v", otherRules, err)
	}
	if err := RevertRecommendationFeedback(704, blockItem.ID); err != nil {
		t.Fatal(err)
	}
	active, _ := ListUserBlockRules(704, true)
	if len(active) != 0 {
		t.Fatalf("block survived feedback revert: %#v", active)
	}
}

func TestFeedbackM13PreferenceResetKeepsEvents(t *testing.T) {
	setupSQLiteDB(t)
	clock := useRecommendationTestClock(t, time.Date(2026, 1, 1, 8, 0, 0, 0, time.UTC))
	item := createFeedbackItem(t, 706, "https://reset.example/one", []string{"systems"}, "essay")
	if _, _, err := RecordRecommendationFeedback(706, item.ID, RecommendationFeedbackValuable, nil); err != nil {
		t.Fatal(err)
	}
	before, err := RebuildUserRecommendationProfile(706)
	if err != nil {
		t.Fatal(err)
	}
	clock.Advance(time.Minute)
	reset, err := ResetUserRecommendationPreferences(706)
	if err != nil {
		t.Fatal(err)
	}
	after, err := RebuildUserRecommendationProfile(706)
	if err != nil {
		t.Fatal(err)
	}
	history, _ := ListRecommendationFeedbackHistory(706, item.ID)
	if reset.ProfileVersion != before.ProfileVersion+1 || after.ProfileVersion != reset.ProfileVersion || len(parseWeightMap(after.TopicWeights)) != 0 || len(history) != 1 {
		t.Fatalf("reset before=%#v reset=%#v after=%#v history=%#v", before, reset, after, history)
	}
}

func TestFeedbackM13ColdStartSettingsSeedOnlyThatUser(t *testing.T) {
	setupSQLiteDB(t)
	settings := DefaultRecommendationSettings(707)
	settings.PreferredTopics = `["Go","databases"]`
	settings.PreferredLanguages = `["zh","en"]`
	settings.PreferredLength = "long"
	settings.ExplorationRate = 0.3
	settings.FavoriteSources = `["b.example"]`
	saved, err := SaveRecommendationSettings(&settings)
	if err != nil {
		t.Fatal(err)
	}
	profile, err := RebuildUserRecommendationProfile(707)
	if err != nil {
		t.Fatal(err)
	}
	other, err := RebuildUserRecommendationProfile(708)
	if err != nil {
		t.Fatal(err)
	}
	if saved.PreferredLength != "long" || len(parseStringList(saved.PreferredLanguages)) != 2 || profile.ExplorationRate != 0.3 || profile.DepthPreference != 0.75 || parseWeightMap(profile.TopicWeights)["Go"] != 0.75 {
		t.Fatalf("saved=%#v profile=%#v", saved, profile)
	}
	if len(parseWeightMap(other.TopicWeights)) != 0 || other.DepthPreference != 0.5 {
		t.Fatalf("cold start leaked across users: %#v", other)
	}
}

func TestFeedbackM13EngagementStrengthAndExposureNeutrality(t *testing.T) {
	setupSQLiteDB(t)
	now := time.Date(2026, 1, 1, 8, 0, 0, 0, time.UTC)
	useRecommendationTestClock(t, now)
	opened := createReadyCandidate(t, "https://signals.example/opened", "Opened", []string{"opened"}, "signal-opened", 0.8, 0.7)
	deep := createReadyCandidate(t, "https://signals.example/deep", "Deep", []string{"deep"}, "signal-deep", 0.8, 0.7)
	exposed := createReadyCandidate(t, "https://signals.example/exposed", "Exposed", []string{"exposed"}, "signal-exposed", 0.8, 0.7)
	states := []discovery.UserCandidateState{
		{UserID: 709, CandidateID: opened.ID, OpenedAt: &now},
		{UserID: 709, CandidateID: deep.ID, DeepReadAt: &now},
		{UserID: 709, CandidateID: exposed.ID, FirstExposedAt: &now, LastExposedAt: &now, ExposureCount: 3},
	}
	if err := db.Create(&states).Error; err != nil {
		t.Fatal(err)
	}
	profile, err := RebuildUserRecommendationProfile(709)
	if err != nil {
		t.Fatal(err)
	}
	weights := parseWeightMap(profile.TopicWeights)
	if weights["opened"] <= 0 || weights["deep"] <= weights["opened"] || weights["exposed"] != 0 {
		t.Fatalf("engagement weights = %#v", weights)
	}
}
