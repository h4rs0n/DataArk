package recommendation

import (
	"DataArk/assessment"
	"DataArk/discovery"
	"errors"
	"testing"
	"time"

	"gorm.io/gorm"
)

func TestRecommendationExperienceM15ContextIsTraceableAndUserScoped(t *testing.T) {
	setupSQLiteDB(t)
	now := time.Date(2026, 5, 1, 8, 0, 0, 0, time.UTC)
	site := discovery.DiscoverySite{RootURL: "https://trace.example", HostKey: "trace.example", DisplayName: "Trace Blog", Status: discovery.DiscoverySiteStatusSeed, DiscoveryMethod: "manual", CrawlAllowed: true, FirstDiscoveredAt: now}
	if err := db.Create(&site).Error; err != nil {
		t.Fatal(err)
	}
	source := discovery.DiscoverySource{Name: "Trace Feed", URL: "https://trace.example/feed.xml", Type: "feed", SiteID: &site.ID, Enabled: true}
	if err := db.Create(&source).Error; err != nil {
		t.Fatal(err)
	}
	candidate := createReadyCandidate(t, "https://trace.example/article", "Traceable article", []string{"Go"}, "traceable", 0.9, 0.8)
	candidate.SourceID, candidate.SourceName = source.ID, source.Name
	if err := db.Save(&candidate).Error; err != nil {
		t.Fatal(err)
	}
	provenance := discovery.DiscoveryCandidateProvenance{ProvenanceKey: "trace-provenance", CandidateID: candidate.ID, SiteID: site.ID, SourceID: &source.ID, DiscoveryMethod: "feed", OriginalURL: candidate.URL, FirstSeenAt: now, LastSeenAt: now}
	if err := db.Create(&provenance).Error; err != nil {
		t.Fatal(err)
	}
	row := assessment.ArticleAssessment{CandidateID: candidate.ID, ContentVersion: candidate.ContentVersion, Assessor: "rules", AssessorVersion: "1", PolicyVersion: "v1", OverallQuality: 0.9}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	day, err := CreateRecommendationDay(910, "2026-05-01", 1)
	if err != nil {
		t.Fatal(err)
	}
	item, err := AddRecommendationItem(&RecommendationItem{DayID: uintPointer(day.ID), UserID: 910, CandidateID: candidate.ID, AssessmentID: &row.ID, Rank: 1})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := RecordRecommendationFeedback(910, item.ID, RecommendationFeedbackValuable, nil); err != nil {
		t.Fatal(err)
	}
	context, err := GetRecommendationItemContext(910, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if context.Feedback == nil || context.Assessment == nil || context.UserState == nil || len(context.Provenance) != 2 || context.Provenance[0].Site.ID != site.ID || context.Provenance[0].Graph == nil {
		t.Fatalf("context = %#v", context)
	}
	if _, err := GetRecommendationItemContext(911, item.ID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("cross-user context error = %v", err)
	}
}
