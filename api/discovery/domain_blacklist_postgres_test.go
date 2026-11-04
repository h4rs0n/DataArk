package discovery

import (
	appdatabase "DataArk/database"
	"DataArk/material"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/pressly/goose/v3"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestPostgresCrawlOnlyBlacklistRepair(t *testing.T) {
	dsn := os.Getenv("DATAARK_CRAWL_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("set DATAARK_CRAWL_POSTGRES_TEST_DSN to an empty disposable database")
	}
	database, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := database.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
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
	var count int64
	if err := database.Table("discovery_candidates").Count(&count).Error; err != nil || count != 0 {
		t.Fatal("requires empty disposable database", err)
	}
	if err := goose.DownTo(sqlDB, ".", 46); err != nil {
		t.Fatal(err)
	}
	old := SetDB(database)
	t.Cleanup(func() { SetDB(old) })
	now := time.Now().UTC()
	accepted := newIndexedCandidate(t, "repair-accepted", "Previously accepted body remains available.")
	unaffected := newIndexedCandidate(t, "repair-unaffected", "An unaffected accepted article.")
	placeholder := newIndexedCandidate(t, "repair-placeholder", "A body without successful extraction is not sufficient.")
	for _, c := range []DiscoveryCandidate{accepted, unaffected} {
		if err := database.Model(&DiscoveryCandidate{}).Where("id=?", c.ID).Updates(map[string]any{"extracted_at": now, "dedupe_state": "ready"}).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := database.Exec(`INSERT INTO discovery_article_assessments(id,material_id,candidate_id,content_version,assessor,assessor_version,policy_version,overall_quality)
 VALUES(900001,?,?,1,'fixture','1','fixture',.9)`, accepted.MaterialID, accepted.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Exec(`UPDATE material_article_states SET eligibility_state='unknown', eligibility_reasons='domain_blacklist',assessment_state='ready',current_assessment_id=900001 WHERE material_id=?`, accepted.MaterialID).Error; err != nil {
		t.Fatal(err)
	}
	for _, c := range []DiscoveryCandidate{accepted, placeholder} {
		if err := database.Model(&DiscoveryCandidate{}).Where("id=?", c.ID).Updates(map[string]any{"processing_state": "domain_blocked", "processing_error_type": "domain_blacklist", "processing_error": "fixture blocked"}).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := database.Create(&DiscoveryDomainBlacklistEntry{Domain: "repair-placeholder.example"}).Error; err != nil {
		t.Fatal(err)
	}
	pending := DiscoveryCandidate{URL: "https://pending-repair.example/post", ProcessingState: DiscoveryProcessingDomainBlocked, ProcessingErrorType: processingErrorDomainBlacklist, EligibilityReasons: "domain_blacklist", EligibilityState: DiscoveryEligibilityUnknown}
	if err := database.Create(&pending).Error; err != nil {
		t.Fatal(err)
	}
	var before material.ArticleState
	if err := database.First(&before, "material_id=?", unaffected.MaterialID).Error; err != nil {
		t.Fatal(err)
	}
	if err := goose.Up(sqlDB, "."); err != nil {
		t.Fatal(err)
	}
	var repaired DiscoveryCandidate
	if err := database.First(&repaired, accepted.ID).Error; err != nil {
		t.Fatal(err)
	}
	if repaired.ProcessingState != DiscoveryProcessingReady || repaired.DedupeState != DiscoveryDedupePending || repaired.BodyText != accepted.BodyText || repaired.CurrentAssessmentID != nil || repaired.AssessmentState != DiscoveryAssessmentPending || repaired.EligibilityReasons != "" {
		t.Fatalf("accepted repair = %+v", repaired)
	}
	if err := database.Table("discovery_article_assessments").Where("id=900001").Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("assessment history lost: %d %v", count, err)
	}
	if err := database.First(&placeholder, placeholder.ID).Error; err != nil {
		t.Fatal(err)
	}
	if placeholder.ProcessingState != DiscoveryProcessingDomainBlocked {
		t.Fatalf("unaccepted placeholder became ready: %+v", placeholder)
	}
	if err := database.First(&pending, pending.ID).Error; err != nil {
		t.Fatal(err)
	}
	if pending.MaterialID != 0 || pending.ProcessingState != DiscoveryProcessingFetchPending || pending.EligibilityReasons != "" || pending.NextProcessingAt == nil {
		t.Fatalf("pending repair = %+v", pending)
	}
	var after material.ArticleState
	if err := database.First(&after, "material_id=?", unaffected.MaterialID).Error; err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatal("repair changed unaffected assessment")
	}
	if err := goose.DownTo(sqlDB, ".", 46); err != nil {
		t.Fatal(err)
	}
	if err := database.First(&repaired, accepted.ID).Error; err != nil || repaired.ProcessingState != DiscoveryProcessingReady {
		t.Fatalf("downgrade reintroduced content restriction: %+v %v", repaired, err)
	}
	if err := goose.Up(sqlDB, "."); err != nil {
		t.Fatal(err)
	}
	if err := database.First(&after, "material_id=?", unaffected.MaterialID).Error; err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("repeat migration changed unaffected content", err)
	}
}
