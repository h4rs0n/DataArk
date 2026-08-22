package discovery

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"DataArk/config"
)

func TestBlogrollDiscovererUsesEvidenceAndRejectsOrdinaryLinks(t *testing.T) {
	discoverer := BlogrollDiscoverer{}
	body := []byte(`<!doctype html><html><head><title>Notes</title></head><body>
<main><p>Reference: <a href="https://news.example/story">news</a></p></main>
<section aria-label="People I read"><h2>Friends</h2><ul>
<li><a rel="friend" href="https://b.example/">B</a></li>
<li><a href="https://c.example/">C</a></li>
</ul></section><a href="/links">Links</a></body></html>`)
	result, err := discoverer.Discover(body, "https://a.example/", "https://a.example/")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Links) != 2 {
		t.Fatalf("blogroll links = %#v", result.Links)
	}
	for _, link := range result.Links {
		if strings.Contains(link.TargetURL, "news.example") || link.DetectionRule == "" || link.ContextSummary == "" {
			t.Fatalf("unexpected link evidence = %#v", link)
		}
	}
	if len(result.DedicatedURLs) != 1 || result.DedicatedURLs[0] != "https://a.example/links" {
		t.Fatalf("dedicated pages = %#v", result.DedicatedURLs)
	}
}

func TestBlogrollDiscovererRecognizesStableExternalList(t *testing.T) {
	body := []byte(`<html><head><title>Bookmarks</title></head><body><section><ul>
<li><a href="https://one.example/">One</a></li><li><a href="https://two.example/">Two</a></li><li><a href="https://three.example/">Three</a></li>
</ul></section></body></html>`)
	result, err := (BlogrollDiscoverer{}).Discover(body, "https://seed.example/about", "https://seed.example/")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Links) != 3 {
		t.Fatalf("stable list links = %#v", result.Links)
	}
	for _, link := range result.Links {
		if link.DetectionRule != "stable_external_list" {
			t.Fatalf("stable list evidence = %#v", link)
		}
	}
}

func TestBlogrollGraphFixtureIsBoundedObservableAndQualityIndependent(t *testing.T) {
	setupSQLiteDB(t)
	world := newDeterministicSiteWorld(t)
	clock := &advancingClock{now: time.Date(2026, 7, 13, 14, 0, 0, 0, time.UTC)}
	oldClock := discoveryClock
	oldFetcher := fetchDiscoveryRequest
	discoveryClock = clock
	fetchDiscoveryRequest = world.Fetcher.Fetch
	t.Cleanup(func() {
		discoveryClock = oldClock
		fetchDiscoveryRequest = oldFetcher
	})

	seedSource, err := CreateDiscoverySource("Seed A", world.A.URL+"/", DiscoverySourceTypeSite, true)
	if err != nil {
		t.Fatal(err)
	}
	var seed DiscoverySite
	if err := db.First(&seed, *seedSource.SiteID).Error; err != nil {
		t.Fatal(err)
	}
	// Deliberately poor historical metadata is present before graph recognition;
	// graph membership must not consult it.
	if err := db.Create(&DiscoveryCandidate{
		SourceID: seedSource.ID, SourceName: seedSource.Name, URL: world.B.URL + "/ordinary",
		QualityScore: 0, Status: DiscoveryCandidateStatusIgnored, ProcessingState: DiscoveryProcessingDiscovered,
		EligibilityState: DiscoveryEligibilityUnknown, LastSeenAt: clock.Now(),
	}).Error; err != nil {
		t.Fatal(err)
	}
	jobs := &recordedJobs{keys: make(map[string]struct{})}
	queue := recordingJobEnqueuer{store: jobs}
	if _, err := ScanBlogroll(context.Background(), seed.ID, queue); err != nil {
		t.Fatal(err)
	}

	var sites []DiscoverySite
	if err := db.Order("id").Find(&sites).Error; err != nil {
		t.Fatal(err)
	}
	if len(sites) != 3 {
		t.Fatalf("sites after A scan = %#v", sites)
	}
	byHost := make(map[string]DiscoverySite)
	for _, site := range sites {
		byHost[site.HostKey] = site
	}
	_, bKey, _ := canonicalLegacySite(world.B.URL)
	_, cKey, _ := canonicalLegacySite(world.C.URL)
	b := byHost[bKey]
	c := byHost[cKey]
	if b.Status != DiscoverySiteStatusObserving || b.OperationalPause != "" || c.Status != DiscoverySiteStatusObserving {
		t.Fatalf("observing sites B=%#v C=%#v", b, c)
	}
	var bEndpoint DiscoverySource
	if err := db.Where("site_id = ? AND endpoint_type = ?", b.ID, DiscoveryEndpointHomepage).First(&bEndpoint).Error; err != nil {
		t.Fatal(err)
	}
	if bEndpoint.NextDueAt == nil || !bEndpoint.NextDueAt.Equal(clock.Now()) {
		t.Fatalf("B endpoint due = %#v", bEndpoint.NextDueAt)
	}
	if _, ok := jobs.keys[fmt.Sprintf("fetch:%d", bEndpoint.ID)]; !ok {
		t.Fatalf("B endpoint fetch was not scheduled: %#v", jobs.keys)
	}
	if _, ok := jobs.keys[fmt.Sprintf("blogroll:%d", b.ID)]; ok {
		t.Fatalf("B graph scan bypassed homepage verification: %#v", jobs.keys)
	}

	if _, err := FetchDiscoverySource(context.Background(), &bEndpoint); err != nil {
		t.Fatal(err)
	}
	if err := db.First(&b, b.ID).Error; err != nil || b.Status != DiscoverySiteStatusActive {
		t.Fatalf("B homepage verification = %#v, %v", b, err)
	}
	if _, err := ScanBlogroll(context.Background(), b.ID, queue); err != nil {
		t.Fatal(err)
	}
	cRoot, err := world.Fetcher.Fetch(context.Background(), FetchRequest{URL: world.C.URL + "/", Kind: FetchKindHTML})
	if err != nil {
		t.Fatal(err)
	}
	cRootDiscovery, err := (BlogrollDiscoverer{}).Discover(cRoot.Body, world.C.URL+"/", world.C.URL+"/")
	if err != nil {
		t.Fatal(err)
	}
	if len(cRootDiscovery.Links) != 0 {
		t.Fatalf("ordinary C homepage links were classified as blogroll: %#v", cRootDiscovery.Links)
	}
	var cEndpoint DiscoverySource
	if err := db.Where("site_id = ? AND endpoint_type = ?", c.ID, DiscoveryEndpointHomepage).First(&cEndpoint).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := FetchDiscoverySource(context.Background(), &cEndpoint); err != nil {
		t.Fatal(err)
	}
	if err := db.First(&c, c.ID).Error; err != nil || c.Status != DiscoverySiteStatusActive {
		t.Fatalf("C homepage verification = %#v, %v", c, err)
	}
	if _, err := ScanBlogroll(context.Background(), c.ID, queue); err != nil {
		t.Fatal(err)
	}
	if _, err := ScanBlogroll(context.Background(), seed.ID, queue); err != nil {
		t.Fatal(err)
	}
	if err := db.Find(&sites).Error; err != nil || len(sites) != 3 {
		t.Fatalf("cycle sites = %d, %v", len(sites), err)
	}
	var edgeCount int64
	if err := db.Model(&DiscoverySiteEdge{}).Count(&edgeCount).Error; err != nil || edgeCount != 5 {
		t.Fatalf("cycle edge count = %d, %v", edgeCount, err)
	}
	var ordinaryEdges int64
	if err := db.Model(&DiscoverySiteEdge{}).Where("source_page_url = ?", world.C.URL+"/").Count(&ordinaryEdges).Error; err != nil || ordinaryEdges != 0 {
		t.Fatalf("ordinary C home edges = %d, %v", ordinaryEdges, err)
	}

	view, err := GetSiteGraph(b.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(view.ShortestSeedPath.Sites) != 2 || view.ShortestSeedPath.Sites[0].ID != seed.ID || view.ShortestSeedPath.Sites[1].ID != b.ID {
		t.Fatalf("B shortest path = %#v", view.ShortestSeedPath)
	}
	if len(view.ShortestSeedPath.Edges) != 1 || !strings.Contains(view.ShortestSeedPath.Edges[0].SourcePageURL, "links.html") || view.ShortestSeedPath.Edges[0].DetectionRule == "" {
		t.Fatalf("B path evidence = %#v", view.ShortestSeedPath.Edges)
	}
}

func TestGraphLimitsRetainPendingTargets(t *testing.T) {
	tests := []struct {
		name        string
		fromDepth   int
		maxDepth    int
		perSite     int
		daily       int
		wantPending string
	}{
		{name: "depth", fromDepth: 3, maxDepth: 3, perSite: 50, daily: 100, wantPending: DiscoveryPauseDepthLimit},
		{name: "per_site", fromDepth: 0, maxDepth: 3, perSite: 1, daily: 100, wantPending: DiscoveryPausePerSiteLimit},
		{name: "daily", fromDepth: 0, maxDepth: 3, perSite: 50, daily: 1, wantPending: DiscoveryPauseDailyLimit},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			setupSQLiteDB(t)
			oldDepth := config.DISCOVERYMAXGRAPHDEPTH
			oldPerSite := config.DISCOVERYMAXBLOGROLLTARGETS
			oldDaily := config.DISCOVERYDAILYOBSERVINGLIMIT
			config.DISCOVERYMAXGRAPHDEPTH = testCase.maxDepth
			config.DISCOVERYMAXBLOGROLLTARGETS = testCase.perSite
			config.DISCOVERYDAILYOBSERVINGLIMIT = testCase.daily
			t.Cleanup(func() {
				config.DISCOVERYMAXGRAPHDEPTH = oldDepth
				config.DISCOVERYMAXBLOGROLLTARGETS = oldPerSite
				config.DISCOVERYDAILYOBSERVINGLIMIT = oldDaily
			})
			now := time.Date(2026, 7, 13, 14, 0, 0, 0, time.UTC)
			from := DiscoverySite{RootURL: "https://seed.example/", HostKey: "seed.example", Status: DiscoverySiteStatusSeed, DiscoveryMethod: "test", GraphDepth: testCase.fromDepth, CrawlAllowed: true, RobotsStatus: "unknown", FirstDiscoveredAt: now, CreatedAt: now}
			if err := db.Create(&from).Error; err != nil {
				t.Fatal(err)
			}
			links := []BlogrollLink{{TargetURL: "https://b.example/", SourcePageURL: from.RootURL + "links", DetectionRule: "explicit_rel", RelationType: "friend", Confidence: .9}}
			if testCase.wantPending != DiscoveryPauseDepthLimit {
				links = append(links, BlogrollLink{TargetURL: "https://c.example/", SourcePageURL: from.RootURL + "links", DetectionRule: "explicit_rel", RelationType: "friend", Confidence: .8})
			}
			clock := &advancingClock{now: now}
			service := SiteGraphService{Clock: clock, Classifier: SiteClassifier{}}
			result, err := service.ApplyLinks(context.Background(), from, links)
			if err != nil {
				t.Fatal(err)
			}
			if result.TargetsPending == 0 {
				t.Fatalf("result = %#v", result)
			}
			var pending DiscoverySiteEdge
			if err := db.Where("pending_reason = ?", testCase.wantPending).First(&pending).Error; err != nil {
				t.Fatalf("pending edge not retained: %v", err)
			}
			var target DiscoverySite
			if err := db.First(&target, pending.ToSiteID).Error; err != nil || target.OperationalPause != testCase.wantPending {
				t.Fatalf("pending target = %#v, %v", target, err)
			}
			if testCase.wantPending == DiscoveryPausePerSiteLimit || testCase.wantPending == DiscoveryPauseDailyLimit {
				if testCase.wantPending == DiscoveryPauseDailyLimit {
					clock.Advance(24 * time.Hour)
				}
				if _, err := service.ApplyLinks(context.Background(), from, links); err != nil {
					t.Fatal(err)
				}
				if err := db.First(&target, target.ID).Error; err != nil || target.OperationalPause != "" || !target.CrawlAllowed {
					t.Fatalf("pending target was not resumed = %#v, %v", target, err)
				}
			}
		})
	}
}
