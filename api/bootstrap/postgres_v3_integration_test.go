package bootstrap

import (
	"DataArk/archive"
	"DataArk/auth"
	appdatabase "DataArk/database"
	"DataArk/discovery"
	"DataArk/jobqueue"
	"DataArk/recommendation"
	"context"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// TestPostgresV3MigrationsRiverRestartAndPGVector is an opt-in release-gate
// test. It must only be pointed at a disposable, empty database because it
// exercises the complete production migration and persistent queue paths.
func TestPostgresV3MigrationsRiverRestartAndPGVector(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("DATAARK_POSTGRES_TEST_DSN"))
	if dsn == "" {
		t.Skip("set DATAARK_POSTGRES_TEST_DSN to a disposable PostgreSQL database")
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

	var databaseName string
	if err := database.Raw("SELECT current_database()").Scan(&databaseName).Error; err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(databaseName, "dataark_v3_verify") {
		t.Fatalf("refusing to run destructive integration setup against database %q", databaseName)
	}

	previousDatabase := appdatabase.SetDB(database)
	configureDomainDatabases(database)
	t.Cleanup(func() {
		appdatabase.SetDB(previousDatabase)
		configureDomainDatabases(previousDatabase)
	})

	if err := appdatabase.AutoMigrate(
		&auth.User{},
		&archive.ArchiveTask{},
		&archive.ArchiveStat{},
		&archive.ArchiveDocument{},
		&archive.SearchEvent{},
		&archive.ArchiveClickEvent{},
		&discovery.DiscoverySource{},
		&discovery.DiscoveryCandidate{},
		&discovery.DiscoveryCandidateFeedback{},
	); err != nil {
		t.Fatalf("auto-migrate legacy schema: %v", err)
	}
	for attempt := 1; attempt <= 2; attempt++ {
		if err := appdatabase.RunDatabaseMigrations(database); err != nil {
			t.Fatalf("production migrations attempt %d: %v", attempt, err)
		}
		if err := migrateV3Compatibility(database); err != nil {
			t.Fatalf("compatibility backfill attempt %d: %v", attempt, err)
		}
	}

	assertPostgresScalar(t, database,
		"SELECT version_id::text FROM goose_db_version WHERE is_applied ORDER BY id DESC LIMIT 1", "19")
	assertPostgresScalar(t, database,
		"SELECT extname FROM pg_extension WHERE extname = 'vector'", "vector")
	assertPostgresScalar(t, database,
		"SELECT to_regclass('public.river_job')::text", "river_job")

	now := time.Now().UTC()
	source := discovery.DiscoverySource{
		Name: "Synthetic PostgreSQL source", URL: "https://fixture.invalid/feed.xml", Type: discovery.DiscoverySourceTypeFeed,
		EndpointType: discovery.DiscoveryEndpointFeed, Enabled: true, CreatedAt: now, UpdatedAt: now,
	}
	if err := database.Create(&source).Error; err != nil {
		t.Fatalf("create PostgreSQL source with empty JSON config: %v", err)
	}
	assertPostgresScalar(t, database,
		fmt.Sprintf("SELECT crawl_config::text FROM discovery_sources WHERE id = %d", source.ID), "{}")

	longCandidateURL := "https://discourse.gohugo.io/t/hugo-module-dart-sass-use-works-when-used-directly-but-fails-when-routed-through-another-scss-file/57267"
	if len(longCandidateURL) <= 128 {
		t.Fatalf("long PostgreSQL fixture URL length = %d", len(longCandidateURL))
	}
	candidate := discovery.DiscoveryCandidate{
		SourceID:         source.ID,
		SourceName:       source.Name,
		URL:              longCandidateURL,
		NormalizedURL:    longCandidateURL,
		CanonicalURL:     longCandidateURL,
		DedupeKey:        "url:" + discovery.ContentHash(longCandidateURL),
		Title:            "Synthetic pgvector fixture",
		Status:           discovery.DiscoveryCandidateStatusNew,
		ProcessingState:  discovery.DiscoveryProcessingReady,
		EligibilityState: discovery.DiscoveryEligibilityEligible,
		LastSeenAt:       now,
	}
	if err := database.Create(&candidate).Error; err != nil {
		t.Fatalf("create long-URL vector fixture: %v", err)
	}
	assertPostgresScalar(t, database,
		"SELECT character_maximum_length::text FROM information_schema.columns WHERE table_name = 'discovery_candidates' AND column_name = 'dedupe_key'", "128")
	assertPostgresScalar(t, database,
		fmt.Sprintf("SELECT dedupe_key FROM discovery_candidates WHERE id = %d", candidate.ID),
		"url:"+discovery.ContentHash(longCandidateURL))
	if err := recommendation.StoreCandidateEmbedding(context.Background(), candidate.ID, "fixture", []float32{0.25, 0.5, 0.75}); err != nil {
		t.Fatalf("store candidate embedding: %v", err)
	}
	assertPostgresScalar(t, database,
		fmt.Sprintf("SELECT embedding::text FROM discovery_candidates WHERE id = %d", candidate.ID),
		"[0.25,0.5,0.75]")
	assertPostgresScalar(t, database,
		fmt.Sprintf("SELECT topics::text || '|' || entities::text FROM discovery_candidates WHERE id = %d", candidate.ID),
		"[]|[]")

	var executions atomic.Int32
	handlers := jobqueue.Handlers{GenerateDaily: func(context.Context, uint, string) error {
		executions.Add(1)
		return nil
	}}
	for restart := 1; restart <= 2; restart++ {
		localDate := fmt.Sprintf("2026-07-%02d", 20+restart)
		stop, err := jobqueue.Start(context.Background(), database, handlers, func(ctx context.Context, queue jobqueue.JobEnqueuer) error {
			return queue.EnqueueGenerateDaily(ctx, uint(9000+restart), localDate)
		})
		if err != nil {
			t.Fatalf("start River runtime %d: %v", restart, err)
		}
		waitForPostgresCondition(t, 10*time.Second, func() bool {
			return executions.Load() >= int32(restart)
		})
		stop()
	}
	assertPostgresScalar(t, database,
		"SELECT count(*)::text FROM river_job WHERE kind = 'recommendation_generate_daily' AND state = 'completed'", "2")
}

func assertPostgresScalar(t *testing.T, database *gorm.DB, query string, want string) {
	t.Helper()
	var got string
	if err := database.Raw(query).Scan(&got).Error; err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("query %q = %q, want %q", query, got, want)
	}
}

func waitForPostgresCondition(t *testing.T, timeout time.Duration, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatal("timed out waiting for PostgreSQL integration condition")
}
