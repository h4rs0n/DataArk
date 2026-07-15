package discovery

import (
	"testing"
	"time"
)

func TestDomainIdentityUsesRegistrableDomainAndKeepsLocalPorts(t *testing.T) {
	tests := map[string]string{
		"https://blog.example.com/posts/1":       "example.com",
		"https://www.example.com/":               "example.com",
		"https://feed.example.co.uk/rss":         "example.co.uk",
		"https://notes.github.io/":               "notes.github.io",
		"http://127.0.0.1:41001/":                "127.0.0.1:41001",
		"http://127.0.0.1:41002/":                "127.0.0.1:41002",
		"http://localhost:8080/":                 "localhost:8080",
		"https://LOCALHOST:443/ignored-default/": "localhost",
	}
	for rawURL, want := range tests {
		got, err := domainKeyForURL(rawURL)
		if err != nil {
			t.Fatalf("%s: %v", rawURL, err)
		}
		if got != want {
			t.Fatalf("%s domain key = %q, want %q", rawURL, got, want)
		}
	}
}

func TestDiscoverySourcesReuseAndListOneRegistrableDomain(t *testing.T) {
	setupSQLiteDB(t)
	first, err := CreateDiscoverySource("Example Feed", "https://blog.example.com/feed.xml", DiscoverySourceTypeFeed, true)
	if err != nil {
		t.Fatal(err)
	}
	second, err := CreateDiscoverySource("Example Atom", "https://www.example.com/atom.xml", DiscoverySourceTypeFeed, true)
	if err != nil {
		t.Fatal(err)
	}
	if first.SiteID == nil || second.SiteID == nil || *first.SiteID != *second.SiteID {
		t.Fatalf("same registrable domain used different sites: first=%#v second=%#v", first.SiteID, second.SiteID)
	}
	if _, err := CreateDiscoverySource("Other", "https://other.example.net/feed.xml", DiscoverySourceTypeFeed, true); err != nil {
		t.Fatal(err)
	}

	var endpointCount int64
	if err := db.Model(&DiscoverySource{}).Count(&endpointCount).Error; err != nil {
		t.Fatal(err)
	}
	if endpointCount != 5 {
		t.Fatalf("physical endpoint count = %d, want 5", endpointCount)
	}
	listed, err := ListDiscoverySources()
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 2 {
		t.Fatalf("visible sites = %#v", listed)
	}
	if listed[0].ID != first.ID && listed[1].ID != first.ID {
		t.Fatalf("earliest user endpoint is not the domain representative: %#v", listed)
	}
}

func TestDomainBackfillGroupsExistingDuplicateSitesWithoutDeletingEndpoints(t *testing.T) {
	setupSQLiteDB(t)
	now := time.Date(2026, 7, 15, 8, 0, 0, 0, time.UTC)
	sites := []DiscoverySite{
		{RootURL: "https://blog.example.com/", HostKey: "blog.example.com", Status: DiscoverySiteStatusObserving, DiscoveryMethod: DiscoveryMethodBlogroll, CrawlAllowed: true, FirstDiscoveredAt: now},
		{RootURL: "https://feed.example.com/", HostKey: "feed.example.com", Status: DiscoverySiteStatusObserving, DiscoveryMethod: DiscoveryMethodBlogroll, CrawlAllowed: true, FirstDiscoveredAt: now},
	}
	for index := range sites {
		if err := db.Create(&sites[index]).Error; err != nil {
			t.Fatal(err)
		}
		source := DiscoverySource{Name: sites[index].HostKey, URL: sites[index].RootURL + "feed.xml", Type: DiscoverySourceTypeFeed, EndpointType: DiscoveryEndpointFeed, SiteID: &sites[index].ID, Enabled: true, CreatedAt: now.Add(time.Duration(index) * time.Minute)}
		if err := db.Create(&source).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := BackfillV3Compatibility(db); err != nil {
		t.Fatal(err)
	}
	if err := BackfillV3Compatibility(db); err != nil {
		t.Fatal(err)
	}
	var storedSites []DiscoverySite
	if err := db.Order("id").Find(&storedSites).Error; err != nil {
		t.Fatal(err)
	}
	if len(storedSites) != 2 || storedSites[0].DomainKey != "example.com" || storedSites[1].DomainKey != "example.com" {
		t.Fatalf("non-destructive domain backfill = %#v", storedSites)
	}
	var endpoints []DiscoverySource
	if err := db.Order("id").Find(&endpoints).Error; err != nil {
		t.Fatal(err)
	}
	if len(endpoints) != 2 || endpoints[0].SiteID == nil || endpoints[1].SiteID == nil || *endpoints[0].SiteID != *endpoints[1].SiteID {
		t.Fatalf("endpoint reassignment = %#v", endpoints)
	}
	listed, err := ListDiscoverySources()
	if err != nil || len(listed) != 1 {
		t.Fatalf("visible grouped sources = %#v, %v", listed, err)
	}
}
