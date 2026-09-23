package recommendation

import (
	"DataArk/assessment"
	"DataArk/discovery"
	"context"
	"testing"
	"time"
)

func TestObservabilityM16MetricsExplainOperationsAndLongTailGems(t *testing.T) {
	setupSQLiteDB(t)
	now := time.Date(2026, 6, 1, 8, 0, 0, 0, time.UTC)
	site := discovery.DiscoverySite{RootURL: "https://tail.example", HostKey: "tail.example", DisplayName: "Tail", Status: discovery.DiscoverySiteStatusObserving, DiscoveryMethod: "blogroll", GraphDepth: 2, CrawlAllowed: true, FirstDiscoveredAt: now}
	if err := db.Create(&site).Error; err != nil {
		t.Fatal(err)
	}
	source := discovery.DiscoverySource{Name: "Tail Feed", URL: "https://tail.example/feed", Type: "feed", SiteID: &site.ID, Enabled: true}
	if err := db.Create(&source).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&discovery.DiscoverySourceScheduleDecision{SourceID: source.ID, SiteID: &site.ID, Basis: "fixture", BaseIntervalSeconds: 3600, ChosenIntervalSeconds: 7200, NextDueAt: now, ComputedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&[]discovery.DiscoveryFetchRun{
		{SourceID: source.ID, SiteID: &site.ID, StartedAt: now, Status: "succeeded", NotModified: true},
		{SourceID: source.ID, SiteID: &site.ID, StartedAt: now, Status: "failed", RobotsStatus: "denied", ErrorCategory: "robots"},
	}).Error; err != nil {
		t.Fatal(err)
	}
	var gem DiscoveryCandidate
	for index := 0; index < 10; index++ {
		candidate := DiscoveryCandidate{SourceID: source.ID, SourceName: source.Name, URL: "https://tail.example/article-" + string(rune('a'+index)), Title: "Article", ProcessingState: discovery.DiscoveryProcessingReady, DedupeState: discovery.DiscoveryDedupeReady, EligibilityState: discovery.DiscoveryEligibilityIneligible, LastSeenAt: now}
		if index == 0 {
			candidate.EligibilityState, candidate.Topics = discovery.DiscoveryEligibilityEligible, `["systems"]`
		}
		if err := db.Create(&candidate).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&discovery.DiscoveryCandidateProvenance{ProvenanceKey: "metric-" + candidate.URL, CandidateID: candidate.ID, SiteID: site.ID, SourceID: &source.ID, DiscoveryMethod: "archive", OriginalURL: candidate.URL, FirstSeenAt: now, LastSeenAt: now}).Error; err != nil {
			t.Fatal(err)
		}
		if index == 0 {
			gem = candidate
		}
	}
	if err := db.Create(&[]assessment.ArticleAssessment{
		{CandidateID: gem.ID, ContentVersion: 1, Assessor: "deterministic_rules", AssessorVersion: "1", PolicyVersion: "v1", OverallQuality: .9},
		{CandidateID: gem.ID, ContentVersion: 1, Assessor: "model", AssessorVersion: "1", PolicyVersion: "v2", OverallQuality: .95},
	}).Error; err != nil {
		t.Fatal(err)
	}
	settings := DefaultRecommendationSettings(930)
	settings.DailyLimit, settings.ExplorationRate = 5, .2
	if _, err := SaveRecommendationSettings(&settings); err != nil {
		t.Fatal(err)
	}
	day, err := CreateRecommendationDay(930, "2026-06-01", 5)
	if err != nil {
		t.Fatal(err)
	}
	item, err := AddRecommendationItem(&RecommendationItem{DayID: uintPointer(day.ID), UserID: 930, CandidateID: gem.ID, Rank: 1, PoolType: "exploration", CreatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&RecommendationDay{}).Where("id = ?", day.ID).Updates(map[string]interface{}{"status": RecommendationDayStatusPublished, "actual_count": 1, "published_at": now, "shortage_reasons": `{"excluded":{"article_ineligible":9}}`}).Error; err != nil {
		t.Fatal(err)
	}
	if _, _, err := RecordRecommendationFeedback(930, item.ID, RecommendationFeedbackValuable, nil); err != nil {
		t.Fatal(err)
	}
	metrics, err := GetAdminProductMetrics(now)
	if err != nil {
		t.Fatal(err)
	}
	if metrics.Sites.ByStatus[discovery.DiscoverySiteStatusObserving] != 1 || metrics.Sites.ByDepth[2] != 1 || metrics.Sites.ScheduleFloorViolations != 1 {
		t.Fatalf("site metrics = %#v", metrics.Sites)
	}
	if metrics.Fetch.Attempts != 2 || metrics.Fetch.NotModified != 1 || metrics.Fetch.RobotsDenied != 1 {
		t.Fatalf("fetch metrics = %#v", metrics.Fetch)
	}
	if metrics.Candidates.Total != 10 || metrics.Candidates.Eligible != 1 || metrics.Assessments.ModelUsageRate.Rate != .5 {
		t.Fatalf("candidate metrics = %#v/%#v", metrics.Candidates, metrics.Assessments)
	}
	if metrics.Digests.FillRate.Numerator != 1 || metrics.Digests.FillRate.Denominator != 5 || metrics.Digests.ShortageReasons["article_ineligible"] != 9 || metrics.Digests.ExplorationQuotaRate.Rate != 1 {
		t.Fatalf("digest metrics = %#v", metrics.Digests)
	}
	if metrics.LongTail.GemContribution.Rate != 1 || metrics.LongTail.ContributingSites != 1 || metrics.LongTail.BlogrollPositiveArticles != 1 || metrics.LongTail.BackfillPositiveArticles != 1 {
		t.Fatalf("long-tail metrics = %#v", metrics.LongTail)
	}
	if metrics.Integrity.HardFilterViolations != 0 || metrics.Integrity.DuplicateClusterViolations != 0 || metrics.Integrity.ActiveBlockViolations != 0 {
		t.Fatalf("integrity = %#v", metrics.Integrity)
	}
}

func TestObservabilityM16IntegrityUsesPublicationEvidence(t *testing.T) {
	setupSQLiteDB(t)
	useRecommendationTestClock(t, time.Date(2026, 6, 2, 8, 0, 0, 0, time.UTC))
	settings := DefaultRecommendationSettings(931)
	settings.DailyLimit = 1
	if _, err := SaveRecommendationSettings(&settings); err != nil {
		t.Fatal(err)
	}
	candidate := createReadyCandidate(t, "https://evidence.example/article", "Evidence", []string{"audit"}, "evidence", .9, .8)
	if _, err := GenerateDailyRecommendations(context.Background(), 931, "2026-06-02"); err != nil {
		t.Fatal(err)
	}
	if err := discovery.UpdateCandidates(db.Model(&DiscoveryCandidate{}).Where("id = ?", candidate.ID), map[string]interface{}{"processing_state": discovery.DiscoveryProcessingFailed, "eligibility_state": discovery.DiscoveryEligibilityIneligible}).Error; err != nil {
		t.Fatal(err)
	}
	metrics, err := GetAdminProductMetrics(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if metrics.Integrity.HardFilterViolations != 0 {
		t.Fatalf("publication evidence changed with live candidate: %#v", metrics.Integrity)
	}
}
