package discovery

import (
	"DataArk/material"
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestCrawlFailuresPreserveSharedContent(t *testing.T) {
	for _, failure := range []struct {
		name     string
		response FetchResult
		err      error
	}{
		{"timeout", FetchResult{}, context.DeadlineExceeded},
		{"robots", FetchResult{}, ErrRobotsDisallowed},
		{"http", FetchResult{StatusCode: 404}, nil},
		{"extraction", FetchResult{StatusCode: 200, Body: []byte("invalid article")}, nil},
	} {
		t.Run(failure.name, func(t *testing.T) {
			setupSQLiteDB(t)
			candidate := newIndexedCandidate(t, "refresh-"+failure.name, strings.Repeat("Accepted article with evidence. ", 50))
			if err := UpdateCandidate(db, candidate.ID, map[string]interface{}{"dedupe_state": "ready", "assessment_state": "ready", "current_assessment_id": uint(77), "eligibility_state": "eligible", "eligibility_reasons": "article_quality_passed"}).Error; err != nil {
				t.Fatal(err)
			}
			alias := DiscoveryCandidate{MaterialID: candidate.MaterialID, URL: "https://alias.example/" + failure.name, ProcessingState: "ready", DedupeState: "ready"}
			if err := db.Create(&alias).Error; err != nil {
				t.Fatal(err)
			}
			var before material.ArticleState
			if err := db.First(&before, "material_id=?", candidate.MaterialID).Error; err != nil {
				t.Fatal(err)
			}
			oldFetch := fetchDiscoveryRequest
			t.Cleanup(func() { fetchDiscoveryRequest = oldFetch })
			fetchDiscoveryRequest = func(context.Context, FetchRequest) (FetchResult, error) { return failure.response, failure.err }
			_ = ProcessCandidate(context.Background(), candidate.ID, "1")
			var after material.ArticleState
			if err := db.First(&after, "material_id=?", candidate.MaterialID).Error; err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(before, after) {
				t.Fatalf("crawl changed article state: before=%+v after=%+v", before, after)
			}
			if err := db.First(&candidate, candidate.ID).Error; err != nil {
				t.Fatal(err)
			}
			if candidate.ProcessingErrorType == "" {
				t.Fatal("missing crawl diagnostic")
			}
			ready, err := material.ArticleReady(db, candidate.MaterialID)
			if err != nil || !ready {
				t.Fatalf("retained article unavailable: %v", err)
			}
		})
	}
}

func TestUnchangedExtractionPreservesAssessmentAndMetadata(t *testing.T) {
	setupSQLiteDB(t)
	body := strings.Repeat("Accepted text with reproducible experiments. ", 50)
	candidate := newIndexedCandidate(t, "unchanged", body)
	if err := UpdateCandidate(db, candidate.ID, map[string]interface{}{"title": "Assessed title", "summary": "Assessed summary", "dedupe_state": "ready", "assessment_state": "ready", "current_assessment_id": uint(77), "eligibility_state": "eligible", "eligibility_reasons": "article_quality_passed"}).Error; err != nil {
		t.Fatal(err)
	}
	var before material.ArticleState
	if err := db.First(&before, "material_id=?", candidate.MaterialID).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	article := &ExtractedArticle{Title: "Fresh page title", Description: "Page description", Text: body, Language: "en"}
	if err := commitExtractedArticle(candidate.ID, 1, candidate.URL, now, article, now); err != nil {
		t.Fatal(err)
	}
	var after material.ArticleState
	if err := db.First(&after, "material_id=?", candidate.MaterialID).Error; err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("same body invalidated assessment: %+v", after)
	}
	if err := db.First(&candidate, candidate.ID).Error; err != nil {
		t.Fatal(err)
	}
	if candidate.Title != "Assessed title" || candidate.Summary != "Assessed summary" || candidate.DedupeState != "ready" {
		t.Fatalf("same body replaced assessed content: %+v", candidate)
	}
	article.Text += " Changed conclusion."
	if err := commitExtractedArticle(candidate.ID, 1, candidate.URL, now, article, now); err != nil {
		t.Fatal(err)
	}
	if err := db.First(&after, "material_id=?", candidate.MaterialID).Error; err != nil {
		t.Fatal(err)
	}
	if after.ContentVersion != 2 || after.AssessmentState != "pending" || after.CurrentAssessmentID != nil || after.EligibilityState != "unknown" {
		t.Fatalf("new body did not invalidate assessment: %+v", after)
	}
	if err := recordCandidateFetchFailure(&candidate, errors.New("stale failure"), now); err != nil && err.Error() != "stale failure" {
		t.Fatal(err)
	}
	if err := db.First(&candidate, candidate.ID).Error; err != nil {
		t.Fatal(err)
	}
	if candidate.ProcessingState != "ready" || candidate.ProcessingError != "" {
		t.Fatal("old failure overwrote new extraction")
	}
}
