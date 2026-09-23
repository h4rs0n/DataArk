package recommendation

import (
	"DataArk/discovery"
	"context"
	"testing"
)

func TestRecommendationUsesMaterialAcrossDistinctCandidateIDs(t *testing.T) {
	setupSQLiteDB(t)
	first := createReadyCandidate(t, "https://one.example/work", "Shared work", []string{"systems"}, "entry-one", .9, .8)
	alias := DiscoveryCandidate{MaterialID: first.MaterialID, URL: "https://two.example/work", Status: DiscoveryCandidateStatusNew, ProcessingState: discovery.DiscoveryProcessingReady, DedupeState: discovery.DiscoveryDedupeReady, DedupeKey: "entry-two"}
	if err := db.Create(&alias).Error; err != nil {
		t.Fatal(err)
	}
	inventory, err := GetCandidateInventory(701)
	if err != nil || inventory.EligibleCandidates != 1 {
		t.Fatalf("material inventory = %#v, %v", inventory, err)
	}
	settings := DefaultRecommendationSettings(701)
	settings.Enabled, settings.DailyLimit = true, 2
	if _, err := SaveRecommendationSettings(&settings); err != nil {
		t.Fatal(err)
	}
	day, err := GenerateDailyRecommendations(context.Background(), 701, "2026-09-22")
	if err != nil || len(day.Items) != 1 || day.Items[0].MaterialID != first.MaterialID {
		t.Fatalf("daily material dedupe = %#v, %v", day, err)
	}
	if _, err := discovery.MarkUserCandidateIgnored(701, alias.ID); err != nil {
		t.Fatal(err)
	}
	inventory, err = GetCandidateInventory(701)
	if err != nil || inventory.UserAvailableCandidates != 0 {
		t.Fatalf("alias feedback must filter material: %#v, %v", inventory, err)
	}
}
