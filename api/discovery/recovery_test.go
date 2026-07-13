package discovery

import (
	"context"
	"errors"
	"testing"
	"time"
)

type recoveryRecordingQueue struct {
	fetches    []uint
	scans      []uint
	backfills  []uint
	candidates []uint
	failSource uint
}

func (queue *recoveryRecordingQueue) EnqueueFetchSource(_ context.Context, sourceID uint) error {
	queue.fetches = append(queue.fetches, sourceID)
	if sourceID == queue.failSource {
		return errors.New("fixture source failure")
	}
	return nil
}

func (queue *recoveryRecordingQueue) EnqueueScanBlogroll(_ context.Context, siteID uint) error {
	queue.scans = append(queue.scans, siteID)
	return nil
}

func (queue *recoveryRecordingQueue) EnqueueBackfillSite(_ context.Context, siteID uint) error {
	queue.backfills = append(queue.backfills, siteID)
	return nil
}

func (queue *recoveryRecordingQueue) EnqueueProcessCandidate(_ context.Context, candidateID uint, _ string) error {
	queue.candidates = append(queue.candidates, candidateID)
	return nil
}

func (*recoveryRecordingQueue) EnqueueGenerateDaily(context.Context, uint, string) error {
	return nil
}

func TestRecoverDueJobsContinuesAfterIndependentSourceFailure(t *testing.T) {
	setupSQLiteDB(t)
	if err := db.AutoMigrate(V3Models()...); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 13, 9, 0, 0, 0, time.UTC)
	past := now.Add(-time.Hour)
	future := now.Add(time.Hour)
	site := DiscoverySite{RootURL: "https://recovery.example/", HostKey: "recovery.example", Status: DiscoverySiteStatusSeed, DiscoveryMethod: "test", CrawlAllowed: true, RobotsStatus: "unknown", FirstDiscoveredAt: now}
	if err := db.Create(&site).Error; err != nil {
		t.Fatal(err)
	}
	due := DiscoverySource{Name: "Due", URL: "https://recovery.example/feed.xml", Type: DiscoverySourceTypeFeed, EndpointType: "feed", Enabled: true, NextDueAt: &past}
	futureSource := DiscoverySource{Name: "Future", URL: "https://future.example/feed.xml", Type: DiscoverySourceTypeFeed, EndpointType: "feed", Enabled: true, NextDueAt: &future}
	if err := db.Create(&due).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&futureSource).Error; err != nil {
		t.Fatal(err)
	}
	backfill := DiscoveryBackfillState{SiteID: site.ID, Strategy: "sitemap", Status: "pending", NextBatchAt: &past}
	if err := db.Create(&backfill).Error; err != nil {
		t.Fatal(err)
	}
	candidate := DiscoveryCandidate{SourceID: due.ID, SourceName: due.Name, URL: "https://recovery.example/article", Status: DiscoveryCandidateStatusNew, ProcessingState: "fetch_pending", EligibilityState: DiscoveryEligibilityUnknown, LastSeenAt: now}
	if err := db.Create(&candidate).Error; err != nil {
		t.Fatal(err)
	}
	futureCandidate := DiscoveryCandidate{SourceID: due.ID, SourceName: due.Name, URL: "https://recovery.example/article-later", Status: DiscoveryCandidateStatusNew, ProcessingState: DiscoveryProcessingFetchPending, EligibilityState: DiscoveryEligibilityUnknown, NextProcessingAt: &future, LastSeenAt: now}
	if err := db.Create(&futureCandidate).Error; err != nil {
		t.Fatal(err)
	}

	queue := &recoveryRecordingQueue{failSource: due.ID}
	if err := RecoverDueJobs(context.Background(), queue, now); err == nil {
		t.Fatal("recovery should report the isolated source enqueue failure")
	}
	if len(queue.fetches) != 1 || queue.fetches[0] != due.ID {
		t.Fatalf("fetch recoveries = %#v", queue.fetches)
	}
	if len(queue.scans) != 1 || queue.scans[0] != site.ID {
		t.Fatalf("graph scan recoveries = %#v", queue.scans)
	}
	if len(queue.backfills) != 1 || queue.backfills[0] != site.ID {
		t.Fatalf("backfill recoveries = %#v", queue.backfills)
	}
	if len(queue.candidates) != 1 || queue.candidates[0] != candidate.ID {
		t.Fatalf("candidate recoveries = %#v", queue.candidates)
	}
}
