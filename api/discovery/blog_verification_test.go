package discovery

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestBlogVerificationRequiresDeterministicPositiveEvidence(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		wantBlog bool
		wantCode string
	}{
		{name: "declared feed", body: `<html><head><link rel="alternate" type="application/rss+xml" href="/feed.xml"></head></html>`, wantBlog: true, wantCode: BlogVerificationDeclaredFeed},
		{name: "known generator", body: `<html><head><meta name="generator" content="Hugo 0.148"></head></html>`, wantBlog: true, wantCode: BlogVerificationGenerator},
		{name: "structured data", body: `<html><head><script type="application/ld+json">{"@context":"https://schema.org","@type":"Blog"}</script></head></html>`, wantBlog: true, wantCode: BlogVerificationStructuredData},
		{name: "article collection", body: `<html><body><article>One</article><article>Two</article></body></html>`, wantBlog: true, wantCode: BlogVerificationArticleCollection},
		{name: "semantic article link", body: `<html><head><title>Engineering Blog</title></head><body><a href="/posts/one">A detailed post</a></body></html>`, wantBlog: true, wantCode: BlogVerificationSemanticLinks},
		{name: "corporate navigation", body: `<html><head><title>Example Company</title></head><body><nav><a href="/products">Products</a><a href="/pricing">Pricing</a></nav><main><h1>Build faster</h1></main></body></html>`, wantCode: BlogVerificationNoEvidence},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			got, err := VerifyBlogHomepage([]byte(testCase.body), "https://example.com/", "https://example.com/")
			if err != nil {
				t.Fatal(err)
			}
			if got.IsBlog != testCase.wantBlog || got.Evidence != testCase.wantCode {
				t.Fatalf("verification = %#v", got)
			}
		})
	}
}

func TestBlogVerificationPrecedesAutomaticExpansion(t *testing.T) {
	setupSQLiteDB(t)
	now := time.Date(2026, 7, 15, 9, 0, 0, 0, time.UTC)
	clock := &advancingClock{now: now}
	oldClock := discoveryClock
	oldFetch := fetchDiscoveryRequest
	discoveryClock = clock
	requests := make(map[string]int)
	fetchDiscoveryRequest = func(_ context.Context, request FetchRequest) (FetchResult, error) {
		requests[request.URL]++
		switch request.URL {
		case "https://blog.example.com/":
			return FetchResult{StatusCode: http.StatusOK, FinalURL: request.URL, ContentType: "text/html", Body: []byte(`<html><head><title>Field Notes</title><link rel="alternate" type="application/rss+xml" href="/feed.xml"><link rel="sitemap" href="/sitemap.xml"></head><body><a href="/posts/one">First note</a></body></html>`)}, nil
		case "https://blog.example.com/robots.txt":
			return FetchResult{StatusCode: http.StatusNotFound, FinalURL: request.URL, ContentType: "text/plain"}, nil
		case "https://company.example.net/":
			return FetchResult{StatusCode: http.StatusOK, FinalURL: request.URL, ContentType: "text/html", Body: []byte(`<html><head><title>Example Company</title></head><body><nav><a href="/products">Products</a><a href="/pricing">Pricing</a></nav></body></html>`)}, nil
		default:
			return FetchResult{}, fmt.Errorf("unexpected expansion request %s", request.URL)
		}
	}
	t.Cleanup(func() {
		discoveryClock = oldClock
		fetchDiscoveryRequest = oldFetch
	})

	seed := DiscoverySite{RootURL: "https://seed.example.org/", HostKey: "seed.example.org", DomainKey: "example.org", DisplayName: "Seed", Status: DiscoverySiteStatusSeed, DiscoveryMethod: DiscoveryMethodManualSeed, CrawlAllowed: true, FirstDiscoveredAt: now}
	if err := db.Create(&seed).Error; err != nil {
		t.Fatal(err)
	}
	jobs := &recordedJobs{keys: make(map[string]struct{})}
	queue := recordingJobEnqueuer{store: jobs}
	links := []BlogrollLink{
		{TargetURL: "https://blog.example.com/", SourcePageURL: seed.RootURL + "links", AnchorText: "Field Notes", RelationType: "friend", DetectionRule: "explicit_rel", Confidence: .98},
		{TargetURL: "https://company.example.net/", SourcePageURL: seed.RootURL + "links", AnchorText: "Company", RelationType: "friend", DetectionRule: "explicit_rel", Confidence: .97},
	}
	result, err := (SiteGraphService{Clock: clock, Queue: queue, Classifier: SiteClassifier{}}).ApplyLinks(context.Background(), seed, links)
	if err != nil {
		t.Fatal(err)
	}
	if result.SitesCreated != 2 || result.SitesActivated != 2 {
		t.Fatalf("graph apply = %#v", result)
	}
	for key := range jobs.keys {
		if strings.HasPrefix(key, "blogroll:") || strings.HasPrefix(key, "backfill:") {
			t.Fatalf("automatic expansion preceded verification: %#v", jobs.keys)
		}
	}

	var sites []DiscoverySite
	if err := db.Where("domain_key IN ?", []string{"example.com", "example.net"}).Order("domain_key").Find(&sites).Error; err != nil {
		t.Fatal(err)
	}
	if len(sites) != 2 {
		t.Fatalf("observing sites = %#v", sites)
	}
	byDomain := map[string]*DiscoverySite{sites[0].DomainKey: &sites[0], sites[1].DomainKey: &sites[1]}
	for _, domain := range []string{"example.com", "example.net"} {
		var homepage DiscoverySource
		if err := db.Where("site_id = ? AND endpoint_type = ?", byDomain[domain].ID, DiscoveryEndpointHomepage).First(&homepage).Error; err != nil {
			t.Fatal(err)
		}
		if _, err := FetchDiscoverySource(context.Background(), &homepage); err != nil {
			t.Fatalf("verify %s: %v", domain, err)
		}
	}

	var blogSite DiscoverySite
	if err := db.First(&blogSite, byDomain["example.com"].ID).Error; err != nil {
		t.Fatal(err)
	}
	if blogSite.Status != DiscoverySiteStatusActive || !blogSite.CrawlAllowed {
		t.Fatalf("verified blog = %#v", blogSite)
	}
	var companySite DiscoverySite
	if err := db.First(&companySite, byDomain["example.net"].ID).Error; err != nil {
		t.Fatal(err)
	}
	if companySite.Status != DiscoverySiteStatusNonBlog || companySite.CrawlAllowed || companySite.OperationalPause != DiscoveryPauseNonBlog || companySite.OperationalDetails != "blog_verification:"+BlogVerificationNoEvidence {
		t.Fatalf("non-blog classification = %#v", companySite)
	}
	if requests["https://company.example.net/"] != 1 || len(requests) != 2 {
		t.Fatalf("request boundary = %#v", requests)
	}
	var companyHomepage DiscoverySource
	if err := db.Where("site_id = ?", companySite.ID).First(&companyHomepage).Error; err != nil {
		t.Fatal(err)
	}
	if companyHomepage.Enabled {
		t.Fatalf("non-blog homepage remained enabled: %#v", companyHomepage)
	}
	visible, err := ListDiscoverySources()
	if err != nil {
		t.Fatal(err)
	}
	if len(visible) != 0 {
		t.Fatalf("automatically verified blogs appeared as manual subscriptions = %#v", visible)
	}
	before := requests["https://company.example.net/"]
	if scan, err := ScanBlogroll(context.Background(), companySite.ID, queue); err != nil || scan.PagesScanned != 0 {
		t.Fatalf("non-blog scan = %#v, %v", scan, err)
	}
	if requests["https://company.example.net/"] != before {
		t.Fatal("non-blog scan performed another request")
	}
	var edges int64
	if err := db.Model(&DiscoverySiteEdge{}).Count(&edges).Error; err != nil || edges != 2 {
		t.Fatalf("retained graph edges = %d, %v", edges, err)
	}

	resumed, err := UpdateDiscoverySiteOperationalStatus(companySite.ID, DiscoverySiteStatusActive, "owner confirmed blog")
	if err != nil {
		t.Fatal(err)
	}
	if !resumed.CrawlAllowed || resumed.Status != DiscoverySiteStatusActive {
		t.Fatalf("owner recovery = %#v", resumed)
	}
	if err := db.First(&companyHomepage, companyHomepage.ID).Error; err != nil || !companyHomepage.Enabled || companyHomepage.NextDueAt == nil {
		t.Fatalf("owner recovery homepage = %#v, %v", companyHomepage, err)
	}
}

func TestRecoveryDoesNotScanUnverifiedObservingSites(t *testing.T) {
	setupSQLiteDB(t)
	now := time.Date(2026, 7, 15, 9, 0, 0, 0, time.UTC)
	for _, site := range []DiscoverySite{
		{RootURL: "https://seed.example/", HostKey: "seed.example", DomainKey: "seed.example", Status: DiscoverySiteStatusSeed, DiscoveryMethod: DiscoveryMethodManualSeed, CrawlAllowed: true, FirstDiscoveredAt: now},
		{RootURL: "https://active.example/", HostKey: "active.example", DomainKey: "active.example", Status: DiscoverySiteStatusActive, DiscoveryMethod: DiscoveryMethodBlogroll, CrawlAllowed: true, FirstDiscoveredAt: now},
		{RootURL: "https://pending.example/", HostKey: "pending.example", DomainKey: "pending.example", Status: DiscoverySiteStatusObserving, DiscoveryMethod: DiscoveryMethodBlogroll, CrawlAllowed: true, FirstDiscoveredAt: now},
	} {
		current := site
		if err := db.Create(&current).Error; err != nil {
			t.Fatal(err)
		}
	}
	queue := &recoveryRecordingQueue{}
	if err := RecoverDueJobs(context.Background(), queue, now); err != nil {
		t.Fatal(err)
	}
	if len(queue.scans) != 2 {
		t.Fatalf("graph scan recoveries = %#v", queue.scans)
	}
}
