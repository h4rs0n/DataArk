package assessment

import (
	"DataArk/discovery"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupAssessmentDB(t *testing.T) {
	t.Helper()
	sqliteDB, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := sqliteDB.AutoMigrate(
		&discovery.DiscoverySource{},
		&discovery.DiscoveryCandidate{},
		&ArticleAssessment{},
		&discovery.DiscoveryDomainBlacklistEntry{},
		&LLMCall{},
	); err != nil {
		t.Fatalf("migrate sqlite: %v", err)
	}
	if err := discovery.MigrateMaterialTestSchema(sqliteDB); err != nil {
		t.Fatal(err)
	}
	oldDiscovery := discovery.SetDB(sqliteDB)
	oldAssessment := SetDB(sqliteDB)
	t.Cleanup(func() {
		discovery.SetDB(oldDiscovery)
		SetDB(oldAssessment)
	})
}
