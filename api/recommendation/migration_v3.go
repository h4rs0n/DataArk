package recommendation

import (
	"fmt"

	"gorm.io/gorm"
)

// V3Models returns the recommendation models needed by SQLite and test
// environments where Goose PostgreSQL migrations are intentionally skipped.
func V3Models() []interface{} {
	return []interface{}{
		&RecommendationSettings{},
		&RecommendationDay{},
		&RecommendationItem{},
		&RecommendationFeedback{},
		&UserBlockRule{},
		&UserRecommendationProfile{},
	}
}

// BackfillV3Compatibility freezes legacy display data into the additive
// recommendation snapshot columns. Repeating it does not overwrite snapshots.
func BackfillV3Compatibility(database *gorm.DB) error {
	if database == nil {
		return nil
	}
	if err := DropLegacyPermanentUniqueness(database); err != nil {
		return err
	}
	if err := BackfillFeedbackCurrentState(database); err != nil {
		return err
	}
	if err := database.Exec(`
UPDATE recommendation_days
SET status = CASE status WHEN 'pending' THEN 'draft' WHEN 'generated' THEN 'published' ELSE status END,
    policy_version = CASE WHEN policy_version IS NULL OR policy_version = '' THEN 'v2' ELSE policy_version END,
    audit_version = CASE WHEN audit_version IS NULL OR audit_version = 0 THEN 1 ELSE audit_version END,
    published_at = CASE WHEN status IN ('generated', 'published') AND published_at IS NULL THEN generated_at ELSE published_at END`).Error; err != nil {
		return err
	}
	return database.Exec(`
UPDATE recommendation_items
SET snapshot_title = CASE WHEN snapshot_title IS NULL OR snapshot_title = '' THEN COALESCE((SELECT title FROM discovery_candidates WHERE discovery_candidates.id = recommendation_items.candidate_id), '') ELSE snapshot_title END,
    snapshot_url = CASE WHEN snapshot_url IS NULL OR snapshot_url = '' THEN COALESCE((SELECT url FROM discovery_candidates WHERE discovery_candidates.id = recommendation_items.candidate_id), '') ELSE snapshot_url END,
    snapshot_summary = CASE WHEN snapshot_summary IS NULL OR snapshot_summary = '' THEN COALESCE((SELECT summary FROM discovery_candidates WHERE discovery_candidates.id = recommendation_items.candidate_id), '') ELSE snapshot_summary END,
    snapshot_author = CASE WHEN snapshot_author IS NULL OR snapshot_author = '' THEN COALESCE((SELECT author FROM discovery_candidates WHERE discovery_candidates.id = recommendation_items.candidate_id), '') ELSE snapshot_author END,
    snapshot_source = CASE WHEN snapshot_source IS NULL OR snapshot_source = '' THEN COALESCE((SELECT source_name FROM discovery_candidates WHERE discovery_candidates.id = recommendation_items.candidate_id), '') ELSE snapshot_source END,
    snapshot_published_at = CASE WHEN snapshot_published_at IS NULL THEN (SELECT published_at FROM discovery_candidates WHERE discovery_candidates.id = recommendation_items.candidate_id) ELSE snapshot_published_at END,
    audit_version = CASE WHEN audit_version IS NULL OR audit_version = 0 THEN 1 ELSE audit_version END`).Error
}

// BackfillFeedbackCurrentState selects the newest active legacy event for each
// user/item without deleting older events. It is portable to SQLite test runs.
func BackfillFeedbackCurrentState(database *gorm.DB) error {
	if database == nil || !database.Migrator().HasColumn(&RecommendationFeedback{}, "is_current") {
		return nil
	}
	return database.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&RecommendationFeedback{}).Where("is_current = ?", true).Updates(map[string]interface{}{
			"is_current": false, "current_key": nil,
		}).Error; err != nil {
			return err
		}
		var rows []RecommendationFeedback
		if err := tx.Where("reverted_at IS NULL").Order("user_id asc, recommendation_item_id asc, created_at desc, id desc").Find(&rows).Error; err != nil {
			return err
		}
		seen := make(map[string]struct{})
		for _, row := range rows {
			key := fmt.Sprintf("%d:%d", row.UserID, row.RecommendationItemID)
			if _, ok := seen[key]; ok {
				if row.ClosedReason == "" {
					if err := tx.Model(&RecommendationFeedback{}).Where("id = ?", row.ID).Update("closed_reason", "superseded").Error; err != nil {
						return err
					}
				}
				continue
			}
			seen[key] = struct{}{}
			if err := tx.Model(&RecommendationFeedback{}).Where("id = ?", row.ID).Updates(map[string]interface{}{
				"is_current": true, "current_key": key,
			}).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

// DropLegacyPermanentUniqueness removes only cross-day uniqueness. The
// day/candidate unique index remains and v2 continues its application check.
func DropLegacyPermanentUniqueness(database *gorm.DB) error {
	if database == nil {
		return nil
	}
	if database.Dialector.Name() == "postgres" {
		if err := database.Exec(`ALTER TABLE recommendation_items DROP CONSTRAINT IF EXISTS recommendation_items_user_id_candidate_id_key`).Error; err != nil {
			return err
		}
	}
	for _, index := range []string{"idx_recommendation_items_user_candidate", "uq_recommendation_user_dedupe"} {
		if database.Migrator().HasIndex(&RecommendationItem{}, index) {
			if err := database.Migrator().DropIndex(&RecommendationItem{}, index); err != nil {
				return err
			}
		}
	}
	return nil
}
