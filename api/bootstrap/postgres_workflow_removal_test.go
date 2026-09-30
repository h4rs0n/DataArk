package bootstrap

import (
	"testing"

	"gorm.io/gorm"
)

// Use SQL against the published pre-removal schema, without retaining retired models.
func seedPostgresRetiredWorkflow(t *testing.T, db *gorm.DB) {
	t.Helper()
	for _, query := range []string{
		`INSERT INTO article_assessment_workflow_runs(id, seed, manifest_digest, policy_version, status, created_by)
 VALUES(5000, 'removal-fixture', 'fixture-digest', 'article-value-v4', 'evaluating', 5000)`,
		`INSERT INTO article_assessment_workflow_items(run_id, position, sample_id, candidate_id, content_version_id,
 content_version, content_hash, body_characters, stratum, baseline_quality, baseline_depth, baseline_evergreen,
 rule_quality, rule_depth, rule_evergreen, material_id, material_version_id)
 SELECT 5000, 0, 'fixture', candidate_id, id, content_version, content_hash, length(body_text), 'core',
 80, 70, 60, 50, 40, 30, material_id, material_version_id FROM discovery_article_content_versions WHERE id=5000`,
		`INSERT INTO article_assessment_workflow_labels(run_id, sample_id, pass, quality, depth, evergreen, labeled_by)
 VALUES(5000, 'fixture', 1, 80, 70, 60, 5000)`,
		`INSERT INTO article_assessment_workflow_scores(run_id, generation, sample_id, model_run, candidate_id)
 VALUES(5000, 1, 'fixture', 1, 5000)`,
		`INSERT INTO article_assessment_workflow_calls(run_id, generation, candidate_id, status)
 VALUES(5000, 1, 5000, 'success')`,
		`INSERT INTO assessment_llm_calls(id, candidate_id, material_id, stage, status, total_tokens)
 VALUES(5000, 5000, 5000, 'article_assessment', 'success', 1234)`,
	} {
		if err := db.Exec(query).Error; err != nil {
			t.Fatalf("seed retired workflow: %v", err)
		}
	}
	for _, table := range []string{"runs", "items", "labels", "scores", "calls"} {
		assertPostgresScalar(t, db, "SELECT COUNT(*)::text FROM article_assessment_workflow_"+table, "1")
	}
}

func verifyPostgresWorkflowRemoval(t *testing.T, db *gorm.DB) {
	t.Helper()
	assertPostgresScalar(t, db, `SELECT COUNT(*)::text FROM information_schema.tables
 WHERE table_schema='public' AND table_name LIKE 'article_assessment_workflow_%'`, "0")
	assertPostgresScalar(t, db, `SELECT COUNT(*)::text FROM discovery_article_content_versions WHERE id IN (5000,5001)`, "2")
	assertPostgresScalar(t, db, `SELECT COUNT(*)::text FROM discovery_article_assessments WHERE id IN (5000,5001)`, "2")
	assertPostgresScalar(t, db, `SELECT total_tokens::text FROM assessment_llm_calls WHERE id=5000`, "1234")
	assertPostgresScalar(t, db, `SELECT string_agg(snapshot_title, '|' ORDER BY rank) FROM recommendation_items WHERE day_id=5000`, "Frozen first|Frozen second")
}
