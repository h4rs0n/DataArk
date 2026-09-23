package assessment

import (
	"DataArk/discovery"
	"DataArk/material"
	"context"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"strconv"
)

// AssessMaterial is the content-owned entry point. Candidate endpoints remain
// adapters; job identity and optimistic concurrency use the material version.
func AssessMaterial(ctx context.Context, id uint, expectedVersion string, assessor ArticleAssessor) error {
	if db == nil || id == 0 {
		return gorm.ErrRecordNotFound
	}
	id, err := material.ResolveID(db, id)
	if err != nil {
		return err
	}
	var state material.ArticleState
	if err := db.First(&state, "material_id = ?", id).Error; err != nil {
		return err
	}
	if expectedVersion != "" && strconv.FormatUint(uint64(state.ContentVersion), 10) != expectedVersion {
		return nil
	}
	var candidate discovery.DiscoveryCandidate
	result := discovery.Candidates(db).Where("material_id = ?", id).Order("CASE WHEN representative_id = id THEN 0 ELSE 1 END, id").Limit(1).Find(&candidate)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		var core material.Material
		if err := db.First(&core, id).Error; err != nil {
			return err
		}
		if core.CurrentVersionID == nil {
			return nil
		}
		var body material.Representation
		if err := db.Where("version_id = ? AND kind = ? AND role = ?", *core.CurrentVersionID, "text", "body").First(&body).Error; err != nil {
			return err
		}
		candidate = discovery.DiscoveryCandidate{MaterialID: id, Title: core.Title, Summary: core.Summary, BodyText: body.Text, ContentVersion: state.ContentVersion,
			CurrentAssessmentID: state.CurrentAssessmentID, ProcessingState: discovery.DiscoveryProcessingReady, DedupeState: discovery.DiscoveryDedupeReady}
	}
	return assessMaterialRecord(ctx, candidate, assessor)
}

func updateAssessmentMaterial(candidate discovery.DiscoveryCandidate, values map[string]interface{}) *gorm.DB {
	result := db.Session(&gorm.Session{NewDB: true})
	err := result.Transaction(func(tx *gorm.DB) error {
		var current material.ArticleState
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&current, "material_id = ?", candidate.MaterialID).Error; err != nil {
			return err
		}
		if current.ContentVersion != candidate.ContentVersion {
			return nil
		}
		core, state := map[string]interface{}{}, map[string]interface{}{}
		for name, value := range values {
			switch name {
			case "title", "summary", "topics", "entities", "language", "published_at":
				core[name] = value
			default:
				state[name] = value
			}
		}
		if len(core) > 0 {
			if err := tx.Model(&material.Material{}).Where("id = ?", candidate.MaterialID).Updates(core).Error; err != nil {
				return err
			}
		}
		if len(state) > 0 {
			return tx.Model(&material.ArticleState{}).Where("material_id = ?", candidate.MaterialID).Updates(state).Error
		}
		return nil
	})
	result.AddError(err)
	return result
}
