package recommendation

import (
	"DataArk/discovery"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupSQLiteDB(t *testing.T) {
	t.Helper()
	sqliteDB, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open sqlite db: %v", err)
	}
	if err := sqliteDB.AutoMigrate(
		&discovery.DiscoverySource{},
		&discovery.DiscoveryCandidate{},
		&discovery.DiscoveryCandidateFeedback{},
		&discovery.DiscoveryDuplicateReviewSignal{},
		&discovery.DiscoveryArticleAssessment{},
		&discovery.UserCandidateState{},
		&discovery.DiscoverySite{},
		&discovery.DiscoverySiteEdge{},
		&discovery.DiscoveryCandidateProvenance{},
		&discovery.DiscoveryFetchRun{},
		&discovery.DiscoveryBackfillState{},
		&discovery.DiscoverySiteOperationalStats{},
		&discovery.DiscoverySourceScheduleDecision{},
		&RecommendationSettings{},
		&RecommendationDay{},
		&RecommendationFeedBatch{},
		&RecommendationItem{},
		&RecommendationFeedback{},
		&UserBlockRule{},
		&UserRecommendationProfile{},
	); err != nil {
		t.Fatalf("failed to migrate sqlite db: %v", err)
	}
	oldDiscovery := discovery.SetDB(sqliteDB)
	oldRecommendation := SetDB(sqliteDB)
	t.Cleanup(func() {
		discovery.SetDB(oldDiscovery)
		SetDB(oldRecommendation)
	})
}
