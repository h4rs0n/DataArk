package discovery

import (
	"context"
	"testing"
	"time"
)

func TestFairScheduleKeepsLowYieldFloorAndRewardsExtraSignals(t *testing.T) {
	clock := &advancingClock{now: time.Date(2026, 7, 14, 4, 0, 0, 0, time.UTC)}
	policy := SourceSchedulePolicy{
		Clock: clock, ActiveFeedInterval: 24 * time.Hour, ObservingInterval: 7 * 24 * time.Hour,
		DormantInterval: 30 * 24 * time.Hour, MinimumInterval: time.Hour,
	}
	low := DiscoverySource{ID: 1, EndpointType: DiscoveryEndpointHomepage}
	high := DiscoverySource{ID: 2, EndpointType: DiscoveryEndpointHomepage}
	lowDecision := policy.DecideNextSuccess(low, DiscoverySiteStatusObserving, false, DiscoverySiteOperationalStats{CandidateCount: 100, EligibleCandidateCount: 0, DuplicateCandidateCount: 90})
	highDecision := policy.DecideNextSuccess(high, DiscoverySiteStatusObserving, false, DiscoverySiteOperationalStats{IndependentInboundSites: 3, CandidateCount: 100, EligibleCandidateCount: 20, PositiveFeedbackArticles: 4})
	if lowDecision.Basis != "base_floor" || lowDecision.Chosen != 7*24*time.Hour {
		t.Fatalf("low-yield decision = %#v", lowDecision)
	}
	if highDecision.Basis != "extra_budget" || highDecision.Chosen >= lowDecision.Chosen || highDecision.Explanation == "" {
		t.Fatalf("high-output decision = %#v", highDecision)
	}

	checks := 0
	deadline := clock.Now().Add(120 * 24 * time.Hour)
	for clock.Now().Before(deadline) {
		decision := policy.DecideNextSuccess(low, DiscoverySiteStatusObserving, false, DiscoverySiteOperationalStats{CandidateCount: 1000, EligibleCandidateCount: 0, DuplicateCandidateCount: 999})
		if decision.NextDueAt.Sub(clock.Now()) > 7*24*time.Hour {
			t.Fatalf("low-yield source exceeded maximum idle time: %#v", decision)
		}
		checks++
		clock.Advance(decision.NextDueAt.Sub(clock.Now()))
	}
	if checks < 17 {
		t.Fatalf("low-yield checks over four months = %d, want at least 17", checks)
	}
}

func TestSiteOperationsExposeNamedStatsAndScheduleBasis(t *testing.T) {
	setupSQLiteDB(t)
	clock := &advancingClock{now: time.Date(2026, 7, 14, 5, 0, 0, 0, time.UTC)}
	oldClock := discoveryClock
	discoveryClock = clock
	t.Cleanup(func() { discoveryClock = oldClock })

	seedA := DiscoverySite{RootURL: "https://a.example/", HostKey: "a.example", Status: DiscoverySiteStatusSeed, DiscoveryMethod: DiscoveryMethodManualSeed, CrawlAllowed: true, FirstDiscoveredAt: clock.Now()}
	seedB := DiscoverySite{RootURL: "https://b.example/", HostKey: "b.example", Status: DiscoverySiteStatusSeed, DiscoveryMethod: DiscoveryMethodManualSeed, CrawlAllowed: true, FirstDiscoveredAt: clock.Now()}
	target := DiscoverySite{RootURL: "https://tail.example/", HostKey: "tail.example", Status: DiscoverySiteStatusObserving, DiscoveryMethod: DiscoveryMethodBlogroll, GraphDepth: 1, CrawlAllowed: true, FirstDiscoveredAt: clock.Now()}
	for _, site := range []*DiscoverySite{&seedA, &seedB, &target} {
		if err := db.Create(site).Error; err != nil {
			t.Fatal(err)
		}
	}
	for index, from := range []DiscoverySite{seedA, seedB} {
		edge := DiscoverySiteEdge{EdgeKey: string(rune('a' + index)), FromSiteID: from.ID, ToSiteID: target.ID, SourcePageURL: from.RootURL + "links", RelationType: "blogroll", FirstSeenAt: clock.Now(), LastSeenAt: clock.Now(), Active: true}
		if err := db.Create(&edge).Error; err != nil {
			t.Fatal(err)
		}
	}
	source := DiscoverySource{Name: "Tail Feed", URL: "https://tail.example/feed.xml", Type: DiscoverySourceTypeFeed, EndpointType: DiscoveryEndpointFeed, SiteID: &target.ID, Enabled: true}
	if err := db.Create(&source).Error; err != nil {
		t.Fatal(err)
	}
	candidate := DiscoveryCandidate{SourceID: source.ID, SourceName: source.Name, URL: "https://tail.example/gem", Status: DiscoveryCandidateStatusNew, ProcessingState: DiscoveryProcessingReady, EligibilityState: DiscoveryEligibilityEligible, DedupeState: DiscoveryDedupeReady, LastSeenAt: clock.Now()}
	if err := db.Create(&candidate).Error; err != nil {
		t.Fatal(err)
	}
	provenance := DiscoveryCandidateProvenance{ProvenanceKey: "tail-gem", CandidateID: candidate.ID, SiteID: target.ID, SourceID: &source.ID, DiscoveryMethod: "feed", OriginalURL: candidate.URL, FirstSeenAt: clock.Now(), LastSeenAt: clock.Now()}
	if err := db.Create(&provenance).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&UserCandidateState{UserID: 9, CandidateID: candidate.ID, CurrentFeedback: UserCandidateFeedbackValuable}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&DiscoveryFetchRun{SiteID: &target.ID, SourceID: source.ID, StartedAt: clock.Now(), Status: "succeeded", NewCount: 1}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&DiscoveryBackfillState{SiteID: target.ID, Strategy: BackfillStrategyArchive, Status: BackfillStatusPending, URLsSeen: 10, ArticlesFound: 2}).Error; err != nil {
		t.Fatal(err)
	}
	stats, err := RefreshDiscoverySiteOperationalStats(target.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stats.IndependentInboundSites != 2 || stats.FetchSuccesses != 1 || stats.CandidateCount != 1 || stats.EligibleCandidateCount != 1 || stats.PositiveFeedbackArticles != 1 || stats.BackfillURLsSeen != 10 {
		t.Fatalf("site stats = %#v", stats)
	}
	policy := SourceSchedulePolicy{Clock: clock, ObservingInterval: 7 * 24 * time.Hour, ActiveFeedInterval: 24 * time.Hour, MinimumInterval: time.Hour}
	decision := policy.DecideNextSuccess(source, target.Status, false, *stats)
	if err := SaveDiscoverySourceScheduleDecision(source, decision); err != nil {
		t.Fatal(err)
	}
	operations, err := GetDiscoverySiteOperations(target.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(operations.Endpoints) != 1 || operations.Endpoints[0].Decision == nil || operations.Endpoints[0].Decision.Basis != "extra_budget" {
		t.Fatalf("site operations = %#v", operations)
	}
	if operations.Endpoints[0].Decision.Explanation == "" || operations.Endpoints[0].Decision.ChosenIntervalSeconds >= operations.Endpoints[0].Decision.BaseIntervalSeconds {
		t.Fatalf("schedule explanation = %#v", operations.Endpoints[0].Decision)
	}

	due := clock.Now().Add(-time.Minute)
	if err := db.Model(&source).Update("next_due_at", &due).Error; err != nil {
		t.Fatal(err)
	}
	other := DiscoverySource{Name: "Other Due", URL: "https://other.example/feed.xml", Type: DiscoverySourceTypeFeed, EndpointType: DiscoveryEndpointFeed, Enabled: true, NextDueAt: &due}
	if err := db.Create(&other).Error; err != nil {
		t.Fatal(err)
	}
	queue := &recoveryRecordingQueue{}
	if err := RecoverDueJobs(context.Background(), queue, clock.Now()); err != nil {
		t.Fatal(err)
	}
	if len(queue.fetches) != 2 {
		t.Fatalf("fair recovery fetches = %#v, want each due source once", queue.fetches)
	}
}
