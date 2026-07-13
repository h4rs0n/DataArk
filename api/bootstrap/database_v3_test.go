package bootstrap

import (
	"DataArk/auth"
	"DataArk/discovery"
	appmigrations "DataArk/migrations"
	"DataArk/recommendation"
	"strings"
	"testing"
	"time"

	"github.com/pressly/goose/v3"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type legacyUser struct {
	ID        uint `gorm:"primaryKey"`
	Username  string
	Password  string
	CreatedAt time.Time
	UpdatedAt time.Time
}

func (legacyUser) TableName() string { return "users" }

type legacyDiscoverySource struct {
	ID            uint `gorm:"primaryKey"`
	Name          string
	URL           string `gorm:"uniqueIndex"`
	Type          string
	Enabled       bool
	ETag          string
	LastModified  string
	FailureCount  int
	NextFetchAt   *time.Time
	CrawlConfig   string
	LastFetchedAt *time.Time
	LastError     string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

func (legacyDiscoverySource) TableName() string { return "discovery_sources" }

type legacyDiscoveryCandidate struct {
	ID          uint `gorm:"primaryKey"`
	SourceID    uint
	SourceName  string
	URL         string `gorm:"uniqueIndex"`
	Title       string
	Summary     string
	Author      string
	Status      string
	Score       float64
	PublishedAt *time.Time
	LastSeenAt  time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func (legacyDiscoveryCandidate) TableName() string { return "discovery_candidates" }

type legacyRecommendationDay struct {
	ID                 uint `gorm:"primaryKey"`
	UserID             uint
	RecommendationDate string
	Timezone           string
	Status             string
	RequestedCount     int
	ActualCount        int
	GeneratedAt        *time.Time
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

func (legacyRecommendationDay) TableName() string { return "recommendation_days" }

type legacyRecommendationItem struct {
	ID          uint `gorm:"primaryKey"`
	DayID       uint
	UserID      uint
	CandidateID uint
	DedupeKey   string
	Rank        int
	Reason      string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func (legacyRecommendationItem) TableName() string { return "recommendation_items" }

type legacyRecommendationFeedback struct {
	ID                   uint `gorm:"primaryKey"`
	UserID               uint
	RecommendationItemID uint
	CandidateID          uint
	Action               string
	CreatedAt            time.Time
}

func (legacyRecommendationFeedback) TableName() string { return "recommendation_feedbacks" }

func TestV3SQLiteMigrationPreservesAndBackfillsLegacyData(t *testing.T) {
	database, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared&_foreign_keys=on"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	legacyModels := []interface{}{
		&legacyUser{},
		&legacyDiscoverySource{},
		&legacyDiscoveryCandidate{},
		&legacyRecommendationDay{},
		&legacyRecommendationItem{},
		&legacyRecommendationFeedback{},
	}
	if err := database.AutoMigrate(legacyModels...); err != nil {
		t.Fatal(err)
	}

	now := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	users := []legacyUser{
		{ID: 1, Username: "admin", Password: "hash", CreatedAt: now, UpdatedAt: now},
		{ID: 2, Username: "member", Password: "hash", CreatedAt: now, UpdatedAt: now},
	}
	if err := database.Create(&users).Error; err != nil {
		t.Fatal(err)
	}
	sources := []legacyDiscoverySource{
		{ID: 10, Name: "Example Feed", URL: "https://www.example.com/feed.xml", Type: discovery.DiscoverySourceTypeFeed, Enabled: true, CreatedAt: now, UpdatedAt: now},
		{ID: 11, Name: "Example Home", URL: "https://example.com/", Type: discovery.DiscoverySourceTypeSite, Enabled: true, CreatedAt: now, UpdatedAt: now},
	}
	if err := database.Create(&sources).Error; err != nil {
		t.Fatal(err)
	}
	candidates := []legacyDiscoveryCandidate{
		{ID: 20, SourceID: 10, SourceName: "Example Feed", URL: "https://example.com/one", Title: "Frozen title", Summary: "Frozen summary", Author: "Author", Status: discovery.DiscoveryCandidateStatusNew, LastSeenAt: now, CreatedAt: now, UpdatedAt: now},
		{ID: 21, SourceID: 11, SourceName: "Example Home", URL: "https://example.com/two", Title: "Second title", Status: discovery.DiscoveryCandidateStatusNew, LastSeenAt: now, CreatedAt: now, UpdatedAt: now},
	}
	if err := database.Create(&candidates).Error; err != nil {
		t.Fatal(err)
	}
	day := legacyRecommendationDay{ID: 30, UserID: 1, RecommendationDate: "2026-07-13", Timezone: "Asia/Shanghai", Status: recommendation.RecommendationDayStatusGenerated, RequestedCount: 10, ActualCount: 1, GeneratedAt: &now, CreatedAt: now, UpdatedAt: now}
	if err := database.Create(&day).Error; err != nil {
		t.Fatal(err)
	}
	item := legacyRecommendationItem{ID: 40, DayID: 30, UserID: 1, CandidateID: 20, DedupeKey: "legacy-key", Rank: 1, Reason: "legacy reason", CreatedAt: now, UpdatedAt: now}
	if err := database.Create(&item).Error; err != nil {
		t.Fatal(err)
	}
	feedback := legacyRecommendationFeedback{ID: 50, UserID: 1, RecommendationItemID: 40, CandidateID: 20, Action: "valuable", CreatedAt: now}
	if err := database.Create(&feedback).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Exec("CREATE UNIQUE INDEX idx_recommendation_items_user_candidate ON recommendation_items(user_id, candidate_id)").Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Exec("CREATE UNIQUE INDEX uq_recommendation_user_dedupe ON recommendation_items(user_id, dedupe_key)").Error; err != nil {
		t.Fatal(err)
	}

	if err := database.AutoMigrate(&auth.User{}, &discovery.DiscoverySource{}, &discovery.DiscoveryCandidate{}, &discovery.DiscoveryCandidateFeedback{}); err != nil {
		t.Fatal(err)
	}
	if err := migrateV3Compatibility(database); err != nil {
		t.Fatal(err)
	}
	if err := migrateV3Compatibility(database); err != nil {
		t.Fatalf("second migration run was not idempotent: %v", err)
	}

	assertCount(t, database, &auth.User{}, 2)
	assertCount(t, database, &discovery.DiscoverySource{}, 2)
	assertCount(t, database, &discovery.DiscoveryCandidate{}, 2)
	assertCount(t, database, &recommendation.RecommendationDay{}, 1)
	assertCount(t, database, &recommendation.RecommendationItem{}, 1)
	assertCount(t, database, &recommendation.RecommendationFeedback{}, 1)
	assertCount(t, database, &discovery.DiscoverySite{}, 1)
	assertCount(t, database, &discovery.DiscoveryCandidateProvenance{}, 2)

	var admin auth.User
	var member auth.User
	if err := database.First(&admin, 1).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.First(&member, 2).Error; err != nil {
		t.Fatal(err)
	}
	if admin.Role != auth.UserRoleOwner || member.Role != auth.UserRoleMember {
		t.Fatalf("roles after migration: admin=%q member=%q", admin.Role, member.Role)
	}

	var migratedSources []discovery.DiscoverySource
	if err := database.Order("id").Find(&migratedSources).Error; err != nil {
		t.Fatal(err)
	}
	if migratedSources[0].SiteID == nil || migratedSources[1].SiteID == nil || *migratedSources[0].SiteID != *migratedSources[1].SiteID {
		t.Fatalf("legacy endpoints did not map to one logical site: %#v", migratedSources)
	}
	if migratedSources[0].EndpointType != "feed" || migratedSources[1].EndpointType != "homepage" {
		t.Fatalf("endpoint types = %q, %q", migratedSources[0].EndpointType, migratedSources[1].EndpointType)
	}

	var migratedCandidate discovery.DiscoveryCandidate
	if err := database.First(&migratedCandidate, 20).Error; err != nil {
		t.Fatal(err)
	}
	if migratedCandidate.FirstSeenAt == nil || migratedCandidate.ProcessingState != discovery.DiscoveryProcessingFetchPending || migratedCandidate.EligibilityState != discovery.DiscoveryEligibilityUnknown {
		t.Fatalf("candidate compatibility fields = %#v", migratedCandidate)
	}

	var migratedDay recommendation.RecommendationDay
	var migratedItem recommendation.RecommendationItem
	if err := database.First(&migratedDay, 30).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.First(&migratedItem, 40).Error; err != nil {
		t.Fatal(err)
	}
	if migratedDay.PolicyVersion != "v2" || migratedDay.PublishedAt == nil || migratedDay.AuditVersion != 1 {
		t.Fatalf("day compatibility fields = %#v", migratedDay)
	}
	if migratedItem.SnapshotTitle != "Frozen title" || migratedItem.SnapshotURL != "https://example.com/one" || migratedItem.SnapshotSummary != "Frozen summary" || migratedItem.SnapshotSource != "Example Feed" {
		t.Fatalf("item snapshot = %#v", migratedItem)
	}

	state := discovery.UserCandidateState{UserID: 1, CandidateID: 20}
	if err := database.Create(&state).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Create(&discovery.UserCandidateState{UserID: 1, CandidateID: 20}).Error; err == nil {
		t.Fatal("duplicate user/candidate state should violate its unique constraint")
	}
	if err := database.Create(&discovery.UserCandidateState{UserID: 1, CandidateID: 9999}).Error; err == nil {
		t.Fatal("user candidate state should enforce candidate foreign key")
	}

	secondDay := recommendation.RecommendationDay{UserID: 1, RecommendationDate: "2026-07-14", Timezone: "Asia/Shanghai", Status: recommendation.RecommendationDayStatusGenerated, RequestedCount: 10}
	if err := database.Create(&secondDay).Error; err != nil {
		t.Fatal(err)
	}
	secondItem := recommendation.RecommendationItem{DayID: secondDay.ID, UserID: 1, CandidateID: 20, DedupeKey: "legacy-key", Rank: 1}
	if err := database.Create(&secondItem).Error; err != nil {
		t.Fatalf("cross-day candidate cooldown history must be representable: %v", err)
	}
	if err := database.Create(&recommendation.RecommendationItem{DayID: secondDay.ID, UserID: 1, CandidateID: 20, Rank: 2}).Error; err == nil {
		t.Fatal("same day/candidate should remain unique")
	}
}

func TestV3GooseMigrationIsAdditiveAndParseable(t *testing.T) {
	goose.SetBaseFS(appmigrations.FS)
	migrations, err := goose.CollectMigrations(".", 0, goose.MaxVersion)
	if err != nil {
		t.Fatal(err)
	}
	if len(migrations) != 8 || migrations[len(migrations)-1].Version != 8 {
		t.Fatalf("goose migrations = %#v", migrations)
	}
	body, err := appmigrations.FS.ReadFile("000003_blog_discovery_v3.sql")
	if err != nil {
		t.Fatal(err)
	}
	sql := string(body)
	for _, required := range []string{
		"CREATE TABLE IF NOT EXISTS discovery_sites",
		"CREATE TABLE IF NOT EXISTS discovery_candidate_provenances",
		"CREATE TABLE IF NOT EXISTS user_candidate_states",
		"DROP CONSTRAINT IF EXISTS recommendation_items_user_id_candidate_id_key",
		"Goose Down is a data-preserving no-op",
	} {
		if !strings.Contains(sql, required) {
			t.Fatalf("migration missing %q", required)
		}
	}
	down := strings.Split(sql, "-- +goose Down")
	if len(down) != 2 || strings.Contains(strings.ToUpper(down[1]), "DROP TABLE") || strings.Contains(strings.ToUpper(down[1]), "DROP COLUMN") {
		t.Fatal("v3 Down migration must preserve additive data")
	}
	observability, err := appmigrations.FS.ReadFile("000004_discovery_fetch_observability.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"final_url", "content_type", "robots_status", "retained on rollback"} {
		if !strings.Contains(string(observability), required) {
			t.Fatalf("fetch observability migration missing %q", required)
		}
	}
	graphEvidence, err := appmigrations.FS.ReadFile("000005_blogroll_graph_evidence.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"detection_rule", "context_summary", "activated_at", "retained on rollback"} {
		if !strings.Contains(string(graphEvidence), required) {
			t.Fatalf("graph evidence migration missing %q", required)
		}
	}
	recentIngestion, err := appmigrations.FS.ReadFile("000006_recent_ingestion_metadata.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"metadata_confidence", "provenance remain useful"} {
		if !strings.Contains(string(recentIngestion), required) {
			t.Fatalf("recent ingestion migration missing %q", required)
		}
	}
	historicalBackfill, err := appmigrations.FS.ReadFile("000007_historical_backfill_observability.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"published_confidence", "last_batch_at", "retained on rollback"} {
		if !strings.Contains(string(historicalBackfill), required) {
			t.Fatalf("historical backfill migration missing %q", required)
		}
	}
	articleProcessing, err := appmigrations.FS.ReadFile("000008_article_processing_pipeline.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"processing_attempts", "next_processing_at", "dedupe_state", "assessment_state", "discovery_article_content_versions", "content_hash", "Data-preserving rollback"} {
		if !strings.Contains(string(articleProcessing), required) {
			t.Fatalf("article processing migration missing %q", required)
		}
	}
}

func assertCount(t *testing.T, database *gorm.DB, model interface{}, want int64) {
	t.Helper()
	var got int64
	if err := database.Model(model).Count(&got).Error; err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("%T count = %d, want %d", model, got, want)
	}
}
