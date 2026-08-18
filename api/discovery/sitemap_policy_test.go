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
