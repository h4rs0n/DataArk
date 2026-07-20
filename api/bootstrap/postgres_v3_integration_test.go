package bootstrap

import (
	"DataArk/archive"
	"DataArk/auth"
	appdatabase "DataArk/database"
	"DataArk/discovery"
	"DataArk/jobqueue"
	appmigrations "DataArk/migrations"
	"DataArk/recommendation"
	"context"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pressly/goose/v3"
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
	goose.SetBaseFS(appmigrations.FS)
	if err := goose.SetDialect("postgres"); err != nil {
		t.Fatal(err)
	}
	if err := goose.UpTo(sqlDB, ".", 19); err != nil {
		t.Fatalf("migrate through subscription tiers: %v", err)
	}
	legacyNow := time.Now().UTC()
	legacySite := discovery.DiscoverySite{
		RootURL: "https://legacy-sitemap.invalid/", HostKey: "legacy-sitemap.invalid", DomainKey: "legacy-sitemap.invalid",
		DisplayName: "Legacy sitemap", Status: discovery.DiscoverySiteStatusSeed, DiscoveryMethod: discovery.DiscoveryMethodManualSeed,
		CrawlAllowed: true, FirstDiscoveredAt: legacyNow,
	}
	if err := database.Create(&legacySite).Error; err != nil {
		t.Fatalf("create pre-migration sitemap site: %v", err)
	}
	legacySitemap := discovery.DiscoverySource{
		Name: "Legacy sitemap", URL: legacySite.RootURL + "sitemap.xml", Type: discovery.DiscoverySourceTypeSitemap,
		EndpointType: discovery.DiscoveryEndpointSitemap, SiteID: &legacySite.ID, UserManaged: true, Enabled: true,
		NextDueAt: &legacyNow, NextFetchAt: &legacyNow,
	}
	if err := database.Create(&legacySitemap).Error; err != nil {
		t.Fatalf("create pre-migration sitemap endpoint: %v", err)
	}
	legacyBackfill := discovery.DiscoveryBackfillState{
		SiteID: legacySite.ID, Strategy: discovery.BackfillStrategySitemap, Cursor: `{\"pending\":[\"https://legacy-sitemap.invalid/sitemap.xml\"],\"visited\":[]}`,
		Status: discovery.BackfillStatusPending, NextBatchAt: &legacyNow,
	}
	if err := database.Create(&legacyBackfill).Error; err != nil {
		t.Fatalf("create pre-migration sitemap backfill: %v", err)
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
		"SELECT version_id::text FROM goose_db_version WHERE is_applied ORDER BY id DESC LIMIT 1", "22")
	assertPostgresScalar(t, database,
		"SELECT extname FROM pg_extension WHERE extname = 'vector'", "vector")
	assertPostgresScalar(t, database,
		"SELECT to_regclass('public.river_job')::text", "river_job")
	assertPostgresScalar(t, database,
		"SELECT to_regclass('public.recommendation_feed_batches')::text", "recommendation_feed_batches")
	assertPostgresScalar(t, database,
		fmt.Sprintf("SELECT enabled::text || ':' || user_managed::text FROM discovery_sources WHERE id = %d", legacySitemap.ID), "false:false")
	assertPostgresScalar(t, database,
		fmt.Sprintf("SELECT status || ':' || completion_reason FROM discovery_backfill_states WHERE id = %d", legacyBackfill.ID), "paused:sitemap_requires_owner_request")
	assertPostgresScalar(t, database,
		fmt.Sprintf("SELECT (owner_requested_at IS NULL)::text FROM discovery_backfill_states WHERE id = %d", legacyBackfill.ID), "true")

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

	legacyArgs := `{"source_id":4343}`
	interruptedArgs := `{"source_id":4344}`
	if err := database.Exec(`
INSERT INTO river_job (state, attempt, max_attempts, attempted_at, attempted_by, args, kind, queue)
VALUES
	('available', 0, 25, NULL, NULL, CAST(? AS jsonb), ?, 'default'),
	('running', 1, 25, ?, ARRAY['legacy-worker'], CAST(? AS jsonb), ?, 'default')`,
		legacyArgs, jobqueue.FetchSourceJobKind, time.Now().Add(-time.Minute), interruptedArgs, jobqueue.FetchSourceJobKind,
	).Error; err != nil {
		t.Fatalf("insert legacy default-queue discovery jobs: %v", err)
	}

	var crawlExecutions atomic.Int32
	crawlStop, err := jobqueue.Start(context.Background(), database, jobqueue.Handlers{
		FetchSource: func(context.Context, uint) error {
			crawlExecutions.Add(1)
			return nil
		},
	}, func(ctx context.Context, queue jobqueue.JobEnqueuer) error {
		return queue.EnqueueFetchSource(ctx, 4242)
	})
	if err != nil {
		t.Fatalf("start manually gated River runtime: %v", err)
	}
	time.Sleep(250 * time.Millisecond)
	if crawlExecutions.Load() != 0 {
		crawlStop()
		t.Fatalf("startup crawl executions = %d, want 0", crawlExecutions.Load())
	}
	assertPostgresScalar(t, database,
		"SELECT count(*)::text FROM river_job WHERE queue = 'default' AND kind = 'discovery_fetch_source' AND state IN ('available', 'pending', 'retryable', 'running', 'scheduled')", "0")
	assertPostgresScalar(t, database,
		"SELECT count(*)::text FROM river_job WHERE queue = 'discovery_crawl' AND kind = 'discovery_fetch_source' AND state = 'running'", "0")
	controller, available := jobqueue.CrawlControl()
	if !available {
		crawlStop()
		t.Fatal("manual crawl controller unavailable")
	}
	snapshot, err := controller.Snapshot(context.Background(), 50)
	if err != nil || snapshot.State != "waiting" || snapshot.Counts.Pending < 3 || snapshot.Counts.Running != 0 {
		crawlStop()
		t.Fatalf("paused PostgreSQL snapshot = %#v err=%v", snapshot, err)
	}
	if _, err := controller.Run(context.Background()); err != nil {
		crawlStop()
		t.Fatalf("run PostgreSQL crawl queue: %v", err)
	}
	waitForPostgresCondition(t, 10*time.Second, func() bool { return crawlExecutions.Load() == 3 })
	crawlStop()
	assertPostgresScalar(t, database,
		"SELECT count(*)::text FROM river_job WHERE kind = 'discovery_fetch_source' AND state = 'completed'", "3")
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
