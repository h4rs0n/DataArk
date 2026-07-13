package discovery

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestResolveCandidateDuplicatesKeepsOneRepresentativeAndAllProvenance(t *testing.T) {
	setupSQLiteDB(t)
	now := time.Date(2026, 7, 14, 1, 0, 0, 0, time.UTC)
	body := strings.Repeat("durable evidence explains the original observation and reproducible conclusion ", 12)

	mirror := createDedupeCandidate(t, "https://mirror.example/post", "https://mirror.example/post", "https://author.example/post", body, now)
	redirect := createDedupeCandidate(t, "https://short.example/r", "https://author.example/post", "https://author.example/post", body, now)
	if err := ResolveCandidateDuplicates(context.Background(), mirror.ID); err != nil {
		t.Fatal(err)
	}
	if err := ResolveCandidateDuplicates(context.Background(), redirect.ID); err != nil {
		t.Fatal(err)
	}
	var interim DiscoveryCandidate
	if err := db.Where("id IN ? AND representative_id = id", []uint{mirror.ID, redirect.ID}).First(&interim).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE TABLE recommendation_items_m8 (id INTEGER PRIMARY KEY, candidate_id INTEGER NOT NULL, snapshot_title TEXT NOT NULL)`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO recommendation_items_m8(id, candidate_id, snapshot_title) VALUES(1, ?, ?)`, interim.ID, "Published snapshot").Error; err != nil {
		t.Fatal(err)
	}

	original := createDedupeCandidate(t, "https://author.example/post", "https://author.example/post", "https://author.example/post", body, now)
	if err := ResolveCandidateDuplicates(context.Background(), original.ID); err != nil {
		t.Fatal(err)
	}

	var members []DiscoveryCandidate
	if err := db.Where("id IN ?", []uint{mirror.ID, redirect.ID, original.ID}).Order("id").Find(&members).Error; err != nil {
		t.Fatal(err)
	}
	if len(members) != 3 {
		t.Fatalf("members = %#v", members)
	}
	clusterID := members[0].DuplicateClusterID
	for _, member := range members {
		if member.DuplicateClusterID != clusterID || member.DedupeKey != clusterID || member.RepresentativeID == nil || *member.RepresentativeID != original.ID || member.DedupeState != DiscoveryDedupeReady {
			t.Fatalf("cluster member = %#v", member)
		}
		if member.ID != original.ID && (member.EligibilityState != DiscoveryEligibilityIneligible || member.EligibilityReasons != "duplicate_non_representative") {
			t.Fatalf("non-representative eligibility = %#v", member)
		}
	}
	var cluster DiscoveryDuplicateCluster
	if err := db.First(&cluster, "cluster_id = ?", clusterID).Error; err != nil {
		t.Fatal(err)
	}
	if cluster.RepresentativeID != original.ID || cluster.MemberCount != 3 || cluster.MatchMethod != DuplicateMatchExact || !strings.Contains(cluster.RepresentativeReason, "canonical_origin") {
		t.Fatalf("cluster = %#v", cluster)
	}
	var provenanceCount int64
	if err := db.Model(&DiscoveryCandidateProvenance{}).Where("candidate_id = ?", original.ID).Count(&provenanceCount).Error; err != nil {
		t.Fatal(err)
	}
	if provenanceCount != 3 {
		t.Fatalf("representative provenance count = %d, want 3", provenanceCount)
	}
	var identityCount int64
	if err := db.Model(&DiscoveryCandidateIdentity{}).Where("candidate_id IN ?", []uint{mirror.ID, redirect.ID, original.ID}).Count(&identityCount).Error; err != nil {
		t.Fatal(err)
	}
	if identityCount < 12 {
		t.Fatalf("identity count = %d", identityCount)
	}
	var historyCandidateID uint
	var snapshotTitle string
	if err := db.Raw(`SELECT candidate_id, snapshot_title FROM recommendation_items_m8 WHERE id = 1`).Row().Scan(&historyCandidateID, &snapshotTitle); err != nil {
		t.Fatal(err)
	}
	if historyCandidateID != interim.ID || snapshotTitle != "Published snapshot" {
		t.Fatalf("history changed to candidate=%d title=%q", historyCandidateID, snapshotTitle)
	}
}

func TestResolveCandidateDuplicatesClustersNearBodiesButNotDistinctArticles(t *testing.T) {
	setupSQLiteDB(t)
	now := time.Date(2026, 7, 14, 2, 0, 0, 0, time.UTC)
	baseWords := []string{"observed", "latency", "during", "deployment", "and", "collected", "traces", "from", "three", "regions", "before", "testing", "cache", "pressure", "with", "controlled", "requests", "the", "results", "showed", "a", "repeatable", "queue", "interaction", "that", "explains", "the", "failure", "and", "supports", "the", "recommended", "mitigation", "through", "independent", "measurements", "and", "counterexamples", "recorded", "for", "future", "operators"}
	firstBody := strings.Join(baseWords, " ")
	secondWords := append([]string(nil), baseWords...)
	secondWords[3] = "rollout"
	secondWords[28] = "validates"
	secondBody := strings.Join(secondWords, " ")
	distinctBody := strings.Repeat("gardening soil sunlight watering seasonal flowers compost biodiversity pollinators ", 8)

	first := createDedupeCandidate(t, "https://one.example/post", "https://one.example/post", "https://one.example/post", firstBody, now)
	second := createDedupeCandidate(t, "https://two.example/repost", "https://two.example/repost", "https://two.example/repost", secondBody, now)
	distinct := createDedupeCandidate(t, "https://three.example/garden", "https://three.example/garden", "https://three.example/garden", distinctBody, now)
	for _, candidate := range []DiscoveryCandidate{first, second, distinct} {
		if err := ResolveCandidateDuplicates(context.Background(), candidate.ID); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.First(&first, first.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.First(&second, second.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.First(&distinct, distinct.ID).Error; err != nil {
		t.Fatal(err)
	}
	if first.ContentHash == second.ContentHash || first.DuplicateClusterID != second.DuplicateClusterID {
		t.Fatalf("near duplicate clusters = %q/%q hashes=%q/%q", first.DuplicateClusterID, second.DuplicateClusterID, first.ContentHash, second.ContentHash)
	}
	if distinct.DuplicateClusterID == first.DuplicateClusterID {
		t.Fatalf("distinct article joined near cluster %q", distinct.DuplicateClusterID)
	}
	var cluster DiscoveryDuplicateCluster
	if err := db.First(&cluster, "cluster_id = ?", first.DuplicateClusterID).Error; err != nil {
		t.Fatal(err)
	}
	if cluster.MatchMethod != DuplicateMatchNear || cluster.MemberCount != 2 {
		t.Fatalf("near cluster = %#v", cluster)
	}
}

func TestUpsertTrackingAliasesKeepsOneCandidateAndBothDiscoveryPaths(t *testing.T) {
	setupSQLiteDB(t)
	now := time.Date(2026, 7, 14, 3, 0, 0, 0, time.UTC)
	site := DiscoverySite{RootURL: "https://aliases.example/", HostKey: "aliases.example", Status: DiscoverySiteStatusSeed, DiscoveryMethod: "fixture", CrawlAllowed: true, FirstDiscoveredAt: now}
	if err := db.Create(&site).Error; err != nil {
		t.Fatal(err)
	}
	source := DiscoverySource{Name: "Aliases", URL: "https://aliases.example/feed.xml", Type: DiscoverySourceTypeFeed, SiteID: &site.ID, EndpointType: DiscoveryEndpointFeed, Enabled: true}
	if err := db.Create(&source).Error; err != nil {
		t.Fatal(err)
	}
	first, err := upsertDiscoveryCandidate(source, discoveredCandidate{URL: "https://aliases.example/post?id=42&utm_source=feed", DiscoveryMethod: DiscoveryMethodFeed, SourcePageURL: source.URL})
	if err != nil {
		t.Fatal(err)
	}
	second, err := upsertDiscoveryCandidate(source, discoveredCandidate{URL: "https://aliases.example/post?fbclid=click&id=42", DiscoveryMethod: DiscoveryMethodHomepageLink, SourcePageURL: site.RootURL})
	if err != nil {
		t.Fatal(err)
	}
	if !first.Created || second.Created || first.Candidate.ID != second.Candidate.ID {
		t.Fatalf("alias writes first=%#v second=%#v", first, second)
	}
	var candidateCount, provenanceCount int64
	if err := db.Model(&DiscoveryCandidate{}).Count(&candidateCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&DiscoveryCandidateProvenance{}).Where("candidate_id = ?", first.Candidate.ID).Count(&provenanceCount).Error; err != nil {
		t.Fatal(err)
	}
	if candidateCount != 1 || provenanceCount != 2 {
		t.Fatalf("candidate/provenance counts = %d/%d", candidateCount, provenanceCount)
	}
}

func createDedupeCandidate(t *testing.T, rawURL string, finalURL string, canonicalURL string, body string, now time.Time) DiscoveryCandidate {
	t.Helper()
	site := DiscoverySite{RootURL: rootForTestURL(rawURL), HostKey: fmt.Sprintf("site-%x", ContentHash(rawURL)[:8]), Status: DiscoverySiteStatusSeed, DiscoveryMethod: "fixture", CrawlAllowed: true, FirstDiscoveredAt: now}
	if err := db.Create(&site).Error; err != nil {
		t.Fatal(err)
	}
	source := DiscoverySource{Name: site.HostKey, URL: site.RootURL + "feed.xml", Type: DiscoverySourceTypeFeed, SiteID: &site.ID, EndpointType: DiscoveryEndpointFeed, Enabled: true}
	if err := db.Create(&source).Error; err != nil {
		t.Fatal(err)
	}
	normalized, err := NormalizeArticleURL(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	candidate := DiscoveryCandidate{
		SourceID: source.ID, SourceName: source.Name, URL: rawURL, NormalizedURL: normalized,
		FinalURL: finalURL, CanonicalURL: canonicalURL, Title: "A durable investigation",
		BodyText: body, Language: "en", WordCount: len(strings.Fields(body)), ContentHash: ContentHash(body),
		ContentVersion: 1, Status: DiscoveryCandidateStatusNew, ProcessingState: DiscoveryProcessingReady,
		DedupeState: DiscoveryDedupePending, AssessmentState: DiscoveryAssessmentPending,
		EligibilityState: DiscoveryEligibilityUnknown, LastSeenAt: now, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&candidate).Error; err != nil {
		t.Fatal(err)
	}
	provenance := DiscoveryCandidateProvenance{
		ProvenanceKey: fmt.Sprintf("fixture-%d", candidate.ID), CandidateID: candidate.ID,
		SiteID: site.ID, SourceID: &source.ID, DiscoveryMethod: DiscoveryMethodFeed,
		OriginalURL: rawURL, SourcePageURL: source.URL, FirstSeenAt: now, LastSeenAt: now,
	}
	if err := db.Create(&provenance).Error; err != nil {
		t.Fatal(err)
	}
	return candidate
}

func rootForTestURL(rawURL string) string {
	host := articleHost(rawURL)
	return "https://" + host + "/"
}
