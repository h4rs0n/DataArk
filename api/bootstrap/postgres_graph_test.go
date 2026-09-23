package bootstrap

import (
	"DataArk/discovery"
	"context"
	"testing"
	"time"

	"gorm.io/gorm"
)

// Called only after the opt-in migration test has prepared a disposable database.
func verifyPostgresConcurrentGraph(t *testing.T, database *gorm.DB) {
	t.Helper()
	now := time.Now().UTC()
	sources := []discovery.DiscoverySite{
		{RootURL: "https://graph-source-a-verify.invalid/", HostKey: "graph-source-a-verify.invalid", DomainKey: "graph-source-a-verify.invalid", Status: discovery.DiscoverySiteStatusSeed, DiscoveryMethod: discovery.DiscoveryMethodManualSeed, CrawlAllowed: true, FirstDiscoveredAt: now},
		{RootURL: "https://graph-source-b-verify.invalid/", HostKey: "graph-source-b-verify.invalid", DomainKey: "graph-source-b-verify.invalid", Status: discovery.DiscoverySiteStatusSeed, DiscoveryMethod: discovery.DiscoveryMethodManualSeed, CrawlAllowed: true, FirstDiscoveredAt: now},
	}
	targets := []discovery.DiscoverySite{
		{RootURL: "https://graph-target-a-verify.invalid/", HostKey: "graph-target-a-verify.invalid", DomainKey: "graph-target-a-verify.invalid", Status: discovery.DiscoverySiteStatusObserving, DiscoveryMethod: discovery.DiscoveryMethodBlogroll, CrawlAllowed: true, FirstDiscoveredAt: now},
		{RootURL: "https://graph-target-b-verify.invalid/", HostKey: "graph-target-b-verify.invalid", DomainKey: "graph-target-b-verify.invalid", Status: discovery.DiscoverySiteStatusObserving, DiscoveryMethod: discovery.DiscoveryMethodBlogroll, CrawlAllowed: true, FirstDiscoveredAt: now},
	}
	for index := range sources {
		if err := database.Create(&sources[index]).Error; err != nil {
			t.Fatal(err)
		}
	}
	for index := range targets {
		if err := database.Create(&targets[index]).Error; err != nil {
			t.Fatal(err)
		}
	}
	service := discovery.SiteGraphService{}
	for round := 0; round < 4; round++ {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		start := make(chan struct{})
		type outcome struct {
			result discovery.GraphApplyResult
			err    error
		}
		outcomes := make(chan outcome, len(sources))
		for index, source := range sources {
			links := []discovery.BlogrollLink{
				{TargetURL: targets[index].RootURL, SourcePageURL: source.RootURL + "links", DetectionRule: "explicit_rel", RelationType: "friend", Confidence: 0.9},
				{TargetURL: targets[1-index].RootURL, SourcePageURL: source.RootURL + "links", DetectionRule: "explicit_rel", RelationType: "friend", Confidence: 0.8},
			}
			go func() {
				<-start
				result, err := service.ApplyLinks(ctx, source, links)
				outcomes <- outcome{result: result, err: err}
			}()
		}
		close(start)
		for range sources {
			select {
			case got := <-outcomes:
				if got.err != nil || got.result.EdgesSeen != 2 || got.result.SitesCreated != 0 {
					t.Fatalf("concurrent graph round %d: result=%#v err=%v", round, got.result, got.err)
				}
			case <-ctx.Done():
				t.Fatalf("concurrent graph round %d timed out: %v", round, ctx.Err())
			}
		}
		cancel()
	}
	var edgeCount int64
	if err := database.Model(&discovery.DiscoverySiteEdge{}).
		Where("from_site_id IN ? AND to_site_id IN ?", []uint{sources[0].ID, sources[1].ID}, []uint{targets[0].ID, targets[1].ID}).
		Count(&edgeCount).Error; err != nil {
		t.Fatal(err)
	}
	if edgeCount != 4 {
		t.Fatalf("concurrent graph edge count = %d, want 4", edgeCount)
	}
}
