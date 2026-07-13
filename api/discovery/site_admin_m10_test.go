package discovery

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestOwnerPausePreservesCandidatesAndStopsSiteWork(t *testing.T) {
	setupSQLiteDB(t)
	now := time.Date(2026, 7, 14, 3, 0, 0, 0, time.UTC)
	due := now.Add(-time.Hour)
	site := DiscoverySite{RootURL: "https://pause.example/", HostKey: "pause.example", Status: DiscoverySiteStatusActive, DiscoveryMethod: DiscoveryMethodManualSeed, CrawlAllowed: true, RobotsStatus: "allowed", FirstDiscoveredAt: now, NextGraphScanAt: &due}
	if err := db.Create(&site).Error; err != nil {
		t.Fatal(err)
	}
	source := DiscoverySource{Name: "Pause Feed", URL: "https://pause.example/feed.xml", Type: DiscoverySourceTypeFeed, EndpointType: DiscoveryEndpointFeed, SiteID: &site.ID, Enabled: true, NextDueAt: &due}
	if err := db.Create(&source).Error; err != nil {
		t.Fatal(err)
	}
	candidate := DiscoveryCandidate{SourceID: source.ID, SourceName: source.Name, URL: "https://pause.example/post", Status: DiscoveryCandidateStatusNew, LastSeenAt: now}
	if err := db.Create(&candidate).Error; err != nil {
		t.Fatal(err)
	}
	backfill := DiscoveryBackfillState{SiteID: site.ID, Strategy: BackfillStrategyArchive, Status: BackfillStatusPending, NextBatchAt: &due}
	if err := db.Create(&backfill).Error; err != nil {
		t.Fatal(err)
	}

	paused, err := UpdateDiscoverySiteOperationalStatus(site.ID, DiscoverySiteStatusPaused, "maintenance")
	if err != nil {
		t.Fatal(err)
	}
	if paused.CrawlAllowed || paused.OperationalPause != DiscoveryPauseOwnerPaused || paused.OperationalDetails != "maintenance" {
		t.Fatalf("paused site = %#v", paused)
	}
	var candidateCount int64
	if err := db.Model(&DiscoveryCandidate{}).Where("id = ?", candidate.ID).Count(&candidateCount).Error; err != nil {
		t.Fatal(err)
	}
	if candidateCount != 1 {
		t.Fatalf("candidate count after pause = %d, want 1", candidateCount)
	}
	queue := &recoveryRecordingQueue{}
	if err := RecoverDueJobs(context.Background(), queue, now); err != nil {
		t.Fatal(err)
	}
	if len(queue.fetches) != 0 || len(queue.scans) != 0 || len(queue.backfills) != 0 {
		t.Fatalf("paused site work was scheduled: %#v", queue)
	}
	if _, err := FetchDiscoverySource(context.Background(), &source); !errors.Is(err, ErrDiscoverySiteNotCrawlable) {
		t.Fatalf("paused source fetch err = %v", err)
	}
	if err := RequestDiscoverySiteBackfill(context.Background(), site.ID); !errors.Is(err, ErrDiscoverySiteNotCrawlable) {
		t.Fatalf("paused backfill err = %v", err)
	}

	resumed, err := UpdateDiscoverySiteOperationalStatus(site.ID, DiscoverySiteStatusActive, "")
	if err != nil {
		t.Fatal(err)
	}
	if !resumed.CrawlAllowed || resumed.OperationalPause != "" || resumed.NextGraphScanAt == nil {
		t.Fatalf("resumed site = %#v", resumed)
	}
	var resumedBackfill DiscoveryBackfillState
	if err := db.First(&resumedBackfill, backfill.ID).Error; err != nil {
		t.Fatal(err)
	}
	if resumedBackfill.Status != BackfillStatusPending || resumedBackfill.NextBatchAt == nil {
		t.Fatalf("resumed backfill = %#v", resumedBackfill)
	}
}

func TestGlobalSafetyBlockIsExplicitAndReversible(t *testing.T) {
	setupSQLiteDB(t)
	now := time.Date(2026, 7, 14, 3, 30, 0, 0, time.UTC)
	site := DiscoverySite{RootURL: "https://blocked.example/", HostKey: "blocked.example", Status: DiscoverySiteStatusObserving, DiscoveryMethod: DiscoveryMethodBlogroll, CrawlAllowed: true, RobotsStatus: "unknown", FirstDiscoveredAt: now}
	if err := db.Create(&site).Error; err != nil {
		t.Fatal(err)
	}
	blocked, err := UpdateDiscoverySiteOperationalStatus(site.ID, DiscoverySiteStatusBlocked, "malware review")
	if err != nil {
		t.Fatal(err)
	}
	if blocked.Status != DiscoverySiteStatusBlocked || blocked.OperationalPause != DiscoveryPauseGlobalSafety || blocked.OperationalDetails != "malware review" || blocked.CrawlAllowed {
		t.Fatalf("blocked site = %#v", blocked)
	}
	if _, err := UpdateDiscoverySiteOperationalStatus(site.ID, "deleted", ""); err == nil {
		t.Fatal("unsupported destructive site status unexpectedly accepted")
	}
}
