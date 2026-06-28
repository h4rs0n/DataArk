package common

import (
	"errors"
	"testing"
)

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
