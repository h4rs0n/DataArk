package discovery

import (
	"DataArk/config"
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

type fixtureArticleAssessor struct {
	name    string
	version string
	result  ArticleAssessmentResult
	err     error
}

func (assessor fixtureArticleAssessor) Name() string    { return assessor.name }
func (assessor fixtureArticleAssessor) Version() string { return assessor.version }
func (fixtureArticleAssessor) PolicyVersion() string    { return "article-quality-v1+fixture" }
func (assessor fixtureArticleAssessor) Assess(context.Context, ArticleAssessmentInput) (ArticleAssessmentResult, error) {
	return assessor.result, assessor.err
}

type contentAwareArticleAssessor struct{}

func (contentAwareArticleAssessor) Name() string          { return "semantic_fixture" }
func (contentAwareArticleAssessor) Version() string       { return "1" }
func (contentAwareArticleAssessor) PolicyVersion() string { return ArticleQualityPolicyVersion }
func (contentAwareArticleAssessor) Assess(_ context.Context, input ArticleAssessmentInput) (ArticleAssessmentResult, error) {
	quality, depth, evergreen := .15, .10, .15
	if strings.Contains(input.BodyText, "measurement 42") {
		quality, depth, evergreen = .85, .80, .75
	}
	return ArticleAssessmentResult{
		Quality: quality, Depth: depth, Evergreen: evergreen, Confidence: .90,
		Reasons: []string{"content-only fixture evidence", "content-only fixture limitation"},
	}, nil
}

func TestArticleAssessmentInputExcludesSourceAggregates(t *testing.T) {
	typeOfInput := reflect.TypeOf(ArticleAssessmentInput{})
	for index := 0; index < typeOfInput.NumField(); index++ {
		name := strings.ToLower(typeOfInput.Field(index).Name)
		for _, forbidden := range []string{"source", "site", "yield", "graph", "feedback", "reputation", "average", "hit", "qualityhistory", "author", "publication", "url", "html", "published"} {
			if strings.Contains(name, forbidden) {
				t.Fatalf("ArticleAssessmentInput field %q crosses forbidden source boundary %q", name, forbidden)
			}
		}
	}
}

func TestLowHitSourceHighArticlesAreAssessedIndependently(t *testing.T) {
	setupSQLiteDB(t)
	oldThreshold := config.DISCOVERYARTICLEQUALITYTHRESHOLD
	config.DISCOVERYARTICLEQUALITYTHRESHOLD = 0.45
	t.Cleanup(func() { config.DISCOVERYARTICLEQUALITYTHRESHOLD = oldThreshold })
	now := time.Date(2026, 7, 14, 4, 0, 0, 0, time.UTC)
	semantic := contentAwareArticleAssessor{}
	ordinaryBody := strings.Repeat("routine status update. ", 18)
	highBody := strings.Repeat("Evidence from measurement 42 supports this durable method because the experiment compares alternatives, records counterexamples, explains the mechanism, and reaches a reproducible conclusion. ", 12)

	bCandidates := make([]DiscoveryCandidate, 0, 100)
	for index := 0; index < 100; index++ {
		body := ordinaryBody
		title := fmt.Sprintf("Routine note %03d", index)
		if index >= 97 {
			body = highBody
			title = fmt.Sprintf("Independent investigation %03d", index)
		}
		candidate := createAssessmentCandidate(t, "Long-tail B", fmt.Sprintf("https://b.example/posts/%03d", index), title, body, now)
		if err := AssessCandidate(context.Background(), candidate.ID, semantic); err != nil {
			t.Fatal(err)
		}
		bCandidates = append(bCandidates, candidate)
	}
	aOrdinary := createAssessmentCandidate(t, "Seed A", "https://a.example/ordinary", "Ordinary seed note", ordinaryBody, now)
	aHigh := createAssessmentCandidate(t, "Seed A", "https://a.example/high", "Independent investigation 097", highBody, now)
	for _, candidate := range []DiscoveryCandidate{aOrdinary, aHigh} {
		if err := AssessCandidate(context.Background(), candidate.ID, semantic); err != nil {
			t.Fatal(err)
		}
	}

	var eligibleB int64
	if err := db.Model(&DiscoveryCandidate{}).Where("source_name = ? AND eligibility_state = ?", "Long-tail B", DiscoveryEligibilityEligible).Count(&eligibleB).Error; err != nil {
		t.Fatal(err)
	}
	if eligibleB != 3 {
		t.Fatalf("eligible B articles = %d, want 3 independent high articles", eligibleB)
	}
	var bHigh, loadedAHigh, loadedAOrdinary DiscoveryCandidate
	if err := db.First(&bHigh, bCandidates[97].ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.First(&loadedAHigh, aHigh.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.First(&loadedAOrdinary, aOrdinary.ID).Error; err != nil {
		t.Fatal(err)
	}
	if bHigh.QualityScore != loadedAHigh.QualityScore || bHigh.DepthScore != loadedAHigh.DepthScore {
		t.Fatalf("same article scores differ by source: B=%v/%v A=%v/%v", bHigh.QualityScore, bHigh.DepthScore, loadedAHigh.QualityScore, loadedAHigh.DepthScore)
	}
	if bHigh.QualityScore <= loadedAOrdinary.QualityScore || bHigh.EligibilityState != DiscoveryEligibilityEligible {
		t.Fatalf("long-tail high article did not beat seed ordinary: B=%#v A=%#v", bHigh, loadedAOrdinary)
	}
}

func TestOptionalAssessorFailureFallsBackAndVersionsRemainImmutable(t *testing.T) {
	setupSQLiteDB(t)
	now := time.Date(2026, 7, 14, 5, 0, 0, 0, time.UTC)
	body := strings.Repeat("Evidence and measurement support this durable method because alternatives and counterexamples explain the mechanism and conclusion. ", 12)
	candidate := createAssessmentCandidate(t, "Fixture", "https://example.com/assessment", "Assessment fixture", body, now)
	failing := fixtureArticleAssessor{name: "optional_fixture", version: "broken", err: errors.New("fixture LLM unavailable")}
	if err := AssessCandidate(context.Background(), candidate.ID, failing); err != nil {
		t.Fatal(err)
	}
	if err := db.First(&candidate, candidate.ID).Error; err != nil {
		t.Fatal(err)
	}
	if candidate.AssessmentState != DiscoveryAssessmentDegraded || candidate.CurrentAssessmentID == nil || candidate.AssessmentError != "article-quality-v1+fixture: fixture LLM unavailable" || candidate.EligibilityState != DiscoveryEligibilityEligible {
		t.Fatalf("fallback candidate = %#v", candidate)
	}
	var assessments []DiscoveryArticleAssessment
	if err := db.Where("candidate_id = ?", candidate.ID).Find(&assessments).Error; err != nil {
		t.Fatal(err)
	}
	if len(assessments) != 1 || assessments[0].Assessor != RuleArticleAssessorName {
		t.Fatalf("fallback assessments = %#v", assessments)
	}

	if err := db.Exec(`CREATE TABLE recommendation_assessment_history_m9 (id INTEGER PRIMARY KEY, assessment_id INTEGER, snapshot_title TEXT)`).Error; err != nil {
		t.Fatal(err)
	}
	oldAssessmentID := *candidate.CurrentAssessmentID
	if err := db.Exec(`INSERT INTO recommendation_assessment_history_m9(id, assessment_id, snapshot_title) VALUES(1, ?, 'Published assessment')`, oldAssessmentID).Error; err != nil {
		t.Fatal(err)
	}
	updatedBody := body + strings.Repeat(" New observations add independent evidence and a revised conclusion.", 4)
	if err := db.Model(&candidate).Updates(map[string]interface{}{
		"body_text": updatedBody, "word_count": len(strings.Fields(updatedBody)), "content_hash": ContentHash(updatedBody),
		"content_version": 2, "assessment_state": DiscoveryAssessmentPending, "current_assessment_id": nil,
		"eligibility_state": DiscoveryEligibilityUnknown,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := AssessCandidate(context.Background(), candidate.ID, nil); err != nil {
		t.Fatal(err)
	}
	if err := db.Where("candidate_id = ? AND assessor = ?", candidate.ID, RuleArticleAssessorName).Order("content_version").Find(&assessments).Error; err != nil {
		t.Fatal(err)
	}
	if len(assessments) != 2 || assessments[0].ContentVersion != 1 || assessments[1].ContentVersion != 2 || assessments[0].ID == assessments[1].ID {
		t.Fatalf("versioned assessments = %#v", assessments)
	}
	var historyAssessmentID uint
	var snapshotTitle string
	if err := db.Raw(`SELECT assessment_id, snapshot_title FROM recommendation_assessment_history_m9 WHERE id = 1`).Row().Scan(&historyAssessmentID, &snapshotTitle); err != nil {
		t.Fatal(err)
	}
	if historyAssessmentID != oldAssessmentID || snapshotTitle != "Published assessment" {
		t.Fatalf("assessment history changed: %d %q", historyAssessmentID, snapshotTitle)
	}
}

func TestSemanticQualityFloorIsTwentyAndConfidenceDoesNotGate(t *testing.T) {
	setupSQLiteDB(t)
	oldThreshold := config.DISCOVERYARTICLEQUALITYTHRESHOLD
	config.DISCOVERYARTICLEQUALITYTHRESHOLD = .20
	t.Cleanup(func() { config.DISCOVERYARTICLEQUALITYTHRESHOLD = oldThreshold })
	now := time.Date(2026, 8, 10, 11, 0, 0, 0, time.UTC)
	below := createAssessmentCandidate(t, "Fixture", "https://example.com/below", "Below floor", strings.Repeat("substance ", 80), now)
	atFloor := createAssessmentCandidate(t, "Fixture", "https://example.com/at-floor", "At floor", strings.Repeat("substance ", 80), now)
	for _, fixture := range []struct {
		candidate DiscoveryCandidate
		quality   float64
	}{
		{below, .19},
		{atFloor, .20},
	} {
		assessor := fixtureArticleAssessor{name: "floor_fixture", version: fmt.Sprintf("%.2f", fixture.quality), result: ArticleAssessmentResult{Quality: fixture.quality, Depth: .1, Evergreen: .1, Confidence: 0, Reasons: []string{"quality boundary", "low confidence is not a gate"}}}
		if err := AssessCandidate(context.Background(), fixture.candidate.ID, assessor); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.First(&below, below.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.First(&atFloor, atFloor.ID).Error; err != nil {
		t.Fatal(err)
	}
	if below.EligibilityState != DiscoveryEligibilityIneligible || atFloor.EligibilityState != DiscoveryEligibilityEligible {
		t.Fatalf("quality floor results below=%#v atFloor=%#v", below, atFloor)
	}
}

func createAssessmentCandidate(t *testing.T, sourceName string, rawURL string, title string, body string, now time.Time) DiscoveryCandidate {
	t.Helper()
	candidate := DiscoveryCandidate{
		SourceID: 1, SourceName: sourceName, URL: rawURL, NormalizedURL: rawURL, CanonicalURL: rawURL,
		FinalURL: rawURL, Title: title, BodyText: body, Language: "en", WordCount: len(strings.Fields(body)),
		ContentHash: ContentHash(body), ContentVersion: 1, DedupeKey: ContentHash(rawURL),
		Status: DiscoveryCandidateStatusNew, ProcessingState: DiscoveryProcessingReady,
		DedupeState: DiscoveryDedupeReady, AssessmentState: DiscoveryAssessmentPending,
		EligibilityState: DiscoveryEligibilityUnknown, LastSeenAt: now, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&candidate).Error; err != nil {
		t.Fatal(err)
	}
	return candidate
}
