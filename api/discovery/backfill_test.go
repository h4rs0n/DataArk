package discovery

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestBackfillFindsArchiveOnlyHistoricalArticlesWithoutSitemap(t *testing.T) {
	setupSQLiteDB(t)
	world := newDeterministicSiteWorld(t)
	clock := &advancingClock{now: time.Date(2026, 7, 13, 18, 0, 0, 0, time.UTC)}
	oldClock := discoveryClock
	oldFetcher := fetchDiscoveryRequest
	discoveryClock = clock
	var sitemapRequests int
	fetchDiscoveryRequest = func(ctx context.Context, request FetchRequest) (FetchResult, error) {
		if strings.Contains(request.URL, "sitemap") {
			sitemapRequests++
			t.Fatalf("sitemap request is not allowed: %#v", request)
		}
		return world.Fetcher.Fetch(ctx, request)
	}
	t.Cleanup(func() {
		discoveryClock = oldClock
		fetchDiscoveryRequest = oldFetcher
	})
	homepage, err := CreateDiscoverySource("Thoughtful B", world.B.URL+"/", DiscoverySourceTypeSite, true)
	if err != nil {
		t.Fatal(err)
	}
	var site DiscoverySite
	if err := db.First(&site, *homepage.SiteID).Error; err != nil {
		t.Fatal(err)
	}
	page, err := world.Fetcher.Fetch(context.Background(), FetchRequest{URL: site.RootURL, Kind: FetchKindHTML})
	if err != nil {
		t.Fatal(err)
	}
	jobs := &recordedJobs{keys: make(map[string]struct{})}
	queue := recordingJobEnqueuer{store: jobs}
	if _, err := (EndpointDiscoveryService{Clock: clock, Queue: queue}).DiscoverHomepage(context.Background(), site, *homepage, page.Body, page.FinalURL); err != nil {
		t.Fatal(err)
	}
	if _, err := RunBackfillSite(context.Background(), site.ID, queue); err != nil {
		t.Fatal(err)
	}
	if _, err := RunBackfillSite(context.Background(), site.ID, queue); err != nil {
		t.Fatal(err)
	}
	var historical []DiscoveryCandidate
	if err := db.Where("url IN ?", []string{world.B.URL + "/articles/high-sitemap.html", world.B.URL + "/articles/high-archive.html"}).Order("url").Find(&historical).Error; err != nil {
		t.Fatal(err)
	}
	if len(historical) != 2 {
		t.Fatalf("historical candidates = %#v", historical)
	}
	for _, candidate := range historical {
		if candidate.ProcessingState != DiscoveryProcessingFetchPending || candidate.PublishedAt == nil || candidate.PublishedConfidence == "" {
			t.Fatalf("historical candidate state = %#v", candidate)
		}
	}
	coverage, err := ListBackfillCoverage(site.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(coverage) != 1 {
		t.Fatalf("coverage = %#v", coverage)
	}
	item := coverage[0]
	if item.State.Strategy != BackfillStrategyArchive || item.State.Status != BackfillStatusCompleted || item.EstimatedCompletion != 1 {
		t.Fatalf("completed coverage = %#v", item)
	}
	feedBody, err := world.Fetcher.Fetch(context.Background(), FetchRequest{URL: world.B.URL + "/feed.xml", Kind: FetchKindFeed})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(feedBody.Body), "high-sitemap") || strings.Contains(string(feedBody.Body), "high-archive") {
		t.Fatal("historical-only articles leaked into recent feed fixture")
	}
	if sitemapRequests != 0 {
		t.Fatalf("sitemap requests = %d", sitemapRequests)
	}
}

func TestBackfillPersistsCursorAcrossFailureRestartAndIdempotentReplay(t *testing.T) {
	setupSQLiteDB(t)
	clock := &advancingClock{now: time.Date(2026, 7, 13, 18, 0, 0, 0, time.UTC)}
	oldClock := discoveryClock
	oldFetcher := fetchDiscoveryRequest
	discoveryClock = clock
	var failPageTwo = true
	fetchDiscoveryRequest = func(_ context.Context, request FetchRequest) (FetchResult, error) {
		switch request.URL {
		case "https://restart.example/archive/page-1":
			return FetchResult{StatusCode: http.StatusOK, FinalURL: request.URL, Body: []byte(`<html><body><a href="/archive/page-2">Older posts</a><a href="/posts/old-one">Old one</a></body></html>`)}, nil
		case "https://restart.example/archive/page-2":
			if failPageTwo {
				return FetchResult{}, errors.New("fixture interruption")
			}
			return FetchResult{StatusCode: http.StatusOK, FinalURL: request.URL, Body: []byte(`<html><body><a href="/posts/old-two">Old two</a></body></html>`)}, nil
		default:
			return FetchResult{}, fmt.Errorf("unexpected URL %s", request.URL)
		}
	}
	t.Cleanup(func() {
		discoveryClock = oldClock
		fetchDiscoveryRequest = oldFetcher
	})
	site := DiscoverySite{RootURL: "https://restart.example/", HostKey: "restart.example", DisplayName: "Restart", Status: DiscoverySiteStatusObserving, DiscoveryMethod: "test", CrawlAllowed: true, RobotsStatus: "unknown", FirstDiscoveredAt: clock.Now(), CreatedAt: clock.Now()}
	if err := db.Create(&site).Error; err != nil {
		t.Fatal(err)
	}
	homepage := DiscoverySource{Name: "Restart", URL: site.RootURL, Type: DiscoverySourceTypeSite, SiteID: &site.ID, EndpointType: DiscoveryEndpointHomepage, Enabled: true}
	if err := db.Create(&homepage).Error; err != nil {
		t.Fatal(err)
	}
	state, _, err := ensureBackfillState(site.ID, BackfillStrategyArchive, []string{site.RootURL + "archive/page-1"}, clock.Now())
	if err != nil {
		t.Fatal(err)
	}
	queue := recordingJobEnqueuer{store: &recordedJobs{keys: make(map[string]struct{})}}
	first, err := RunBackfillSite(context.Background(), site.ID, queue)
	if err != nil || first.BatchNumber != 1 || first.Status != BackfillStatusPending {
		t.Fatalf("first batch = %#v, %v", first, err)
	}
	firstCursor := decodeBackfillCursor(first.Cursor)
	if len(firstCursor.Pending) != 1 || !strings.Contains(firstCursor.Pending[0], "page-2") {
		t.Fatalf("persisted cursor = %#v", firstCursor)
	}
	clock.Advance(time.Hour)
	failed, err := RunBackfillSite(context.Background(), site.ID, queue)
	if err == nil || failed.FailureCount != 1 {
		t.Fatalf("interrupted batch = %#v, %v", failed, err)
	}
	failedCursor := decodeBackfillCursor(failed.Cursor)
	if len(failedCursor.Pending) != 1 || failedCursor.Pending[0] != firstCursor.Pending[0] {
		t.Fatalf("failure advanced cursor: before=%#v after=%#v", firstCursor, failedCursor)
	}
	failPageTwo = false
	clock.Advance(24 * time.Hour)
	completed, err := RunBackfillSite(context.Background(), site.ID, queue)
	if err != nil || completed.Status != BackfillStatusCompleted {
		t.Fatalf("resumed batch = %#v, %v", completed, err)
	}
	var candidates int64
	if err := Candidates(db).Model(&DiscoveryCandidate{}).Where("url LIKE ?", "https://restart.example/posts/%").Count(&candidates).Error; err != nil || candidates != 2 {
		t.Fatalf("candidate count = %d, %v", candidates, err)
	}
	// Replay the first page as if a crash happened after candidate writes but
	// before cursor persistence. Logical candidates and provenance stay idempotent.
	state.Cursor = encodeBackfillCursor(backfillCursor{Pending: []string{site.RootURL + "archive/page-1"}, Visited: []string{}})
	if err := db.Model(&state).Updates(map[string]interface{}{"cursor": state.Cursor, "status": BackfillStatusPending, "completion_reason": "", "next_batch_at": clock.Now()}).Error; err != nil {
		t.Fatal(err)
	}
	replayed, err := RunBackfillSite(context.Background(), site.ID, queue)
	if err != nil || replayed.DuplicateCount == 0 || replayed.NextBatchAt == nil {
		t.Fatalf("replayed batch = %#v, %v", replayed, err)
	}
	if replayed.NextBatchAt.Sub(clock.Now()) > 7*24*time.Hour {
		t.Fatalf("low-yield next batch is not finite: %s", replayed.NextBatchAt)
	}
	if err := Candidates(db).Model(&DiscoveryCandidate{}).Where("url LIKE ?", "https://restart.example/posts/%").Count(&candidates).Error; err != nil || candidates != 2 {
		t.Fatalf("replay candidate count = %d, %v", candidates, err)
	}
}
