package discovery

import (
	"context"
	"errors"
	"testing"
	"time"
)

type recoveryRecordingQueue struct {
	fetches     []uint
	scans       []uint
	backfills   []uint
	candidates  []uint
	assessments []uint
	failSource  uint
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

func (queue *recoveryRecordingQueue) EnqueueAssessArticle(_ context.Context, candidateID uint, _ string) error {
	queue.assessments = append(queue.assessments, candidateID)
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
	backfill := DiscoveryBackfillState{SiteID: site.ID, Strategy: BackfillStrategyArchive, Status: BackfillStatusPending, NextBatchAt: &past}
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

func TestRecoverDueJobsEnqueuesPendingAssessmentsSeparately(t *testing.T) {
	setupSQLiteDB(t)
	now := time.Date(2026, 8, 18, 10, 0, 0, 0, time.UTC)
	source := DiscoverySource{Name: "Due", URL: "https://assess.example/feed.xml", Type: DiscoverySourceTypeFeed, EndpointType: "feed", Enabled: true}
	if err := db.Create(&source).Error; err != nil {
		t.Fatal(err)
	}
	pendingBody := DiscoveryCandidate{SourceID: source.ID, SourceName: source.Name, URL: "https://assess.example/fetch", Status: DiscoveryCandidateStatusNew, ProcessingState: DiscoveryProcessingFetchPending, EligibilityState: DiscoveryEligibilityUnknown, LastSeenAt: now}
	pendingAssess := DiscoveryCandidate{SourceID: source.ID, SourceName: source.Name, URL: "https://assess.example/ready", Status: DiscoveryCandidateStatusNew, ProcessingState: DiscoveryProcessingReady, DedupeState: DiscoveryDedupeReady, AssessmentState: DiscoveryAssessmentPending, ContentVersion: 1, EligibilityState: DiscoveryEligibilityUnknown, LastSeenAt: now}
	if err := db.Create(&pendingBody).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&pendingAssess).Error; err != nil {
		t.Fatal(err)
	}
	queue := &recoveryRecordingQueue{}
	if err := RecoverDueJobs(context.Background(), queue, now); err != nil {
		t.Fatal(err)
	}
	if len(queue.candidates) != 1 || queue.candidates[0] != pendingBody.ID {
		t.Fatalf("body recoveries = %#v", queue.candidates)
	}
	if len(queue.assessments) != 0 {
		t.Fatalf("discovery recovery must not enqueue assessments = %#v", queue.assessments)
	}
}

func TestRecoverDueSourcesUsesSubscriptionTierOrder(t *testing.T) {
	setupSQLiteDB(t)
	now := time.Date(2026, 7, 16, 9, 0, 0, 0, time.UTC)
	past := now.Add(-time.Minute)
	blogroll := DiscoverySource{Name: "Blogroll", URL: "https://friend.example/feed", Type: DiscoverySourceTypeFeed, EndpointType: DiscoveryEndpointFeed, Priority: DiscoveryPriorityBlogroll, Enabled: true, NextDueAt: &past}
	manual := DiscoverySource{Name: "Manual", URL: "https://manual.example/feed", Type: DiscoverySourceTypeFeed, EndpointType: DiscoveryEndpointFeed, UserManaged: true, Priority: DiscoveryPriorityManual, Enabled: true, NextDueAt: &past}
	if err := db.Create(&blogroll).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&manual).Error; err != nil {
		t.Fatal(err)
	}
	queue := &recoveryRecordingQueue{}
	if err := RecoverDueJobs(context.Background(), queue, now); err != nil {
		t.Fatal(err)
	}
	if len(queue.fetches) != 2 || queue.fetches[0] != manual.ID || queue.fetches[1] != blogroll.ID {
		t.Fatalf("tiered fetch recoveries = %#v", queue.fetches)
	}
}

func TestRecoverDueSourcesReconcilesLegacyFailureCooldown(t *testing.T) {
	setupSQLiteDB(t)
	now := time.Date(2026, 8, 11, 3, 0, 0, 0, time.UTC)
	past := now.Add(-time.Minute)
	recentAttempt := now.Add(-2 * time.Hour)
	oldAttempt := now.Add(-2 * 24 * time.Hour)
	structural := DiscoverySource{
		Name: "Structural", URL: "https://structural.example/feed.xml", Type: DiscoverySourceTypeFeed,
		EndpointType: DiscoveryEndpointFeed, Enabled: true, FailureCount: 1, LastAttemptAt: &recentAttempt,
		NextDueAt: &past, NextFetchAt: &past, BackoffUntil: &past, BackoffReason: "content_type",
	}
	timeout := DiscoverySource{
		Name: "Timeout", URL: "https://timeout.example/feed.xml", Type: DiscoverySourceTypeFeed,
		EndpointType: DiscoveryEndpointFeed, Enabled: true, FailureCount: 1, LastAttemptAt: &oldAttempt,
		NextDueAt: &past, NextFetchAt: &past, BackoffUntil: &past, BackoffReason: "timeout",
	}
	if err := db.Create(&structural).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&timeout).Error; err != nil {
		t.Fatal(err)
	}

	queue := &recoveryRecordingQueue{}
	if err := RecoverDueJobs(context.Background(), queue, now); err != nil {
		t.Fatal(err)
	}
	if len(queue.fetches) != 1 || queue.fetches[0] != timeout.ID {
		t.Fatalf("recovered fetches = %#v, want only timeout source %d", queue.fetches, timeout.ID)
	}
	if err := db.First(&structural, structural.ID).Error; err != nil {
		t.Fatal(err)
	}
	if structural.NextDueAt == nil || !structural.NextDueAt.After(now) || structural.BackoffUntil == nil || !structural.BackoffUntil.Equal(*structural.NextDueAt) {
		t.Fatalf("reconciled structural source = %#v", structural)
	}
}

func TestRecoverySkipsSitemapWithoutExplicitOwnerRequest(t *testing.T) {
	setupSQLiteDB(t)
	now := time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)
	site := DiscoverySite{RootURL: "https://legacy.example/", HostKey: "legacy.example", DomainKey: "legacy.example", Status: DiscoverySiteStatusSeed, DiscoveryMethod: DiscoveryMethodManualSeed, CrawlAllowed: true, FirstDiscoveredAt: now}
	if err := db.Create(&site).Error; err != nil {
		t.Fatal(err)
	}
	state := DiscoveryBackfillState{
		SiteID: site.ID, Strategy: BackfillStrategySitemap, Status: BackfillStatusPending,
		Cursor: encodeBackfillCursor(backfillCursor{Pending: []string{"https://legacy.example/sitemap.xml"}}), NextBatchAt: &now,
	}
	if err := db.Create(&state).Error; err != nil {
		t.Fatal(err)
	}
	queue := &recoveryRecordingQueue{}
	if err := RecoverDueJobs(context.Background(), queue, now); err != nil {
		t.Fatal(err)
	}
	if len(queue.backfills) != 0 {
		t.Fatalf("unrequested sitemap recovery = %#v", queue.backfills)
	}
}

func TestRecoverDueJobsSkipsAllBlacklistedCandidateWork(t *testing.T) {
	setupSQLiteDB(t)
	now := time.Date(2026, 7, 20, 10, 0, 0, 0, time.UTC)
	if err := db.Create(&DiscoveryDomainBlacklistEntry{Domain: "blocked.example"}).Error; err != nil {
		t.Fatal(err)
	}
	blockedSite := DiscoverySite{RootURL: "https://blocked.example/", HostKey: "blocked.example", DomainKey: "blocked.example", Status: DiscoverySiteStatusSeed, DiscoveryMethod: DiscoveryMethodManualSeed, CrawlAllowed: true, FirstDiscoveredAt: now}
	allowedSite := DiscoverySite{RootURL: "https://allowed.example/", HostKey: "allowed.example", DomainKey: "allowed.example", Status: DiscoverySiteStatusSeed, DiscoveryMethod: DiscoveryMethodManualSeed, CrawlAllowed: true, FirstDiscoveredAt: now}
	if err := db.Create(&blockedSite).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&allowedSite).Error; err != nil {
		t.Fatal(err)
	}
	blockedSource := DiscoverySource{Name: "Blocked", URL: "https://blocked.example/feed", CrawlHost: "blocked.example", Type: DiscoverySourceTypeFeed, SiteID: &blockedSite.ID, Enabled: true, NextDueAt: &now}
	allowedSource := DiscoverySource{Name: "Allowed", URL: "https://allowed.example/feed", CrawlHost: "allowed.example", Type: DiscoverySourceTypeFeed, SiteID: &allowedSite.ID, Enabled: true, NextDueAt: &now}
	if err := db.Create(&blockedSource).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&allowedSource).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&DiscoveryBackfillState{SiteID: blockedSite.ID, Strategy: BackfillStrategyArchive, Status: BackfillStatusPending, NextBatchAt: &now}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&DiscoveryBackfillState{SiteID: allowedSite.ID, Strategy: BackfillStrategyArchive, Status: BackfillStatusPending, NextBatchAt: &now}).Error; err != nil {
		t.Fatal(err)
	}
	blockedCandidate := DiscoveryCandidate{SourceID: blockedSource.ID, SourceName: blockedSource.Name, URL: "https://blocked.example/article", CrawlHost: "blocked.example", Status: DiscoveryCandidateStatusNew, ProcessingState: DiscoveryProcessingFetchPending, EligibilityState: DiscoveryEligibilityUnknown, LastSeenAt: now}
	readyCandidate := DiscoveryCandidate{SourceID: blockedSource.ID, SourceName: blockedSource.Name, URL: "https://blocked.example/ready", CrawlHost: "blocked.example", Status: DiscoveryCandidateStatusNew, ProcessingState: DiscoveryProcessingReady, EligibilityState: DiscoveryEligibilityUnknown, DedupeState: DiscoveryDedupePending, ContentHash: "hash", LastSeenAt: now}
	if err := db.Create(&blockedCandidate).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&readyCandidate).Error; err != nil {
		t.Fatal(err)
	}
	queue := &recoveryRecordingQueue{}
	if err := RecoverDueJobs(context.Background(), queue, now); err != nil {
		t.Fatal(err)
	}
	if len(queue.fetches) != 1 || queue.fetches[0] != allowedSource.ID {
		t.Fatalf("fetches = %#v", queue.fetches)
	}
	if len(queue.scans) != 1 || queue.scans[0] != allowedSite.ID {
		t.Fatalf("scans = %#v", queue.scans)
	}
	if len(queue.backfills) != 1 || queue.backfills[0] != allowedSite.ID {
		t.Fatalf("backfills = %#v", queue.backfills)
	}
	if len(queue.candidates) != 0 {
		t.Fatalf("candidates = %#v", queue.candidates)
	}
}
