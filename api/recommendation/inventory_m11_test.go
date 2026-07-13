package recommendation

import (
	"DataArk/discovery"
	"fmt"
	"testing"
	"time"
)

func TestCandidateInventorySeparatesPoolsAndAppliesOnlyUserHardFilters(t *testing.T) {
	setupSQLiteDB(t)
	settings := DefaultRecommendationSettings(301)
	settings.DailyLimit = 2
	if _, err := SaveRecommendationSettings(&settings); err != nil {
		t.Fatal(err)
	}
	otherSettings := DefaultRecommendationSettings(302)
	otherSettings.DailyLimit = 2
	if _, err := SaveRecommendationSettings(&otherSettings); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	seed := discovery.DiscoverySite{RootURL: "https://seed.example/", HostKey: "seed.example", Status: discovery.DiscoverySiteStatusSeed, DiscoveryMethod: "manual", CrawlAllowed: true, FirstDiscoveredAt: now}
	tail := discovery.DiscoverySite{RootURL: "https://tail.example/", HostKey: "tail.example", Status: discovery.DiscoverySiteStatusObserving, DiscoveryMethod: "blogroll", GraphDepth: 2, CrawlAllowed: true, FirstDiscoveredAt: now}
	if err := db.Create(&seed).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&tail).Error; err != nil {
		t.Fatal(err)
	}

	candidates := []DiscoveryCandidate{
		createReadyCandidate(t, "https://seed.example/fresh", "Fresh", []string{"Go"}, "fresh", 0.8, 0.7),
		createReadyCandidate(t, "https://seed.example/evergreen", "Evergreen", []string{"Databases"}, "evergreen", 0.9, 0.8),
		createReadyCandidate(t, "https://tail.example/explore", "Explore", []string{"Systems"}, "explore", 0.85, 0.8),
		createReadyCandidate(t, "https://blocked.example/post", "Blocked", []string{"Security"}, "blocked", 0.9, 0.8),
	}
	old := now.AddDate(0, 0, -90)
	if err := db.Model(&candidates[1]).Update("published_at", &old).Error; err != nil {
		t.Fatal(err)
	}
	for index := range candidates {
		assessment := discovery.DiscoveryArticleAssessment{CandidateID: candidates[index].ID, ContentVersion: 1, Assessor: "rules", AssessorVersion: "1", PolicyVersion: "1", OverallQuality: 0.8, EvergreenValue: 0.3, CreatedAt: now}
		if index == 1 || index == 2 {
			assessment.EvergreenValue = 0.8
		}
		if err := db.Create(&assessment).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Model(&candidates[index]).Update("current_assessment_id", assessment.ID).Error; err != nil {
			t.Fatal(err)
		}
		siteID := seed.ID
		if index == 2 {
			siteID = tail.ID
		}
		provenance := discovery.DiscoveryCandidateProvenance{ProvenanceKey: fmt.Sprintf("inventory-%d", index), CandidateID: candidates[index].ID, SiteID: siteID, DiscoveryMethod: "fixture", OriginalURL: candidates[index].URL, FirstSeenAt: now, LastSeenAt: now}
		if err := db.Create(&provenance).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Create(&UserBlockRule{UserID: 301, RuleType: UserBlockRuleSource, RuleValue: "blocked.example", Active: true}).Error; err != nil {
		t.Fatal(err)
	}
	// Even an intentionally pessimistic source-yield snapshot is operational
	// metadata only and cannot remove an independently eligible article.
	if err := db.Create(&discovery.DiscoverySiteOperationalStats{SiteID: seed.ID, CandidateCount: 1000, EligibleCandidateCount: 0, DuplicateCandidateCount: 999, LastComputedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&discovery.UserCandidateState{UserID: 301, CandidateID: candidates[0].ID, CurrentFeedback: RecommendationFeedbackNotInterested}).Error; err != nil {
		t.Fatal(err)
	}

	inventory, err := GetCandidateInventory(301)
	if err != nil {
		t.Fatal(err)
	}
	if inventory.EligibleCandidates != 4 || inventory.FreshEligibleCandidates != 3 || inventory.EvergreenEligibleCandidates != 2 || inventory.ExplorationEligibleCandidates != 1 {
		t.Fatalf("global inventory = %#v", inventory)
	}
	if inventory.UserAvailableCandidates != 2 || inventory.UserFreshCandidates != 1 || inventory.UserEvergreenCandidates != 2 || inventory.UserExplorationCandidates != 2 {
		t.Fatalf("user inventory = %#v", inventory)
	}
	if inventory.InventoryDays != 1 || inventory.Status != CandidateInventoryCritical {
		t.Fatalf("inventory health = %#v", inventory)
	}
	other, err := GetCandidateInventory(302)
	if err != nil {
		t.Fatal(err)
	}
	if other.UserAvailableCandidates != 4 {
		t.Fatalf("other user inherited hard filters: %#v", other)
	}
}
