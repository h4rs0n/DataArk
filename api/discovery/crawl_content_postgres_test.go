package discovery

import (
	appdatabase "DataArk/database"
	"DataArk/material"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestPostgresSharedContentVersionConflict(t *testing.T) {
	dsn := os.Getenv("DATAARK_SHARED_CONTENT_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("set DATAARK_SHARED_CONTENT_POSTGRES_TEST_DSN to an empty disposable database")
	}
	database, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := database.DB()
	defer sqlDB.Close()
	sqlDB.SetMaxOpenConns(10)
	var name string
	if err := database.Raw("SELECT current_database()").Scan(&name).Error; err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(name, "dataark_v3_verify") {
		t.Fatal("refusing non-verification database")
	}
	if err := appdatabase.RunDatabaseMigrations(database); err != nil {
		t.Fatal(err)
	}
	old := SetDB(database)
	t.Cleanup(func() { SetDB(old) })
	first := newIndexedCandidate(t, "shared-version", strings.Repeat("Accepted original evidence. ", 40))
	const count = 8
	candidates := make([]DiscoveryCandidate, count)
	for i := range candidates {
		candidates[i] = DiscoveryCandidate{MaterialID: first.MaterialID, URL: fmt.Sprintf("https://alias-%d.invalid/article", i), ProcessingState: "ready", DedupeState: "ready"}
		if err := database.Create(&candidates[i]).Error; err != nil {
			t.Fatal(err)
		}
	}
	start := make(chan struct{})
	results := make(chan error, count)
	for i, candidate := range candidates {
		go func(i int, candidate DiscoveryCandidate) {
			<-start
			now := time.Now()
			results <- commitExtractedArticle(candidate.ID, 1, candidate.URL, now, &ExtractedArticle{Title: "Updated article", Text: fmt.Sprintf("Changed evidence from competing fetch %d", i), Language: "en"}, now)
		}(i, candidate)
	}
	close(start)
	for range count {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	var state material.ArticleState
	if err := database.First(&state, "material_id=?", first.MaterialID).Error; err != nil {
		t.Fatal(err)
	}
	var versions int64
	if err := database.Model(&material.Version{}).Where("material_id=?", first.MaterialID).Count(&versions).Error; err != nil {
		t.Fatal(err)
	}
	if state.ContentVersion != 2 || versions != 2 {
		t.Fatalf("stale alias overwrote new version: state=%d versions=%d", state.ContentVersion, versions)
	}
}
