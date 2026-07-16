package discovery

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"
)

func TestHomepageDiscoveryNeverUsesSitemapByDefault(t *testing.T) {
	setupSQLiteDB(t)
	clock := &advancingClock{now: time.Date(2026, 7, 16, 10, 0, 0, 0, time.UTC)}
	oldClock := discoveryClock
	oldFetcher := fetchDiscoveryRequest
	discoveryClock = clock
	requests := make([]FetchRequest, 0)
	fetchDiscoveryRequest = func(_ context.Context, request FetchRequest) (FetchResult, error) {
		requests = append(requests, request)
		return FetchResult{StatusCode: http.StatusNotFound}, nil
	}
	t.Cleanup(func() {
		discoveryClock = oldClock
		fetchDiscoveryRequest = oldFetcher
	})

	manual, err := CreateDiscoverySource("Notes", "https://notes.example.com/feed.xml", DiscoverySourceTypeFeed, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := CreateDiscoverySource("Forbidden", "https://notes.example.com/sitemap.xml", DiscoverySourceTypeSitemap, true); !errors.Is(err, ErrSitemapSourceDisabled) {
		t.Fatalf("manual sitemap source error = %v", err)
	}
	var site DiscoverySite
	if err := db.First(&site, *manual.SiteID).Error; err != nil {
		t.Fatal(err)
	}
	body := []byte(`<html><head><link rel="alternate" type="application/rss+xml" href="/feed.xml"><link rel="sitemap" href="/declared.xml"></head><body><h1>Notes</h1></body></html>`)
	result, err := (EndpointDiscoveryService{Clock: clock}).DiscoverHomepage(context.Background(), site, *manual, body, site.RootURL)
	if err != nil {
		t.Fatal(err)
	}
	if result.FeedsFound != 1 || result.SitemapsFound != 0 || len(requests) != 0 {
		t.Fatalf("default endpoint discovery = %#v requests=%#v", result, requests)
	}
	var sitemapSources int64
	if err := db.Model(&DiscoverySource{}).Where("endpoint_type = ? OR type = ?", DiscoveryEndpointSitemap, DiscoverySourceTypeSitemap).Count(&sitemapSources).Error; err != nil || sitemapSources != 0 {
		t.Fatalf("default sitemap sources = %d, %v", sitemapSources, err)
	}
	var sitemapBackfills int64
	if err := db.Model(&DiscoveryBackfillState{}).Where("strategy = ?", BackfillStrategySitemap).Count(&sitemapBackfills).Error; err != nil || sitemapBackfills != 0 {
		t.Fatalf("default sitemap backfills = %d, %v", sitemapBackfills, err)
	}

	legacy := DiscoverySource{Name: "Legacy sitemap", URL: "https://notes.example.com/sitemap.xml", Type: DiscoverySourceTypeSitemap, EndpointType: DiscoveryEndpointSitemap, SiteID: manual.SiteID, Enabled: true}
	if err := db.Create(&legacy).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := FetchDiscoverySource(context.Background(), &legacy); !errors.Is(err, ErrSitemapSourceDisabled) {
		t.Fatalf("legacy sitemap fetch error = %v", err)
	}
	if len(requests) != 0 {
		t.Fatalf("disabled sitemap source reached fetcher: %#v", requests)
	}
}

func TestOwnerSitemapGapFillRequiresManualSameDomainSeed(t *testing.T) {
	setupSQLiteDB(t)
	clock := &advancingClock{now: time.Date(2026, 7, 16, 11, 0, 0, 0, time.UTC)}
	oldClock := discoveryClock
	oldFetcher := fetchDiscoveryRequest
	discoveryClock = clock
	fetchDiscoveryRequest = func(_ context.Context, request FetchRequest) (FetchResult, error) {
		if request.URL != "https://notes.example.com/history.xml" || request.Kind != FetchKindSitemap {
			t.Fatalf("unexpected explicit sitemap request = %#v", request)
		}
		return FetchResult{StatusCode: http.StatusOK, FinalURL: request.URL, ContentType: "application/xml", Body: []byte(`<?xml version="1.0"?><urlset><url><loc>https://notes.example.com/posts/old-note</loc><lastmod>2024-01-02</lastmod></url></urlset>`)}, nil
	}
	t.Cleanup(func() {
		discoveryClock = oldClock
		fetchDiscoveryRequest = oldFetcher
	})

	manual, err := CreateDiscoverySource("Notes", "https://notes.example.com/feed.xml", DiscoverySourceTypeFeed, true)
	if err != nil {
		t.Fatal(err)
	}
	queue := recordingJobEnqueuer{store: &recordedJobs{keys: make(map[string]struct{})}}
	if err := requestDiscoverySiteSitemapBackfill(context.Background(), *manual.SiteID, "https://other.example.net/sitemap.xml", queue); !errors.Is(err, ErrSitemapDomainMismatch) {
		t.Fatalf("cross-domain sitemap error = %v", err)
	}
	var states int64
	if err := db.Model(&DiscoveryBackfillState{}).Count(&states).Error; err != nil || states != 0 {
		t.Fatalf("rejected request created state = %d, %v", states, err)
	}
	if err := requestDiscoverySiteSitemapBackfill(context.Background(), *manual.SiteID, "https://notes.example.com/history.xml", queue); err != nil {
		t.Fatal(err)
	}
	var state DiscoveryBackfillState
	if err := db.Where("site_id = ? AND strategy = ?", *manual.SiteID, BackfillStrategySitemap).First(&state).Error; err != nil {
		t.Fatal(err)
	}
	if state.OwnerRequestedAt == nil || state.Status != BackfillStatusPending {
		t.Fatalf("explicit sitemap state = %#v", state)
	}
	var sitemapSources int64
	if err := db.Model(&DiscoverySource{}).Where("endpoint_type = ? OR type = ?", DiscoveryEndpointSitemap, DiscoverySourceTypeSitemap).Count(&sitemapSources).Error; err != nil || sitemapSources != 0 {
		t.Fatalf("explicit gap fill created source = %d, %v", sitemapSources, err)
	}
	if _, err := RunBackfillSite(context.Background(), *manual.SiteID, queue); err != nil {
		t.Fatal(err)
	}
	var candidate DiscoveryCandidate
	if err := db.Where("url = ?", "https://notes.example.com/posts/old-note").First(&candidate).Error; err != nil {
		t.Fatal(err)
	}
	if candidate.ProcessingState != DiscoveryProcessingFetchPending || candidate.EligibilityState != DiscoveryEligibilityUnknown {
		t.Fatalf("sitemap candidate bypassed article pipeline = %#v", candidate)
	}
	var provenance DiscoveryCandidateProvenance
	if err := db.Where("candidate_id = ?", candidate.ID).First(&provenance).Error; err != nil {
		t.Fatal(err)
	}
	if provenance.DiscoveryMethod != DiscoveryMethodSitemap || provenance.SourcePageURL != "https://notes.example.com/history.xml" {
		t.Fatalf("sitemap provenance = %#v", provenance)
	}

	auto := DiscoverySite{RootURL: "https://friend.example.org/", HostKey: "friend.example.org", DomainKey: "example.org", Status: DiscoverySiteStatusActive, DiscoveryMethod: DiscoveryMethodBlogroll, CrawlAllowed: true, FirstDiscoveredAt: clock.Now()}
	if err := db.Create(&auto).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := ensureHomepageEndpoint(db, auto, clock.Now()); err != nil {
		t.Fatal(err)
	}
	if err := requestDiscoverySiteSitemapBackfill(context.Background(), auto.ID, "https://friend.example.org/sitemap.xml", queue); !errors.Is(err, ErrSitemapRequiresManualSeed) {
		t.Fatalf("automatic site sitemap error = %v", err)
	}
}
