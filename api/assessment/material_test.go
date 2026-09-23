package assessment

import (
	"DataArk/material"
	"context"
	"testing"
	"time"

	"gorm.io/gorm"
)

func TestAssessMaterialWithoutDiscoveryCandidate(t *testing.T) {
	setupAssessmentDB(t)
	core := material.Material{Title: "A content work independent of crawling"}
	if err := db.Create(&core).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Transaction(func(tx *gorm.DB) error {
		version, err := material.AppendText(tx, core, "Extracted content can originate from a document or a transcript.", 12, "fixture", time.Now())
		if err != nil {
			return err
		}
		return tx.Create(&material.ArticleState{MaterialID: core.ID, ContentVersion: version.Version}).Error
	}); err != nil {
		t.Fatal(err)
	}
	assessor := fixtureArticleAssessor{name: "content-fixture", version: "1", result: ArticleAssessmentResult{Quality: .9, Depth: .8, Evergreen: .7, Confidence: .9, Reasons: []string{"Evidence supports the conclusion"}}}
	if err := AssessMaterial(context.Background(), core.ID, "1", assessor); err != nil {
		t.Fatal(err)
	}
	if err := AssessMaterial(context.Background(), core.ID, "1", assessor); err != nil {
		t.Fatal(err)
	}
	var rows []ArticleAssessment
	if err := db.Where("material_id = ?", core.ID).Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].CandidateID != 0 || rows[0].MaterialVersionID == nil {
		t.Fatalf("content-owned assessment = %#v", rows)
	}
}
