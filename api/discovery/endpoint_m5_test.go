package discovery

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestEndpointDiscoveryCreatesHomepageAndFeedButLeavesSitemapDisabled(t *testing.T) {
	setupSQLiteDB(t)
	world := newDeterministicSiteWorld(t)
	clock := &advancingClock{now: time.Date(2026, 7, 13, 16, 0, 0, 0, time.UTC)}
	oldClock := discoveryClock
	oldFetcher := fetchDiscoveryRequest
	discoveryClock = clock
	fetchDiscoveryRequest = world.Fetcher.Fetch
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
	result, err := (EndpointDiscoveryService{Clock: clock, Queue: queue}).DiscoverHomepage(context.Background(), site, *homepage, page.Body, page.FinalURL)
	if err != nil {
		t.Fatal(err)
	}
	if result.FeedsFound != 1 || result.SitemapsFound != 0 || result.LinksFound != 1 {
		t.Fatalf("endpoint discovery = %#v", result)
	}
	var endpoints []DiscoverySource
	if err := db.Where("site_id = ?", site.ID).Order("endpoint_type").Find(&endpoints).Error; err != nil {
		t.Fatal(err)
	}
	if len(endpoints) != 2 {
		t.Fatalf("site endpoints = %#v", endpoints)
	}
	for _, endpoint := range endpoints {
		if endpoint.NextDueAt == nil || !endpoint.NextDueAt.Equal(clock.Now()) {
			t.Fatalf("endpoint is not immediately due = %#v", endpoint)
		}
		if endpoint.ID != homepage.ID {
			if _, ok := jobs.keys[fmt.Sprintf("fetch:%d", endpoint.ID)]; !ok {
				t.Fatalf("new endpoint %d was not scheduled: %#v", endpoint.ID, jobs.keys)
			}
		}
	}
	fetchResult, err := FetchDiscoverySource(context.Background(), homepage)
	if err != nil || fetchResult.Stored != 1 || fetchResult.LinksFound != 1 {
		t.Fatalf("homepage ingestion = %#v, %v", fetchResult, err)
	}
	var homepageCandidate DiscoveryCandidate
	if err := db.Where("source_id = ?", homepage.ID).First(&homepageCandidate).Error; err != nil {
		t.Fatal(err)
	}
	if homepageCandidate.Title != "A careful field guide" || homepageCandidate.ProcessingState != DiscoveryProcessingFetchPending {
		t.Fatalf("homepage candidate = %#v", homepageCandidate)
	}
}

func TestFeedPreservesMetadataAndDoesNotRepeatProcessing(t *testing.T) {
	setupSQLiteDB(t)
	if err := db.AutoMigrate(&DiscoveryArticleAssessment{}); err != nil {
		t.Fatal(err)
	}
	clock := &advancingClock{now: time.Date(2026, 7, 13, 16, 0, 0, 0, time.UTC)}
	oldClock := discoveryClock
	oldFetcher := fetchDiscoveryRequest
	oldEnqueue := enqueueCandidateForProcessing
	discoveryClock = clock
	var feedMode = "ok"
	fetchDiscoveryRequest = func(_ context.Context, request FetchRequest) (FetchResult, error) {
		switch {
		case strings.Contains(request.URL, "feed.xml"):
			if feedMode == "not_modified" {
				if request.ETag != `"feed-v1"` {
					t.Fatalf("conditional ETag = %q", request.ETag)
				}
				return FetchResult{StatusCode: http.StatusNotModified, ETag: `"feed-v1"`, NotModified: true}, nil
			}
			return FetchResult{StatusCode: http.StatusOK, ETag: `"feed-v1"`, Body: []byte(`<?xml version="1.0"?><rss version="2.0"><channel><item><title>Trusted Feed Title</title><link>https://shared.example/posts/one</link></item></channel></rss>`)}, nil
		default:
			return FetchResult{}, fmt.Errorf("unexpected URL %s", request.URL)
		}
	}
	var processing []string
	enqueueCandidateForProcessing = func(_ context.Context, candidate DiscoveryCandidate) error {
		processing = append(processing, fmt.Sprintf("%d:%s", candidate.ID, candidateContentVersion(candidate)))
		return nil
	}
	t.Cleanup(func() {
		discoveryClock = oldClock
		fetchDiscoveryRequest = oldFetcher
		enqueueCandidateForProcessing = oldEnqueue
	})

	site := DiscoverySite{RootURL: "https://shared.example/", HostKey: "shared.example", DisplayName: "Shared", Status: DiscoverySiteStatusSeed, DiscoveryMethod: "test", CrawlAllowed: true, RobotsStatus: "unknown", FirstDiscoveredAt: clock.Now(), CreatedAt: clock.Now()}
	if err := db.Create(&site).Error; err != nil {
		t.Fatal(err)
	}
	feed, _, err := upsertSiteEndpoint(site, site.RootURL+"feed.xml", DiscoverySourceTypeFeed, DiscoveryEndpointFeed, clock.Now())
	if err != nil {
		t.Fatal(err)
	}
	first, err := FetchDiscoverySource(context.Background(), &feed)
	if err != nil || first.Stored != 1 {
		t.Fatalf("feed fetch = %#v, %v", first, err)
	}
	clock.Advance(time.Minute)
	second, err := FetchDiscoverySource(context.Background(), &feed)
	if err != nil || second.Stored != 0 {
		t.Fatalf("repeat feed fetch = %#v, %v", second, err)
	}
	feedMode = "not_modified"
	clock.Advance(time.Minute)
	third, err := FetchDiscoverySource(context.Background(), &feed)
	if err != nil || third.Stored != 0 || third.Discovered != 0 {
		t.Fatalf("304 feed fetch = %#v, %v", third, err)
	}

	var candidates []DiscoveryCandidate
	if err := db.Find(&candidates).Error; err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 1 {
		t.Fatalf("logical candidates = %#v", candidates)
	}
	candidate := candidates[0]
	if candidate.SourceID != feed.ID || candidate.Title != "Trusted Feed Title" || candidate.ProcessingState != DiscoveryProcessingFetchPending || candidate.BodyText != "" {
		t.Fatalf("candidate metadata/state = %#v", candidate)
	}
	if len(processing) != 1 || !strings.HasSuffix(processing[0], ":0") {
		t.Fatalf("processing enqueues = %#v", processing)
	}
	var provenance []DiscoveryCandidateProvenance
	if err := db.Order("discovery_method").Find(&provenance).Error; err != nil {
		t.Fatal(err)
	}
	if len(provenance) != 1 || provenance[0].DiscoveryMethod != DiscoveryMethodFeed {
		t.Fatalf("candidate provenance = %#v", provenance)
	}
	var assessments int64
	if err := db.Model(&DiscoveryArticleAssessment{}).Count(&assessments).Error; err != nil || assessments != 0 {
		t.Fatalf("assessment count = %d, %v", assessments, err)
	}
	var runs []DiscoveryFetchRun
	if err := db.Order("id").Find(&runs).Error; err != nil {
		t.Fatal(err)
	}
	if len(runs) != 3 || runs[1].DuplicateCount != 1 || !runs[2].NotModified {
		t.Fatalf("fetch runs = %#v", runs)
	}
}

func TestSitemapIndexParserKeepsOnlySameSiteChildren(t *testing.T) {
	setupSQLiteDB(t)
	now := time.Date(2026, 7, 13, 16, 0, 0, 0, time.UTC)
	site := DiscoverySite{RootURL: "https://index.example/", HostKey: "index.example", DisplayName: "Index", Status: DiscoverySiteStatusObserving, DiscoveryMethod: "test", CrawlAllowed: true, RobotsStatus: "unknown", FirstDiscoveredAt: now, CreatedAt: now}
	if err := db.Create(&site).Error; err != nil {
		t.Fatal(err)
	}
	candidates, nested, err := parseSitemapDocument([]byte(`<?xml version="1.0"?><sitemapindex><sitemap><loc>https://index.example/sitemaps/posts.xml</loc></sitemap><sitemap><loc>https://other.example/foreign.xml</loc></sitemap></sitemapindex>`), site.RootURL)
	if err != nil || len(candidates) != 0 || len(nested) != 1 {
		t.Fatalf("sitemap index candidates=%#v nested=%#v err=%v", candidates, nested, err)
	}
	var sitemapEndpoints int64
	if err := db.Model(&DiscoverySource{}).Where("endpoint_type = ?", DiscoveryEndpointSitemap).Count(&sitemapEndpoints).Error; err != nil || sitemapEndpoints != 0 {
		t.Fatalf("parser created sitemap endpoints: %d, %v", sitemapEndpoints, err)
	}
}

func TestDeclaredFeedMediaTypesExcludeWordPressMetadata(t *testing.T) {
	body := []byte(`<html><head>
<link rel="alternate" type="application/rss+xml" href="/feed.xml">
<link rel="alternate" type="application/rdf+xml; charset=UTF-8" href="/feed/rdf">
<link rel="alternate" type="application/feed+json" href="/feed.json">
<link rel="alternate" type="application/json" href="/wp-json/wp/v2/pages/7">
<link rel="alternate" type="application/json+oembed" href="/wp-json/oembed/1.0/embed">
<link rel="alternate" type="text/xml+oembed" href="/wp-json/oembed/1.0/embed?format=xml">
<link rel="EditURI" type="application/rsd+xml" href="/xmlrpc.php?rsd">
</head></html>`)
	links, err := discoverHomepageEndpoints(body, "https://example.com/", "https://example.com/")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"https://example.com/feed.json", "https://example.com/feed.xml", "https://example.com/feed/rdf"}
	if fmt.Sprint(links.feeds) != fmt.Sprint(want) {
		t.Fatalf("declared feeds = %#v, want %#v", links.feeds, want)
	}
}
