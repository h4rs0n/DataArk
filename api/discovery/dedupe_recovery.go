package discovery

import (
	"DataArk/jobqueue"
	"context"
	"fmt"
	"time"

	"gorm.io/gorm"
)

// Match loadDedupeArticle/usableDedupeArticle: the first ready candidate is
// authoritative, including when another candidate of that material is allowed.
// A material without discovery candidates remains usable independently.
func usableDedupeMaterialSQL(materialAlias string) string {
	return `(NOT EXISTS (SELECT 1 FROM discovery_candidates uc WHERE uc.material_id=` + materialAlias + `.id)
 OR EXISTS (SELECT 1 FROM discovery_candidates uc WHERE uc.material_id=` + materialAlias + `.id
 AND uc.id=(SELECT MIN(fc.id) FROM discovery_candidates fc WHERE fc.material_id=` + materialAlias + `.id AND fc.processing_state='ready')
 AND NOT EXISTS (SELECT 1 FROM discovery_domain_blacklist_entries b WHERE uc.crawl_host=b.domain OR uc.crawl_host LIKE '%.' || b.domain)))`
}

// Domain changes wake the affected groups, including usable survivors of a
// newly blocked representative. Blocked candidates themselves do not cause
// actionable drift. This runs in the blacklist mutation's transaction.
func invalidateDedupeDomain(database *gorm.DB, domain string) error {
	if !database.Migrator().HasTable("article_dedupe_settings") {
		return nil
	}
	setting, err := dedupeSetting(database)
	if err != nil || setting.Mode != "active" || setting.ActiveRunID == 0 {
		return err
	}
	affected := database.Session(&gorm.Session{NewDB: true}).Table("discovery_candidates bc").Select("bc.material_id").
		Where("bc.crawl_host=? OR bc.crawl_host LIKE ?", domain, "%."+domain)
	groups := database.Session(&gorm.Session{NewDB: true}).Table("article_dedupe_members bm").Select("bm.representative_id").
		Where("bm.run_id=? AND bm.material_id IN (?)", setting.ActiveRunID, affected)
	members := database.Session(&gorm.Session{NewDB: true}).Table("article_dedupe_members gm").Select("gm.material_id").
		Where("gm.run_id=? AND gm.representative_id IN (?)", setting.ActiveRunID, groups)
	return database.Model(&DiscoveryCandidate{}).Where("processing_state='ready'").
		Where("material_id IN (?) OR material_id IN (?)", affected, members).
		Update("dedupe_state", DiscoveryDedupePending).Error
}

// A full sweep computes canonical candidates and current usable articles once.
// Materialization prevents the coverage join from re-running MIN(candidate.id),
// blacklist checks, and body availability checks for every member lookup.
func dedupeRecoveryCandidates(database *gorm.DB, runID uint) *gorm.DB {
	blacklist, err := discoveryCandidateBlacklistPredicate("candidate")
	if err != nil {
		failed := database.Session(&gorm.Session{NewDB: true})
		failed.AddError(err)
		return failed
	}
	return database.Raw(`WITH ready AS MATERIALIZED (
 SELECT material_id, MIN(id) AS candidate_id,
 MAX(CASE WHEN dedupe_state<>'ready' THEN 1 ELSE 0 END) AS pending
 FROM discovery_candidates WHERE processing_state='ready' GROUP BY material_id
), current_articles AS MATERIALIZED (
 SELECT r.id AS representation_id, m.id AS material_id, r.version_id,
 COALESCE(ready.candidate_id,0) AS candidate_id, COALESCE(ready.pending,0) AS pending
 FROM material m JOIN material_representations r ON r.version_id=m.current_version_id
 JOIN material_article_states s ON s.material_id=m.id
 LEFT JOIN ready ON ready.material_id=m.id
 LEFT JOIN discovery_candidates candidate ON candidate.id=ready.candidate_id
 WHERE r.kind='text' AND r.role='body' AND r.text<>''
 AND (ready.candidate_id IS NOT NULL OR NOT EXISTS (SELECT 1 FROM discovery_candidates c WHERE c.material_id=m.id))
 AND NOT `+blacklist+`
), representatives AS MATERIALIZED (
 SELECT d.representation_id FROM article_dedupe_members d
 JOIN current_articles a ON a.representation_id=d.representation_id
 WHERE d.run_id=? AND d.applied AND (d.representation_id=d.representative_id) IS TRUE
 AND CASE WHEN d.material_id=a.material_id AND d.version_id=a.version_id AND d.candidate_id=a.candidate_id THEN TRUE ELSE FALSE END
)
 SELECT a.representation_id FROM current_articles a
 LEFT JOIN article_dedupe_members d ON d.run_id=? AND d.representation_id=a.representation_id AND d.applied
 LEFT JOIN representatives reps ON reps.representation_id=d.representative_id
 WHERE a.pending=1 OR d.representation_id IS NULL OR reps.representation_id IS NULL
 OR (d.version_id=a.version_id) IS NOT TRUE OR (d.material_id=a.material_id) IS NOT TRUE
 ORDER BY a.representation_id`, runID, runID)
}

// This is the Ready portion of ArticleDedupeQualification, without assessment
// coverage or body reads. Queue pruning uses this gate; the full sweep applies
// the same checks against its materialized current articles.
func qualifiedDedupeRepresentatives(database *gorm.DB, runID uint) *gorm.DB {
	return database.Table("article_dedupe_members rd").
		Select("rd.representation_id, rd.material_id, rd.version_id").
		Joins("JOIN material pm ON pm.id=rd.material_id AND CASE WHEN pm.current_version_id=rd.version_id THEN TRUE ELSE FALSE END").
		Joins("JOIN material_representations pr ON pr.id=rd.representation_id AND CASE WHEN pr.version_id=rd.version_id THEN TRUE ELSE FALSE END").
		Joins("JOIN material_article_states ps ON ps.material_id=pm.id").
		Where("rd.run_id=? AND rd.applied AND (rd.representation_id=rd.representative_id) IS TRUE", runID).
		Where("pr.kind='text' AND pr.role='body' AND pr.text<>''").
		Where("CASE WHEN rd.candidate_id=COALESCE((SELECT MIN(pc.id) FROM discovery_candidates pc WHERE pc.material_id=pm.id AND pc.processing_state='ready'),0) THEN TRUE ELSE FALSE END").
		Where(usableDedupeMaterialSQL("pm"))
}

func suppressedDedupeAssessmentTargets(database *gorm.DB, runID uint, targets []jobqueue.AssessmentTarget) (map[uint]bool, error) {
	result := map[uint]bool{}
	ids := make([]uint, 0, len(targets))
	for _, target := range targets {
		if target.MaterialID != 0 {
			ids = append(ids, target.MaterialID)
		}
	}
	if len(ids) == 0 {
		return result, nil
	}
	var suppressed []uint
	err := database.Table("article_dedupe_members d").
		Joins("JOIN material m ON m.id=d.material_id AND m.current_version_id=d.version_id").
		Joins("JOIN (?) reps ON reps.representation_id=d.representative_id", qualifiedDedupeRepresentatives(database.Session(&gorm.Session{NewDB: true}), runID)).
		Where("d.run_id=? AND d.applied AND d.representation_id<>d.representative_id AND d.material_id IN ?", runID, ids).
		Where("EXISTS (SELECT 1 FROM article_dedupe_settings ds WHERE ds.id=1 AND ds.mode='active' AND ds.active_run_id=?)", runID).
		Distinct().Pluck("d.material_id", &suppressed).Error
	for _, id := range suppressed {
		result[id] = true
	}
	return result, err
}

// PendingAssessmentTargets avoids expanding the candidate details view, which
// joins material a second time for the dedupe gate and can project article text.
// The existing projection-level gate and current-version semantics are retained.
func PendingAssessmentTargets(ctx context.Context, database *gorm.DB, requireDedupeReady bool, dueAt *time.Time) ([]jobqueue.AssessmentTarget, error) {
	database = database.WithContext(ctx)
	query, err := pendingAssessmentTargetsQuery(database, requireDedupeReady, dueAt)
	if err != nil {
		return nil, err
	}
	var rows []struct{ MaterialID, ContentVersion uint }
	if err := query.Scan(&rows).Error; err != nil {
		return nil, err
	}
	targets := make([]jobqueue.AssessmentTarget, 0, len(rows))
	seen := map[uint]bool{}
	for _, row := range rows {
		if !seen[row.MaterialID] {
			seen[row.MaterialID] = true
			targets = append(targets, jobqueue.AssessmentTarget{MaterialID: row.MaterialID, ContentVersion: fmt.Sprint(row.ContentVersion)})
		}
	}
	return targets, nil
}

func pendingAssessmentTargetsQuery(database *gorm.DB, requireDedupeReady bool, dueAt *time.Time) (*gorm.DB, error) {
	setting, err := dedupeSetting(database)
	if err != nil {
		return nil, err
	}
	query := database.Table("discovery_candidates").Select("discovery_candidates.material_id, s.content_version").
		Joins("JOIN material m ON m.id=discovery_candidates.material_id").
		Joins("JOIN material_article_states s ON s.material_id=discovery_candidates.material_id").
		Where("discovery_candidates.processing_state='ready' AND s.assessment_state='pending'").
		Where(DedupeRepresentativeClause("discovery_candidates."))
	if requireDedupeReady {
		query = query.Where("discovery_candidates.dedupe_state='ready'")
	}
	if dueAt != nil {
		query = query.Where("discovery_candidates.next_processing_at IS NULL OR discovery_candidates.next_processing_at<=?", *dueAt)
	}
	if setting.Mode == "active" && setting.ActiveRunID != 0 {
		// Material/version IDs are correlated, but PostgreSQL cannot collect
		// cross-table statistics for their two equalities. Keep the version
		// validation as a boolean filter after the material join; flattening it
		// into another equality estimates ~1 row for a 95k-member library.
		query = query.Where(`EXISTS (SELECT 1 FROM article_dedupe_members d WHERE d.run_id=?
 AND d.material_id=discovery_candidates.material_id AND CASE WHEN d.version_id=m.current_version_id THEN TRUE ELSE FALSE END AND d.applied
 AND (d.representation_id=d.representative_id) IS TRUE)`, setting.ActiveRunID)
	} else if setting.Mode == "exact" {
		query = query.Where("discovery_candidates.id=(SELECT MIN(fc.id) FROM discovery_candidates fc WHERE fc.material_id=discovery_candidates.material_id AND fc.processing_state='ready')")
	}
	query = ExcludeBlacklistedCandidateDomains(query, "discovery_candidates")
	return query.Order("discovery_candidates.id"), nil
}
