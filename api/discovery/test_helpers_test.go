package discovery

import (
	"DataArk/archive"
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
		&archive.ArchiveTask{},
		&archive.ArchiveStat{},
		&archive.ArchiveDocument{},
		&archive.SearchEvent{},
		&archive.ArchiveClickEvent{},
		&DiscoverySource{},
		&DiscoveryCandidate{},
		&DiscoveryCandidateFeedback{},
		&DiscoverySite{},
		&DiscoverySiteEdge{},
		&DiscoveryCandidateProvenance{},
		&DiscoveryFetchRun{},
		&DiscoveryBackfillState{},
		&DiscoveryArticleContentVersion{},
		&DiscoveryDuplicateCluster{},
		&DiscoveryCandidateIdentity{},
		&DiscoveryDuplicateReviewSignal{},
		&DiscoveryArticleAssessment{},
	); err != nil {
		t.Fatalf("failed to migrate sqlite db: %v", err)
	}
	oldArchive := archive.SetDB(sqliteDB)
	oldDiscovery := SetDB(sqliteDB)
	t.Cleanup(func() {
		archive.SetDB(oldArchive)
		SetDB(oldDiscovery)
	})
}
