package discovery

import (
	"DataArk/material"
	"fmt"
	"strings"

	"gorm.io/gorm"
)

// CandidateProjectionSQL is shared with the SQLite test stand-in. Production
// creates the equivalent view through numbered Goose SQL, never AutoMigrate.
func CandidateProjectionSQL(dialect string) string {
	columns := []string{"c.id", "c.material_id", "c.url", "c.crawl_host", "c.canonical_url", "c.normalized_url", "c.final_url", "c.dedupe_key", "c.duplicate_cluster_id", "c.representative_id", "c.status", "c.processing_state", "c.processing_attempts", "c.processing_error", "c.processing_error_type", "c.next_processing_at", "c.fetched_at", "c.extracted_at", "c.dedupe_state", "c.last_seen_at", "c.first_seen_at", "c.created_at", "c.updated_at"}
	for _, column := range materialColumns {
		columns = append(columns, "m."+column)
	}
	for _, column := range articleColumns {
		columns = append(columns, "s."+column)
	}
	if dialect == "postgres" {
		columns = append(columns, "COALESCE(m.authors->>0, '') AS author")
	} else {
		columns = append(columns, "COALESCE(json_extract(m.authors, '$[0]'), '') AS author")
	}
	columns = append(columns, "r.text AS body_text", "r.content_hash", "COALESCE(r.word_count, 0) AS word_count", "COALESCE((SELECT e.model FROM material_embeddings e WHERE e.representation_id = r.id ORDER BY e.updated_at DESC LIMIT 1), '') AS embedding_model")
	for _, column := range provenanceColumns {
		fallback := "''"
		if column == "source_id" || column == "metadata_confidence" {
			fallback = "0"
		}
		columns = append(columns, fmt.Sprintf("COALESCE((SELECT p.%s FROM material_provenances p WHERE p.material_id = c.material_id ORDER BY p.first_seen_at, p.id LIMIT 1), %s) AS %s", column, fallback, column))
	}
	columns = append(columns,
		"COALESCE((SELECT l.task_id FROM material_archive_links l WHERE l.material_id = c.material_id ORDER BY l.task_id LIMIT 1), '') AS archived_task_id",
		"(SELECT COUNT(DISTINCT p.domain_key) FROM material_provenances p WHERE p.material_id = c.material_id AND p.domain_key <> '') AS independent_source_count")
	return "SELECT " + strings.Join(columns, ", ") + ` FROM discovery_candidates c
JOIN material m ON m.id = c.material_id
LEFT JOIN material_article_states s ON s.material_id = m.id
LEFT JOIN material_representations r ON r.version_id = m.current_version_id AND r.kind = 'text' AND r.role = 'body'`
}

// MigrateMaterialTestSchema only supports the SQLite unit-test stand-in.
func MigrateMaterialTestSchema(database *gorm.DB) error {
	if database.Dialector.Name() != "sqlite" {
		return fmt.Errorf("test schema requires SQLite")
	}
	if err := database.AutoMigrate(material.Models()...); err != nil {
		return err
	}
	if err := database.AutoMigrate(&DiscoverySource{}, &DiscoverySite{}); err != nil {
		return err
	}
	if database.Migrator().HasColumn("discovery_candidates", "title") {
		// Test-only equivalent of 000033 for the legacy SQLite fixture.
		if err := database.Transaction(func(tx *gorm.DB) error {
			var legacy []DiscoveryCandidate
			if err := tx.Session(&gorm.Session{SkipHooks: true}).Where("material_id IS NULL OR material_id = 0").Find(&legacy).Error; err != nil {
				return err
			}
			for _, candidate := range legacy {
				if err := candidate.BeforeCreate(tx); err != nil {
					return err
				}
				if err := tx.Model(&DiscoveryCandidate{}).Where("id = ?", candidate.ID).Update("material_id", candidate.MaterialID).Error; err != nil {
					return err
				}
				if err := updateCandidateContent(tx, &candidate, candidateContentValues(&candidate)); err != nil {
					return err
				}
				if candidate.SourceID != 0 {
					if err := saveCandidateSource(tx, &candidate, candidate.SourceID, candidate.SourceName); err != nil {
						return err
					}
				}
			}
			return nil
		}); err != nil {
			return err
		}
	}
	if err := database.Exec("DROP VIEW IF EXISTS discovery_candidate_details").Error; err != nil {
		return err
	}
	return database.Exec("CREATE VIEW discovery_candidate_details AS " + CandidateProjectionSQL("sqlite")).Error
}
