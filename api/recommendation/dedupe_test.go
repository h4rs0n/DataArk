package recommendation

import (
	"DataArk/discovery"
	"context"
	"testing"
)

func TestRecommendationSelectionOnlyUsesDuplicateRepresentative(t *testing.T) {
	setupSQLiteDB(t)
	settings := DefaultRecommendationSettings(18)
	settings.DailyLimit = 2
	settings.Enabled = true
	if _, err := SaveRecommendationSettings(&settings); err != nil {
		t.Fatal(err)
	}
	representative := createReadyCandidate(t, "https://author.example/original", "Original", []string{"Systems"}, "before-cluster-a", 0.8, 0.8)
	member := createReadyCandidate(t, "https://mirror.example/copy", "Copy", []string{"Systems"}, "before-cluster-b", 0.9, 0.9)
	clusterID := "dup-fixture-cluster"
	if err := db.Model(&DiscoveryCandidate{}).Where("id = ?", representative.ID).Updates(map[string]interface{}{
		"duplicate_cluster_id": clusterID, "dedupe_key": clusterID, "representative_id": representative.ID,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&DiscoveryCandidate{}).Where("id = ?", member.ID).Updates(map[string]interface{}{
		"duplicate_cluster_id": clusterID, "dedupe_key": clusterID, "representative_id": representative.ID,
	}).Error; err != nil {
		t.Fatal(err)
	}

	snapshot, err := GenerateDailyRecommendations(context.Background(), 18, "2026-07-14")
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Items) != 1 || snapshot.Items[0].CandidateID != representative.ID {
		t.Fatalf("representative-only snapshot = %#v", snapshot.Items)
	}
}

func TestDuplicateFeedbackCreatesReviewWithoutSourcePenalty(t *testing.T) {
	setupSQLiteDB(t)
	candidate := createReadyCandidate(t, "https://author.example/post", "Possible duplicate", []string{"Systems"}, "dup-review", 0.8, 0.8)
	day, err := CreateRecommendationDay(19, "2026-07-14", 1)
	if err != nil {
		t.Fatal(err)
	}
	item, err := AddRecommendationItem(&RecommendationItem{DayID: uintPointer(day.ID), UserID: 19, CandidateID: candidate.ID, DedupeKey: candidate.DedupeKey, Rank: 1})
	if err != nil {
		t.Fatal(err)
	}
	feedback, _, err := RecordRecommendationFeedback(19, item.ID, RecommendationFeedbackDuplicate, nil)
	if err != nil {
		t.Fatal(err)
	}
	if feedback.Action != RecommendationFeedbackDuplicate {
		t.Fatalf("feedback = %#v", feedback)
	}
	var signals []discovery.DiscoveryDuplicateReviewSignal
	if err := db.Where("candidate_id = ? AND reporter_user_id = ?", candidate.ID, 19).Find(&signals).Error; err != nil {
		t.Fatal(err)
	}
	if len(signals) != 1 || signals[0].Status != "pending" || signals[0].RecommendationItemID != item.ID {
		t.Fatalf("duplicate signals = %#v", signals)
	}
	profile, err := RebuildUserRecommendationProfile(19)
	if err != nil {
		t.Fatal(err)
	}
	if weight := parseWeightMap(profile.SourceWeights)["author.example"]; weight != 0 {
		t.Fatalf("duplicate feedback source weight = %v, want 0", weight)
	}
}
