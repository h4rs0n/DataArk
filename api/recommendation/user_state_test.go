package recommendation

import (
	"DataArk/discovery"
	"context"
	"testing"
)

func TestRecommendationFeedbackAndSourceBlockArePerUser(t *testing.T) {
	setupSQLiteDB(t)
	for _, userID := range []uint{101, 202} {
		settings := DefaultRecommendationSettings(userID)
		settings.DailyLimit = 1
		settings.Enabled = true
		if _, err := SaveRecommendationSettings(&settings); err != nil {
			t.Fatal(err)
		}
	}
	candidate := createReadyCandidate(t, "https://shared.example/post", "Shared Post", []string{"Go"}, "shared-post", 0.9, 0.8)

	userADay, err := GenerateDailyRecommendations(context.Background(), 101, "2026-07-14")
	if err != nil {
		t.Fatal(err)
	}
	if len(userADay.Items) != 1 || userADay.Items[0].CandidateID != candidate.ID {
		t.Fatalf("user A day = %#v", userADay)
	}
	if _, _, err := RecordRecommendationFeedback(101, userADay.Items[0].ID, RecommendationFeedbackNotInterested, nil); err != nil {
		t.Fatal(err)
	}

	userBDay, err := GenerateDailyRecommendations(context.Background(), 202, "2026-07-14")
	if err != nil {
		t.Fatal(err)
	}
	if len(userBDay.Items) != 1 || userBDay.Items[0].CandidateID != candidate.ID {
		t.Fatalf("user B inherited user A feedback: %#v", userBDay)
	}
	userAState, err := discovery.GetUserCandidateState(101, candidate.ID)
	if err != nil {
		t.Fatal(err)
	}
	userBState, err := discovery.GetUserCandidateState(202, candidate.ID)
	if err != nil {
		t.Fatal(err)
	}
	if userAState.CurrentFeedback != RecommendationFeedbackNotInterested || userAState.ExposureCount != 1 {
		t.Fatalf("user A state = %#v", userAState)
	}
	if userBState.CurrentFeedback != "" || userBState.ExposureCount != 1 {
		t.Fatalf("user B state = %#v", userBState)
	}

	if err := db.Create(&UserBlockRule{UserID: 101, RuleType: UserBlockRuleSource, RuleValue: "shared.example", Active: true}).Error; err != nil {
		t.Fatal(err)
	}
	second := createReadyCandidate(t, "https://shared.example/second", "Second Shared Post", []string{"Go"}, "shared-second", 0.95, 0.9)
	blockedForA, err := selectDailyRecommendationCandidates(context.Background(), 101, DefaultRecommendationSettings(101), &UserRecommendationProfile{UserID: 101}, 10)
	if err != nil {
		t.Fatal(err)
	}
	availableForB, err := selectDailyRecommendationCandidates(context.Background(), 202, DefaultRecommendationSettings(202), &UserRecommendationProfile{UserID: 202}, 10)
	if err != nil {
		t.Fatal(err)
	}
	for _, scored := range blockedForA {
		if scored.Candidate.ID == second.ID {
			t.Fatalf("user A source block did not exclude candidate %#v", scored.Candidate)
		}
	}
	foundForB := false
	for _, scored := range availableForB {
		foundForB = foundForB || scored.Candidate.ID == second.ID
	}
	if !foundForB {
		t.Fatalf("user B inherited user A source block: %#v", availableForB)
	}
}
