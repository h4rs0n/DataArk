package discovery

import (
	"DataArk/material"
	"context"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"
)

func TestMaterialKeepsIndependentSourcesAndAssessmentAcrossRediscovery(t *testing.T) {
	setupSQLiteDB(t)
	now := time.Now()
	var sources []DiscoverySource
	for _, rawURL := range []string{"https://one.example/feed", "https://one.example/alternate", "https://two.example/feed"} {
		source := DiscoverySource{Name: rawURL, URL: rawURL, Type: DiscoverySourceTypeFeed}
		if err := db.Create(&source).Error; err != nil {
			t.Fatal(err)
		}
		sources = append(sources, source)
	}
	entry := discoveredCandidate{URL: "https://author.example/article", Title: "Original title"}
	first, err := upsertDiscoveryCandidate(sources[0], entry)
	if err != nil {
		t.Fatal(err)
	}
	if err := UpdateCandidate(db, first.Candidate.ID, map[string]interface{}{
		"body_text": strings.Repeat("independent research evidence ", 20), "word_count": 60,
		"processing_state": DiscoveryProcessingReady, "dedupe_state": DiscoveryDedupeReady,
		"assessment_state": DiscoveryAssessmentReady, "eligibility_state": DiscoveryEligibilityEligible,
	}).Error; err != nil {
		t.Fatal(err)
	}
	for _, source := range sources {
		if _, err := upsertDiscoveryCandidate(source, entry); err != nil {
			t.Fatal(err)
		}
	}
	var got DiscoveryCandidate
	if err := db.First(&got, first.Candidate.ID).Error; err != nil {
		t.Fatal(err)
	}
	if got.IndependentSourceCount != 2 || got.AssessmentState != DiscoveryAssessmentReady || got.BodyText == "" {
		t.Fatalf("rediscovery lost content/state or inflated sources: %#v", got)
	}
	var evidence int64
	if err := db.Model(&material.Provenance{}).Where("material_id = ?", got.MaterialID).Count(&evidence).Error; err != nil {
		t.Fatal(err)
	}
	if evidence != 3 {
		t.Fatalf("provenance count = %d, want 3 endpoints", evidence)
	}
	alias := DiscoveryCandidate{SourceID: sources[2].ID, URL: "https://author.example/article?utm_source=other", Status: DiscoveryCandidateStatusNew, LastSeenAt: now}
	if err := db.Create(&alias).Error; err != nil {
		t.Fatal(err)
	}
	if alias.MaterialID != got.MaterialID || alias.BodyText != got.BodyText || alias.AssessmentState != DiscoveryAssessmentReady {
		t.Fatalf("normalized alias reset existing material: %#v", alias)
	}
	if _, err := MarkUserCandidateRead(42, got.ID); err != nil {
		t.Fatal(err)
	}
	state, err := GetUserCandidateState(42, alias.ID)
	if err != nil || state.ReadAt == nil {
		t.Fatalf("shared read state = %#v, %v", state, err)
	}
	for _, column := range []string{"title", "body_text", "source_id", "assessment_state"} {
		if db.Migrator().HasColumn("discovery_candidates", column) {
			t.Fatalf("candidate still owns %s", column)
		}
	}
}

func TestMaterialMergeRetainsVersionHistoryAndUserState(t *testing.T) {
	setupSQLiteDB(t)
	body := strings.Repeat("A reproducible observation with complete evidence. ", 10)
	a := createDedupeCandidate(t, "https://a.example/work", "https://a.example/work", "https://a.example/work", body, time.Now())
	b := createDedupeCandidate(t, "https://b.example/work", "https://b.example/work", "https://b.example/work", body, time.Now())
	oldID := b.MaterialID
	if _, err := MarkUserCandidateRead(7, a.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := MarkUserCandidateIgnored(7, b.ID); err != nil {
		t.Fatal(err)
	}
	if err := ResolveCandidateDuplicates(context.Background(), a.ID); err != nil {
		t.Fatal(err)
	}
	if err := ResolveCandidateDuplicates(context.Background(), b.ID); err != nil {
		t.Fatal(err)
	}
	if err := db.First(&b, b.ID).Error; err != nil {
		t.Fatal(err)
	}
	if b.MaterialID != a.MaterialID {
		t.Fatalf("exact text did not merge: %d, %d", a.MaterialID, b.MaterialID)
	}
	id, err := material.ResolveID(db, oldID)
	if err != nil || id != a.MaterialID {
		t.Fatalf("redirect = %d, %v", id, err)
	}
	var count int64
	if err := db.Model(&material.Version{}).Where("material_id = ?", id).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("historical versions = %d, want 2", count)
	}
	state, err := GetUserCandidateState(7, a.ID)
	if err != nil || state.ReadAt == nil || state.CurrentFeedback != UserCandidateFeedbackNotInterested {
		t.Fatalf("merged state = %#v, %v", state, err)
	}
	// A second modality is attached to the same logical version without changing
	// material's universal fields or introducing a physical file path.
	var core material.Material
	if err := db.First(&core, id).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Transaction(func(tx *gorm.DB) error {
		return tx.Create(&material.Representation{VersionID: *core.CurrentVersionID, Kind: "text", Role: "transcript", Text: "spoken evidence"}).Error
	}); err != nil {
		t.Fatal(err)
	}
}
