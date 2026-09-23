package material

import (
	"errors"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"sort"
	"time"
)

// BindIdentity merges only explicit strong evidence. Similarity never calls it.
// The enclosing transaction also updates provenance and the processing record.
func BindIdentity(tx *gorm.DB, materialID uint, kind, value string) (uint, error) {
	if value == "" {
		return materialID, nil
	}
	key := Hash(value)
	if tx.Dialector.Name() == "postgres" {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?, 0))", "material:"+kind+":"+key).Error; err != nil {
			return 0, err
		}
	}
	id, err := ResolveID(tx, materialID)
	if err != nil {
		return 0, err
	}
	var identity Identity
	err = tx.Where("kind = ? AND identity_key = ?", kind, key).First(&identity).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return id, tx.Create(&Identity{MaterialID: id, Kind: kind, IdentityKey: key, Value: value}).Error
	}
	if err != nil {
		return 0, err
	}
	other, err := ResolveID(tx, identity.MaterialID)
	if err != nil {
		return 0, err
	}
	if other == id {
		return id, nil
	}
	return Merge(tx, id, other)
}

// Merge retains immutable rows and redirects old IDs; it never rewrites a
// published recommendation snapshot or copies provenance across similar works.
func Merge(tx *gorm.DB, first, second uint) (uint, error) {
	ids := []uint{first, second}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	target, source := ids[0], ids[1]
	if target == source {
		return target, nil
	}
	var rows []Material
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id IN ?", ids).Order("id").Find(&rows).Error; err != nil {
		return 0, err
	}
	if len(rows) != 2 {
		return 0, gorm.ErrRecordNotFound
	}
	chosen := rows[0]
	if chosen.CurrentVersionID == nil {
		chosen = rows[1]
	}
	if err := tx.Model(&Material{}).Where("id = ?", source).Update("current_version_id", nil).Error; err != nil {
		return 0, err
	}
	var versions []Version
	if err := tx.Where("material_id = ?", source).Order("version, id").Find(&versions).Error; err != nil {
		return 0, err
	}
	var maximum uint
	if err := tx.Model(&Version{}).Where("material_id = ?", target).Select("COALESCE(MAX(version),0)").Scan(&maximum).Error; err != nil {
		return 0, err
	}
	for _, version := range versions {
		maximum++
		if err := tx.Model(&Version{}).Where("id = ?", version.ID).Updates(map[string]interface{}{"material_id": target, "version": maximum}).Error; err != nil {
			return 0, err
		}
		for _, table := range []string{"discovery_article_assessments", "recommendation_items"} {
			if !tx.Migrator().HasTable(table) {
				continue
			}
			if err := tx.Table(table).Where("material_version_id = ?", version.ID).Update("content_version", maximum).Error; err != nil {
				return 0, err
			}
		}
	}
	if tx.Migrator().HasTable(&UserState{}) {
		if err := mergeUserStates(tx, target, source); err != nil {
			return 0, err
		}
	}
	// Published duplicates remain as explicit historical exceptions to the new
	// material uniqueness constraints. Their item IDs, ranks and snapshots stay.
	if tx.Migrator().HasTable("recommendation_items") {
		if err := tx.Exec(`UPDATE recommendation_items SET legacy_duplicate = TRUE WHERE material_id = ? AND EXISTS
 (SELECT 1 FROM recommendation_items prior WHERE prior.material_id = ? AND
 ((prior.day_id = recommendation_items.day_id) OR (prior.feed_batch_id = recommendation_items.feed_batch_id)))`, source, target).Error; err != nil {
			return 0, err
		}
	}
	for _, table := range []string{"discovery_candidates", "material_provenances", "material_identities", "material_redirects", "material_candidate_versions", "material_archive_links", "archive_documents", "discovery_article_content_versions", "discovery_article_assessments", "recommendation_items", "recommendation_feedbacks", "assessment_llm_calls", "article_assessment_workflow_items"} {
		if !tx.Migrator().HasColumn(table, "material_id") {
			continue
		}
		if err := tx.Table(table).Where("material_id = ?", source).Update("material_id", target).Error; err != nil {
			return 0, err
		}
	}
	if chosen.ID == source {
		var state ArticleState
		if err := tx.First(&state, "material_id = ?", source).Error; err != nil {
			return 0, err
		}
		state.MaterialID = target
		if chosen.CurrentVersionID != nil {
			var version Version
			if err := tx.First(&version, *chosen.CurrentVersionID).Error; err != nil {
				return 0, err
			}
			state.ContentVersion = version.Version
		}
		if err := tx.Save(&state).Error; err != nil {
			return 0, err
		}
		if err := tx.Model(&Material{}).Where("id = ?", target).Updates(map[string]interface{}{"title": chosen.Title, "summary": chosen.Summary, "authors": chosen.Authors, "language": chosen.Language, "published_at": chosen.PublishedAt, "topics": chosen.Topics, "entities": chosen.Entities, "current_version_id": chosen.CurrentVersionID}).Error; err != nil {
			return 0, err
		}
	}
	if err := tx.Delete(&ArticleState{}, "material_id = ?", source).Error; err != nil {
		return 0, err
	}
	if err := tx.Create(&Redirect{ID: source, MaterialID: target, CreatedAt: time.Now()}).Error; err != nil {
		return 0, err
	}
	return target, tx.Delete(&Material{}, source).Error
}
