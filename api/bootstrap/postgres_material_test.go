package bootstrap

import (
	"DataArk/discovery"
	"DataArk/material"
	"fmt"
	"strings"
	"testing"

	"gorm.io/gorm"
)

// All IDs are confined to the disposable dataark_v3_verify database. These
// rows use raw pre-material SQL so hooks cannot accidentally mask data loss.
func seedPostgresMaterialHistory(t *testing.T, db *gorm.DB, sourceID uint) {
	t.Helper()
	body := strings.Repeat("Historical evidence preserved across multiple independent discoveries. ", 10)
	queries := []struct {
		sql  string
		args []interface{}
	}{
		{`INSERT INTO users(id, username, password) VALUES(5000, 'material-fixture', 'not-a-real-password')`, nil},
		{`INSERT INTO discovery_sites(id, root_url, host_key, domain_key, display_name, status, discovery_method, first_discovered_at) VALUES(5000, 'https://second.invalid/', 'second.invalid', 'second.invalid', 'Second', 'seed', 'manual', NOW())`, nil},
		{`INSERT INTO discovery_sources(id, site_id, name, url, type) VALUES(5000, 5000, 'Second', 'https://second.invalid/feed', 'feed')`, nil},
		{`INSERT INTO discovery_candidates(id, source_id, source_name, url, title, body_text, content_hash, content_version, status, processing_state, dedupe_state, assessment_state, eligibility_state, last_seen_at)
 VALUES(5000, ?, 'First', 'https://first.invalid/work', 'Historical title', ?, ?, 1, 'new', 'ready', 'ready', 'ready', 'eligible', NOW()),
       (5001, 5000, 'Second', 'https://second.invalid/work', 'Same work', ?, ?, 1, 'new', 'ready', 'ready', 'ready', 'eligible', NOW())`, []interface{}{sourceID, body, material.TextHash(body), body, material.TextHash(body)}},
		{`INSERT INTO discovery_article_content_versions(id, candidate_id, content_version, content_hash, title, body_text, fetched_at)
 SELECT id, id, 1, content_hash, title, body_text, NOW() FROM discovery_candidates WHERE id IN (5000,5001)`, nil},
		{`INSERT INTO discovery_article_assessments(id, candidate_id, content_version, assessor, assessor_version, policy_version, overall_quality)
 VALUES(5000,5000,1,'fixture','1','1',0.9),(5001,5001,1,'fixture','1','1',0.8)`, nil},
		{`UPDATE discovery_candidates SET current_assessment_id = id WHERE id IN (5000,5001)`, nil},
		{`INSERT INTO recommendation_days(id,user_id,recommendation_date,timezone,status,requested_count,actual_count) VALUES(5000,5000,'2026-09-20','UTC','published',2,2)`, nil},
		{`INSERT INTO recommendation_items(id,day_id,user_id,candidate_id,rank,snapshot_title,content_version) VALUES(5000,5000,5000,5000,1,'Frozen first',1),(5001,5000,5000,5001,2,'Frozen second',1)`, nil},
		{`INSERT INTO user_candidate_states(user_id,candidate_id,exposure_count,read_at,current_feedback,feedback_set_at) VALUES(5000,5000,2,NOW(),NULL,NULL),(5000,5001,3,NULL,'valuable',NOW())`, nil},
		{`INSERT INTO archive_documents(id,domain,file_name,source_url,title) VALUES(5000,'first.invalid','work.html','https://first.invalid/work','Archived title')`, nil},
	}
	for _, query := range queries {
		if err := db.Exec(query.sql, query.args...).Error; err != nil {
			t.Fatalf("seed material history: %v", err)
		}
	}
}

func verifyPostgresMaterialHistory(t *testing.T, db *gorm.DB) {
	t.Helper()
	for attempt := 0; attempt < 2; attempt++ {
		if err := discovery.ReconcileMaterialIdentities(db); err != nil {
			t.Fatal(err)
		}
	}
	assertPostgresScalar(t, db, `SELECT COUNT(DISTINCT material_id)::text FROM discovery_candidates WHERE id IN (5000,5001)`, "1")
	assertPostgresScalar(t, db, `SELECT COUNT(*)::text FROM material_versions WHERE material_id=5000`, "2")
	assertPostgresScalar(t, db, `SELECT COUNT(*)::text FROM discovery_article_assessments WHERE material_id=5000`, "2")
	assertPostgresScalar(t, db, `SELECT string_agg(snapshot_title, '|' ORDER BY rank) FROM recommendation_items WHERE day_id=5000`, "Frozen first|Frozen second")
	assertPostgresScalar(t, db, `SELECT COUNT(*)::text FROM recommendation_items WHERE day_id=5000 AND legacy_duplicate`, "1")
	assertPostgresScalar(t, db, `SELECT exposure_count::text || ':' || current_feedback || ':' || (read_at IS NOT NULL)::text FROM user_material_states WHERE user_id=5000`, "5:valuable:true")
	assertPostgresScalar(t, db, `SELECT material_id::text FROM archive_documents WHERE id=5000`, "5000")
	assertPostgresScalar(t, db, `SELECT independent_source_count::text FROM discovery_candidate_details WHERE id=5000`, "2")
	assertPostgresScalar(t, db, `SELECT COUNT(*)::text FROM information_schema.columns WHERE table_name='discovery_candidates' AND column_name IN ('title','body_text','source_id','assessment_state','embedding')`, "0")
	var alias discovery.DiscoveryCandidate
	if err := db.First(&alias, 5001).Error; err != nil {
		t.Fatal(err)
	}
	if alias.MaterialID != 5000 || alias.BodyText == "" {
		t.Fatalf("projected alias = %#v", alias)
	}
	// Source deletion must preserve its observed domain for future ranking.
	if err := db.Exec(`DELETE FROM discovery_sources WHERE id=5000`).Error; err != nil {
		t.Fatal(err)
	}
	assertPostgresScalar(t, db, fmt.Sprintf(`SELECT independent_source_count::text FROM discovery_candidate_details WHERE id=%d`, alias.ID), "2")
}
