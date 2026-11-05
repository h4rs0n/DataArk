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
	candidate, ready, err := loadMaterialArticle(db.WithContext(ctx), id)
	if err != nil || !ready {
		return err
	}
	if expectedVersion != "" && strconv.FormatUint(uint64(candidate.ContentVersion), 10) != expectedVersion {
		return nil
	}
	reserved, err := BatchActiveTarget(db.WithContext(ctx), id, strconv.FormatUint(uint64(candidate.ContentVersion), 10))
	if err != nil || reserved {
		return err
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
		// The material state row lock orders this activation against cluster
		// application for the same material. Checking qualification before
		// acquiring it would let a concurrent apply invalidate the answer.
		allowed, err := discovery.DedupeAssessmentAllowed(tx, candidate.MaterialID)
		if err != nil {
			return err
		}
		if !allowed {
			return nil
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

// loadMaterialArticle loads content from its current version. Discovery rows are
// optional provenance; a failed fetch is never evidence against retained text.
func loadMaterialArticle(database *gorm.DB, id uint) (discovery.DiscoveryCandidate, bool, error) {
	var candidate discovery.DiscoveryCandidate
	author := "json_extract(m.authors, '$[0]') AS author"
	if database.Dialector.Name() == "postgres" {
		author = "m.authors->>0 AS author"
	}
	// One snapshot pairs the body and state. Separate reads could pair the old
	// body with a newer version number while another entrypoint commits a refresh.
	result := database.Table("material m").Select("m.id AS material_id, m.title, m.summary, m.language, m.published_at, m.topics, m.entities, "+author+", s.content_version, s.current_assessment_id, s.assessment_state, s.assessment_error, s.eligibility_state, s.eligibility_reasons, r.text AS body_text, r.content_hash, r.word_count").
		Joins("JOIN material_versions v ON v.id=m.current_version_id AND v.material_id=m.id").
		Joins("JOIN material_article_states s ON s.material_id=m.id AND s.content_version=v.version").
		Joins("JOIN material_representations r ON r.version_id=v.id AND r.kind='text' AND r.role='body'").
		Where("m.id=? AND r.text<>''", id).Scan(&candidate)
	if result.Error != nil || result.RowsAffected == 0 {
		return candidate, false, result.Error
	}
	var provenance discovery.DiscoveryCandidate
	result = discovery.Candidates(database).Select("id, url, normalized_url, final_url, canonical_url, representative_id, dedupe_state, published_confidence").Where("material_id=?", id).
		Order("CASE WHEN dedupe_state='ready' THEN 0 ELSE 1 END, CASE WHEN representative_id=id THEN 0 ELSE 1 END, id").Limit(1).Find(&provenance)
	if result.Error != nil {
		return candidate, false, result.Error
	}
	candidate.ID, candidate.URL = provenance.ID, provenance.URL
	candidate.NormalizedURL, candidate.FinalURL, candidate.CanonicalURL = provenance.NormalizedURL, provenance.FinalURL, provenance.CanonicalURL
	candidate.RepresentativeID, candidate.PublishedConfidence = provenance.RepresentativeID, provenance.PublishedConfidence
	candidate.DedupeState = provenance.DedupeState
	if result.RowsAffected == 0 {
		candidate.DedupeState = discovery.DiscoveryDedupeReady
	}
	return candidate, true, nil
}
