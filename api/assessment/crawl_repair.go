package assessment

import (
	"DataArk/discovery"
	"DataArk/material"
	"context"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// RecoverCrawlContaminatedAssessments only reuses stored, current-model results.
// It never invokes a provider or authorizes a new assessment run.
func RecoverCrawlContaminatedAssessments(ctx context.Context) error {
	if db == nil || !db.Migrator().HasTable("material_crawl_repairs") {
		return nil
	}
	assessor := ConfiguredArticleAssessor()
	if assessor == nil {
		return nil
	}
	var cursor uint
	for {
		var repairs []struct {
			MaterialID     uint
			AssessmentID   uint
			StateUpdatedAt time.Time
		}
		if err := db.WithContext(ctx).Table("material_crawl_repairs").Where("repaired_at IS NULL AND material_id>?", cursor).Order("material_id").Limit(250).Find(&repairs).Error; err != nil {
			return err
		}
		if len(repairs) == 0 {
			return nil
		}
		for _, repair := range repairs {
			cursor = repair.MaterialID
			if err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
				var state material.ArticleState
				if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&state, "material_id=?", repair.MaterialID).Error; err != nil {
					return err
				}
				mark := func() error {
					return tx.Table("material_crawl_repairs").Where("material_id=?", repair.MaterialID).Update("repaired_at", time.Now().UTC()).Error
				}
				// A subsequent user action or assessment supersedes the migration evidence.
				if !state.UpdatedAt.Equal(repair.StateUpdatedAt) || state.CurrentAssessmentID == nil || *state.CurrentAssessmentID != repair.AssessmentID {
					return mark()
				}
				ready, err := material.ArticleReady(tx, repair.MaterialID)
				if err != nil || !ready {
					return err
				}
				var row ArticleAssessment
				result := tx.Where("id=? AND material_id=? AND content_version=? AND assessor=? AND assessor_version=? AND policy_version=?", repair.AssessmentID, repair.MaterialID, state.ContentVersion, assessor.Name(), assessor.Version(), assessor.PolicyVersion()).Limit(1).Find(&row)
				if result.Error != nil {
					return result.Error
				}
				if result.RowsAffected == 0 || !isModelAssessment(row) {
					return nil
				}
				value := assessmentResultFromRow(row)
				if validateAssessmentResult(value) != nil {
					return nil
				}
				allowed, err := discovery.DedupeAssessmentAllowed(tx, repair.MaterialID)
				if err != nil || !allowed {
					return err
				}
				if err := tx.Model(&material.ArticleState{}).Where("material_id=?", repair.MaterialID).Updates(assessmentActivationValues(row, value, "")).Error; err != nil {
					return err
				}
				return mark()
			}); err != nil {
				return err
			}
		}
	}
}
