package discovery

import (
	"DataArk/config"
	"DataArk/material"
	"context"
	"errors"
	"gorm.io/gorm"
	"strings"
	"testing"
	"time"
)

func TestMaterialCreatedOnlyAfterArticleValidation(t *testing.T) {
	setupSQLiteDB(t)
	verifyDeferredMaterial(t)
}

// Shared with the opt-in PostgreSQL test: SQLite cannot prove FK/JSONB behavior.
func verifyDeferredMaterial(t *testing.T) {
	t.Helper()
	oldFetch, oldMinimum := fetchDiscoveryRequest, config.DISCOVERYARTICLEMINCHARS
	t.Cleanup(func() { fetchDiscoveryRequest = oldFetch; config.DISCOVERYARTICLEMINCHARS = oldMinimum })
	config.DISCOVERYARTICLEMINCHARS = 120
	var baseline int64
	if err := db.Model(&material.Material{}).Count(&baseline).Error; err != nil {
		t.Fatal(err)
	}
	source := DiscoverySource{Name: "Admission feed", URL: "https://admission.example/feed", Type: DiscoverySourceTypeFeed, EndpointType: DiscoveryEndpointFeed}
	if err := db.Create(&source).Error; err != nil {
		t.Fatal(err)
	}
	published := time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
	entries := []struct {
		name, body string
		status     int
		fetchErr   error
	}{
		{name: "not-article", body: `<html><head><title>Navigation</title></head><body><nav>Home Tags Login</nav></body></html>`, status: 200},
		{name: "short", body: string(articleHTML("Short article", "Very short body.")), status: 200},
		{name: "http", status: 404},
		{name: "retry", fetchErr: errors.New("temporary fetch failure")},
	}
	var retryID, rejectedID uint
	for _, entry := range entries {
		t.Run(entry.name, func(t *testing.T) {
			result, err := upsertDiscoveryCandidate(source, discoveredCandidate{URL: "https://admission.example/posts/" + entry.name, Title: "Feed title", PublishedAt: &published, DiscoveryMethod: DiscoveryMethodFeed})
			if err != nil {
				t.Fatal(err)
			}
			if result.Candidate.MaterialID != 0 || result.Candidate.PublishedAt == nil || !result.Candidate.PublishedAt.Equal(published) {
				t.Fatalf("pending metadata lost or material created early: %#v", result.Candidate)
			}
			// Rediscovery updates metadata without creating a material.
			if _, err := upsertDiscoveryCandidate(source, discoveredCandidate{URL: result.Candidate.URL, Title: "Updated feed title", DiscoveryMethod: DiscoveryMethodFeed}); err != nil {
				t.Fatal(err)
			}
			fetchDiscoveryRequest = func(context.Context, FetchRequest) (FetchResult, error) {
				return FetchResult{StatusCode: entry.status, ContentType: "text/html", Body: []byte(entry.body), FinalURL: result.Candidate.URL, FetchedAt: time.Now()}, entry.fetchErr
			}
			err = ProcessCandidate(context.Background(), result.Candidate.ID, "0")
			if entry.fetchErr == nil && err != nil {
				t.Fatal(err)
			}
			if entry.fetchErr != nil && err == nil {
				t.Fatal("expected retry error")
			}
			var got DiscoveryCandidate
			if err := db.First(&got, result.Candidate.ID).Error; err != nil {
				t.Fatal(err)
			}
			if got.MaterialID != 0 || got.Title != "Updated feed title" || got.ProcessingErrorType == "" {
				t.Fatalf("rejected candidate lost metadata/state: %#v", got)
			}
			var count int64
			if err := db.Model(&material.Material{}).Count(&count).Error; err != nil || count != baseline {
				t.Fatalf("invalid article created material: count=%d baseline=%d err=%v", count, baseline, err)
			}
			if rejectedID == 0 {
				rejectedID = got.ID
			}
			retryID = got.ID
		})
	}
	// A second discovery path can record provenance before any material exists.
	site := DiscoverySite{RootURL: "https://secondary-admission.example/", HostKey: "secondary-admission.example", DomainKey: "secondary-admission.example", Status: DiscoverySiteStatusActive, CrawlAllowed: true, FirstDiscoveredAt: time.Now()}
	if err := db.Create(&site).Error; err != nil {
		t.Fatal(err)
	}
	secondary := DiscoverySource{Name: "Secondary feed", URL: site.RootURL + "feed", Type: DiscoverySourceTypeFeed, SiteID: &site.ID}
	if err := db.Create(&secondary).Error; err != nil {
		t.Fatal(err)
	}
	provenance := DiscoveryCandidateProvenance{ProvenanceKey: "admission-secondary", CandidateID: retryID, SiteID: site.ID, SourceID: &secondary.ID, DiscoveryMethod: DiscoveryMethodFeed, OriginalURL: "https://admission.example/posts/retry", FirstSeenAt: time.Now(), LastSeenAt: time.Now()}
	if err := db.Create(&provenance).Error; err != nil {
		t.Fatal(err)
	}
	if provenance.MaterialID != 0 {
		t.Fatal("source evidence created a material before validation")
	}
	// A later successful retry promotes the same candidate, keeping source evidence.
	body := strings.Repeat("A durable research article with measurements, examples, reproducible evidence and a concrete conclusion. ", 20)
	fetchDiscoveryRequest = func(context.Context, FetchRequest) (FetchResult, error) {
		return FetchResult{StatusCode: 200, ContentType: "text/html", Body: articleHTML("Validated article", body), FinalURL: "https://admission.example/posts/retry", FetchedAt: time.Now()}, nil
	}
	if err := ProcessCandidate(context.Background(), retryID, "0"); err != nil {
		t.Fatal(err)
	}
	var got DiscoveryCandidate
	if err := db.First(&got, retryID).Error; err != nil {
		t.Fatal(err)
	}
	if got.MaterialID == 0 || got.Title != "Validated article" || got.ProcessingState != DiscoveryProcessingReady || got.ContentVersion != 1 || got.BodyText == "" || got.SourceID != source.ID || got.PendingContent != "{}" || got.IndependentSourceCount != 2 {
		t.Fatalf("article promotion incomplete: %#v", got)
	}
	var evidence int64
	if err := db.Model(&material.Provenance{}).Where("candidate_id=? AND material_id=?", retryID, got.MaterialID).Count(&evidence).Error; err != nil || evidence != 2 {
		t.Fatalf("source promotion count=%d err=%v", evidence, err)
	}
	if err := ProcessCandidate(context.Background(), retryID, "0"); err != nil {
		t.Fatal(err)
	}
	var count int64
	if err := db.Model(&material.Material{}).Count(&count).Error; err != nil || count != baseline+1 {
		t.Fatalf("duplicate promotion: count=%d err=%v", count, err)
	}
	verifyMaterialPromotionRollback(t, rejectedID, baseline+1)
}

func verifyMaterialPromotionRollback(t *testing.T, candidateID uint, baseline int64) {
	t.Helper()
	const callback = "test:reject_material_version"
	if err := db.Callback().Create().Before("gorm:create").Register(callback, func(tx *gorm.DB) {
		if tx.Statement.Table == "material_versions" {
			tx.AddError(errors.New("injected version write failure"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer db.Callback().Create().Remove(callback)
	if err := ProcessCandidate(context.Background(), candidateID, "0"); err == nil {
		t.Fatal("expected promotion failure")
	}
	var candidate DiscoveryCandidate
	if err := db.First(&candidate, candidateID).Error; err != nil {
		t.Fatal(err)
	}
	if candidate.MaterialID != 0 || candidate.PendingContent == "{}" {
		t.Fatalf("failed promotion was partially committed: %#v", candidate)
	}
	var count int64
	if err := db.Model(&material.Material{}).Count(&count).Error; err != nil || count != baseline {
		t.Fatalf("failed promotion left material: count=%d err=%v", count, err)
	}
	if err := db.Model(&DiscoveryArticleContentVersion{}).Where("candidate_id=?", candidateID).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("failed promotion left legacy version: count=%d err=%v", count, err)
	}
	if err := db.Model(&material.Provenance{}).Where("candidate_id=? AND material_id IS NOT NULL", candidateID).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("failed promotion moved evidence: count=%d err=%v", count, err)
	}
}
