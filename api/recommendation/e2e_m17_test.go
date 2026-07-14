package recommendation

import (
	"DataArk/discovery"
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

// TestEndToEndM17UserDigestAndLongTailClosure is the database-backed half of
// the M17 acceptance suite. The discovery half uses the deterministic local
// A/B/C HTTP world in discovery tests; this test carries its graph, provenance,
// article, user, immutable digest, fallback, and product-metric evidence through
// one shared SQLite database without an LLM, vector service, or external URL.
func TestEndToEndM17UserDigestAndLongTailClosure(t *testing.T) {
	setupSQLiteDB(t)
	clock := useRecommendationTestClock(t, time.Date(2026, 7, 14, 6, 0, 0, 0, time.UTC))

	sites := createM17SiteCycle(t, clock.Now())
	sources := createM17Sources(t, sites)
	graph, err := discovery.GetSiteGraph(sites[1].ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(graph.ShortestSeedPath.Sites) != 2 || graph.ShortestSeedPath.Sites[0].ID != sites[0].ID || graph.ShortestSeedPath.Sites[1].ID != sites[1].ID {
		t.Fatalf("A -> B shortest seed path = %#v", graph.ShortestSeedPath)
	}
	var edgeCount int64
	if err := db.Model(&discovery.DiscoverySiteEdge{}).Count(&edgeCount).Error; err != nil || edgeCount != 3 {
		t.Fatalf("bounded A -> B -> C -> A edge count = %d, err=%v", edgeCount, err)
	}

	// B deliberately has a low eligible hit rate. Three articles initially pass
	// article-level assessment, while the other 47 remain explicit ineligible
	// records. Two later become eligible to prove new B gems survive per-article
	// dislike and per-user source blocking while the site stays at 10%.
	bCandidates := make([]DiscoveryCandidate, 0, 50)
	for index := 0; index < 50; index++ {
		quality := 0.2
		if index == 0 {
			quality = 0.56 // ordinary but valid; used for article-only feedback.
		}
		if index == 1 {
			quality = 0.99 // historical Sitemap gem.
		}
		if index == 2 {
			quality = 0.98 // archive gem.
		}
		candidate := createReadyCandidate(t, fmt.Sprintf("https://b.example/articles/%02d", index), fmt.Sprintf("B article %02d", index), []string{"systems"}, fmt.Sprintf("b-%02d", index), quality, quality)
		if err := db.Model(&candidate).Updates(map[string]interface{}{"source_id": sources[1].ID, "source_name": sources[1].Name, "content_version": 1}).Error; err != nil {
			t.Fatal(err)
		}
		candidate.SourceID, candidate.SourceName, candidate.ContentVersion = sources[1].ID, sources[1].Name, 1
		if index > 2 {
			if err := db.Model(&candidate).Updates(map[string]interface{}{"eligibility_state": discovery.DiscoveryEligibilityIneligible, "eligibility_reasons": "article_quality_below_threshold"}).Error; err != nil {
				t.Fatal(err)
			}
			candidate.EligibilityState = discovery.DiscoveryEligibilityIneligible
		}
		method := "feed"
		if index == 1 {
			method = "sitemap_backfill"
		} else if index == 2 {
			method = "archive_backfill"
		}
		createM17Provenance(t, sites[1], sources[1], candidate, method, fmt.Sprintf("b-%02d-primary", index), clock.Now())
		bCandidates = append(bCandidates, candidate)
	}
	// The same article has three entry records but one logical candidate.
	createM17Provenance(t, sites[1], sources[1], bCandidates[1], "feed", "b-gem-feed", clock.Now())
	createM17Provenance(t, sites[1], sources[1], bCandidates[1], "archive", "b-gem-archive", clock.Now())
	var gemProvenance int64
	if err := db.Model(&discovery.DiscoveryCandidateProvenance{}).Where("candidate_id = ?", bCandidates[1].ID).Count(&gemProvenance).Error; err != nil || gemProvenance != 3 {
		t.Fatalf("single representative provenance count = %d, err=%v", gemProvenance, err)
	}

	// Seven independent eligible candidates make the initial target exactly 10.
	for index := 0; index < 7; index++ {
		candidate := createReadyCandidate(t, fmt.Sprintf("https://a.example/articles/%02d", index), fmt.Sprintf("A article %02d", index), []string{fmt.Sprintf("topic-%d", index)}, fmt.Sprintf("a-%02d", index), 0.8-float64(index)*0.01, 0.7)
		if err := db.Model(&candidate).Updates(map[string]interface{}{"source_id": sources[0].ID, "source_name": sources[0].Name, "content_version": 1}).Error; err != nil {
			t.Fatal(err)
		}
		createM17Provenance(t, sites[0], sources[0], candidate, "feed", fmt.Sprintf("a-%02d", index), clock.Now())
	}

	for _, value := range []RecommendationSettings{
		m17Settings(1701, "Asia/Shanghai", 10),
		m17Settings(1702, "America/Los_Angeles", 10),
		m17Settings(1703, "UTC", 10),
	} {
		settings := value
		if _, err := SaveRecommendationSettings(&settings); err != nil {
			t.Fatal(err)
		}
	}
	dateAtBoundary := time.Date(2026, 7, 14, 1, 0, 0, 0, time.UTC)
	shanghaiDate, err := RecommendationDateForUser(1701, dateAtBoundary)
	if err != nil {
		t.Fatal(err)
	}
	losAngelesDate, err := RecommendationDateForUser(1702, dateAtBoundary)
	if err != nil {
		t.Fatal(err)
	}
	if shanghaiDate != "2026-07-14" || losAngelesDate != "2026-07-13" {
		t.Fatalf("local dates = Shanghai %s, Los Angeles %s", shanghaiDate, losAngelesDate)
	}

	userOneDay, err := GenerateDailyRecommendations(context.Background(), 1701, "2026-07-14")
	if err != nil {
		t.Fatal(err)
	}
	if userOneDay.Day.RequestedCount != 10 || userOneDay.Day.ActualCount != 10 || len(userOneDay.Items) != 10 {
		t.Fatalf("full digest = %#v", userOneDay.Day)
	}
	explorationItems := 0
	for _, item := range userOneDay.Items {
		if item.PoolType == "exploration" {
			explorationItems++
		}
	}
	if explorationItems < 2 {
		t.Fatalf("exploration items = %d, want ceil(10*0.15)=2", explorationItems)
	}
	ordinaryItem := m17ItemForCandidate(t, userOneDay, bCandidates[0].ID)
	immutableItem := m17ItemForCandidate(t, userOneDay, bCandidates[1].ID)
	if _, _, err := RecordRecommendationFeedback(1701, ordinaryItem.ID, RecommendationFeedbackNotInterested, nil); err != nil {
		t.Fatal(err)
	}
	var sharedOrdinary DiscoveryCandidate
	if err := db.First(&sharedOrdinary, bCandidates[0].ID).Error; err != nil || sharedOrdinary.Status != DiscoveryCandidateStatusNew {
		t.Fatalf("article feedback mutated shared candidate = %#v, err=%v", sharedOrdinary, err)
	}

	userTwoDay, err := GenerateDailyRecommendations(context.Background(), 1702, "2026-07-13")
	if err != nil {
		t.Fatal(err)
	}
	if len(userTwoDay.Items) != 10 {
		t.Fatalf("U2 inherited U1 state: %d items", len(userTwoDay.Items))
	}
	m17ItemForCandidate(t, userTwoDay, bCandidates[0].ID)

	// A newly qualified B article is still recommended to U1 after U1 disliked a
	// different B article.
	m17MakeEligible(t, &bCandidates[3], 0.97)
	clock.Advance(24 * time.Hour)
	userOneSecond, err := GenerateDailyRecommendations(context.Background(), 1701, "2026-07-15")
	if err != nil {
		t.Fatal(err)
	}
	if len(userOneSecond.Items) != 1 || userOneSecond.Items[0].CandidateID != bCandidates[3].ID {
		t.Fatalf("article-only dislike suppressed later B gem: %#v", userOneSecond.Items)
	}

	// Only an explicit source block excludes later B articles, and only for U1.
	clock.Advance(time.Hour)
	if _, rules, err := RecordRecommendationFeedback(1701, userOneSecond.Items[0].ID, RecommendationFeedbackBlockSource, []RecommendationBlockTarget{{Type: UserBlockRuleSource, Value: "b.example"}}); err != nil || len(rules) != 1 {
		t.Fatalf("explicit source block rules=%#v err=%v", rules, err)
	}
	m17MakeEligible(t, &bCandidates[4], 0.96)
	createM17Provenance(t, sites[1], sources[1], bCandidates[4], "archive_backfill", "b-later-gem-archive", clock.Now())
	clock.Advance(23 * time.Hour)
	userOneBlocked, err := GenerateDailyRecommendations(context.Background(), 1701, "2026-07-16")
	if err != nil {
		t.Fatal(err)
	}
	if len(userOneBlocked.Items) != 0 || userOneBlocked.Day.ActualCount != 0 {
		t.Fatalf("U1 source block leaked B items: %#v", userOneBlocked.Items)
	}
	if userOneBlocked.Day.ShortageReasons == "" {
		t.Fatal("candidate shortage did not retain an explanation")
	}
	userTwoSecond, err := GenerateDailyRecommendations(context.Background(), 1702, "2026-07-14")
	if err != nil {
		t.Fatal(err)
	}
	positiveItem := m17ItemForCandidate(t, userTwoSecond, bCandidates[4].ID)
	if _, _, err := RecordRecommendationFeedback(1702, positiveItem.ID, RecommendationFeedbackValuable, nil); err != nil {
		t.Fatal(err)
	}

	// Published display values survive candidate mutation and a compatibility
	// retry. A separate user proves a failed optional reranker still publishes.
	originalTitle := immutableItem.SnapshotTitle
	if err := db.Model(&DiscoveryCandidate{}).Where("id = ?", immutableItem.CandidateID).Updates(map[string]interface{}{"title": "mutated after publication", "eligibility_state": discovery.DiscoveryEligibilityIneligible}).Error; err != nil {
		t.Fatal(err)
	}
	retried, err := RegenerateDailyRecommendations(context.Background(), 1701, "2026-07-14")
	if err != nil {
		t.Fatal(err)
	}
	retriedItem := m17ItemForCandidate(t, retried, immutableItem.CandidateID)
	if retriedItem.SnapshotTitle != originalTitle || retriedItem.Candidate.Title != originalTitle || len(retried.Items) != len(userOneDay.Items) {
		t.Fatalf("published snapshot changed after retry: %#v", retriedItem)
	}
	fallback, err := GenerateDailyRecommendationsWithReranker(context.Background(), 1703, "2026-07-16", fakeReranker{err: errors.New("fixture model unavailable")})
	if err != nil {
		t.Fatal(err)
	}
	if fallback.Day.Status != RecommendationDayStatusPublished || !fallback.Day.Degraded || len(fallback.Items) == 0 {
		t.Fatalf("rule fallback digest = %#v", fallback)
	}

	metrics, err := GetAdminProductMetrics(clock.Now())
	if err != nil {
		t.Fatal(err)
	}
	if metrics.LongTail.GemContribution.Rate <= 0 || metrics.LongTail.ContributingSites != 1 || metrics.LongTail.BlogrollPositiveArticles == 0 || metrics.LongTail.BackfillPositiveArticles == 0 {
		t.Fatalf("long-tail closure metrics = %#v", metrics.LongTail)
	}
	if metrics.Integrity.HardFilterViolations != 0 || metrics.Integrity.DuplicateClusterViolations != 0 || metrics.Integrity.ActiveBlockViolations != 0 {
		t.Fatalf("published integrity metrics = %#v", metrics.Integrity)
	}
}

func createM17SiteCycle(t *testing.T, now time.Time) []discovery.DiscoverySite {
	t.Helper()
	sites := []discovery.DiscoverySite{
		{RootURL: "https://a.example", HostKey: "a.example", DisplayName: "A", Status: discovery.DiscoverySiteStatusSeed, DiscoveryMethod: "manual", GraphDepth: 0, CrawlAllowed: true, FirstDiscoveredAt: now},
		{RootURL: "https://b.example", HostKey: "b.example", DisplayName: "B", Status: discovery.DiscoverySiteStatusObserving, DiscoveryMethod: "blogroll", GraphDepth: 1, CrawlAllowed: true, FirstDiscoveredAt: now.Add(-48 * time.Hour)},
		{RootURL: "https://c.example", HostKey: "c.example", DisplayName: "C", Status: discovery.DiscoverySiteStatusObserving, DiscoveryMethod: "blogroll", GraphDepth: 2, CrawlAllowed: true, FirstDiscoveredAt: now},
	}
	for index := range sites {
		if err := db.Create(&sites[index]).Error; err != nil {
			t.Fatal(err)
		}
	}
	edges := []discovery.DiscoverySiteEdge{
		{EdgeKey: "a-b", FromSiteID: sites[0].ID, ToSiteID: sites[1].ID, SourcePageURL: sites[0].RootURL + "/links", RelationType: "blogroll", DetectionRule: "explicit", FirstSeenAt: now, LastSeenAt: now, Active: true, GraphDepth: 1},
		{EdgeKey: "b-c", FromSiteID: sites[1].ID, ToSiteID: sites[2].ID, SourcePageURL: sites[1].RootURL + "/links", RelationType: "blogroll", DetectionRule: "explicit", FirstSeenAt: now, LastSeenAt: now, Active: true, GraphDepth: 2},
		{EdgeKey: "c-a", FromSiteID: sites[2].ID, ToSiteID: sites[0].ID, SourcePageURL: sites[2].RootURL + "/links", RelationType: "blogroll", DetectionRule: "explicit", FirstSeenAt: now, LastSeenAt: now, Active: true, GraphDepth: 0},
	}
	if err := db.Create(&edges).Error; err != nil {
		t.Fatal(err)
	}
	return sites
}

func createM17Sources(t *testing.T, sites []discovery.DiscoverySite) []discovery.DiscoverySource {
	t.Helper()
	sources := make([]discovery.DiscoverySource, len(sites))
	for index := range sites {
		sources[index] = discovery.DiscoverySource{Name: sites[index].DisplayName + " Feed", URL: sites[index].RootURL + "/feed.xml", Type: "feed", EndpointType: "feed", SiteID: &sites[index].ID, Enabled: true}
		if err := db.Create(&sources[index]).Error; err != nil {
			t.Fatal(err)
		}
	}
	return sources
}

func createM17Provenance(t *testing.T, site discovery.DiscoverySite, source discovery.DiscoverySource, candidate DiscoveryCandidate, method string, key string, now time.Time) {
	t.Helper()
	row := discovery.DiscoveryCandidateProvenance{ProvenanceKey: key, CandidateID: candidate.ID, SiteID: site.ID, SourceID: &source.ID, DiscoveryMethod: method, OriginalURL: candidate.URL, SourcePageURL: source.URL, FirstSeenAt: now, LastSeenAt: now}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
}

func m17Settings(userID uint, timezone string, limit int) RecommendationSettings {
	settings := DefaultRecommendationSettings(userID)
	settings.Timezone, settings.DailyLimit, settings.ExplorationRate, settings.Enabled = timezone, limit, 0.15, true
	return settings
}

func m17MakeEligible(t *testing.T, candidate *DiscoveryCandidate, quality float64) {
	t.Helper()
	if err := db.Model(&DiscoveryCandidate{}).Where("id = ?", candidate.ID).Updates(map[string]interface{}{"eligibility_state": discovery.DiscoveryEligibilityEligible, "eligibility_reasons": "", "quality_score": quality, "depth_score": quality}).Error; err != nil {
		t.Fatal(err)
	}
	candidate.EligibilityState, candidate.QualityScore, candidate.DepthScore = discovery.DiscoveryEligibilityEligible, quality, quality
}

func m17ItemForCandidate(t *testing.T, snapshot *RecommendationDaySnapshot, candidateID uint) RecommendationItem {
	t.Helper()
	for _, item := range snapshot.Items {
		if item.CandidateID == candidateID {
			return item
		}
	}
	t.Fatalf("candidate %d missing from digest %#v", candidateID, snapshot.Day)
	return RecommendationItem{}
}
