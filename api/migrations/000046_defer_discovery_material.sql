-- +goose Up
-- A discovered URL is a crawl candidate, not yet a content identity.
ALTER TABLE discovery_candidates ALTER COLUMN material_id DROP NOT NULL;
ALTER TABLE discovery_candidates ADD COLUMN pending_content JSONB NOT NULL DEFAULT '{}';
ALTER TABLE material_provenances ALTER COLUMN material_id DROP NOT NULL;
ALTER TABLE material_provenances ADD CONSTRAINT material_provenance_owner CHECK (material_id IS NOT NULL OR candidate_id IS NOT NULL);
DROP VIEW discovery_candidate_details;
CREATE VIEW discovery_candidate_details AS
SELECT c.id,
       c.material_id,
       c.url,
       c.crawl_host,
       c.canonical_url,
       c.normalized_url,
       c.final_url,
       c.dedupe_key,
       c.duplicate_cluster_id,
       c.representative_id,
       c.status,
       c.processing_state,
       c.processing_attempts,
       c.processing_error,
       c.processing_error_type,
       c.next_processing_at,
       c.fetched_at,
       c.extracted_at,
       c.dedupe_state,
       c.last_seen_at,
       c.first_seen_at,
       c.created_at,
       c.updated_at,
       c.pending_content,
       COALESCE(m.title, (c.pending_content->>'title')::text) AS title,
       COALESCE(m.summary, (c.pending_content->>'summary')::text) AS summary,
       COALESCE(m.language, (c.pending_content->>'language')::text) AS language,
       COALESCE(m.published_at, (c.pending_content->>'published_at')::timestamptz) AS published_at,
       COALESCE(m.topics, (c.pending_content->>'topics')::jsonb) AS topics,
       COALESCE(m.entities, (c.pending_content->>'entities')::jsonb) AS entities,
       COALESCE(s.content_version, (c.pending_content->>'content_version')::bigint) AS content_version,
       COALESCE(s.body_changed_at, (c.pending_content->>'body_changed_at')::timestamptz) AS body_changed_at,
       COALESCE(s.content_type, (c.pending_content->>'content_type')::text) AS content_type,
       COALESCE(s.content_style, (c.pending_content->>'content_style')::text) AS content_style,
       COALESCE(s.quality_score, (c.pending_content->>'quality_score')::double precision) AS quality_score,
       COALESCE(s.depth_score, (c.pending_content->>'depth_score')::double precision) AS depth_score,
       COALESCE(s.assessment_state, (c.pending_content->>'assessment_state')::text) AS assessment_state,
       COALESCE(s.current_assessment_id, (c.pending_content->>'current_assessment_id')::bigint) AS current_assessment_id,
       COALESCE(s.assessment_error, (c.pending_content->>'assessment_error')::text) AS assessment_error,
       COALESCE(s.eligibility_state, (c.pending_content->>'eligibility_state')::text) AS eligibility_state,
       COALESCE(s.eligibility_reasons, (c.pending_content->>'eligibility_reasons')::text) AS eligibility_reasons,
       COALESCE(s.enrichment_status, (c.pending_content->>'enrichment_status')::text) AS enrichment_status,
       COALESCE(s.enrichment_error, (c.pending_content->>'enrichment_error')::text) AS enrichment_error,
       COALESCE(s.llm_model, (c.pending_content->>'llm_model')::text) AS llm_model,
       COALESCE(s.prompt_version, (c.pending_content->>'prompt_version')::text) AS prompt_version,
       COALESCE(s.enriched_at, (c.pending_content->>'enriched_at')::timestamptz) AS enriched_at,
       COALESCE(s.score, (c.pending_content->>'score')::double precision) AS score,
       COALESCE(m.authors->>0, c.pending_content->>'author', '') AS author,
       r.text AS body_text,
       r.content_hash,
       COALESCE(r.word_count, 0) AS word_count,
       COALESCE((SELECT e.model FROM material_embeddings e WHERE e.representation_id = r.id ORDER BY e.updated_at DESC LIMIT 1), '') AS embedding_model,
       COALESCE((SELECT p.source_id FROM material_provenances p WHERE (p.material_id = c.material_id OR (c.material_id IS NULL AND p.candidate_id = c.id)) ORDER BY p.first_seen_at, p.id LIMIT 1), 0) AS source_id,
       COALESCE((SELECT p.source_name FROM material_provenances p WHERE (p.material_id = c.material_id OR (c.material_id IS NULL AND p.candidate_id = c.id)) ORDER BY p.first_seen_at, p.id LIMIT 1), '') AS source_name,
       COALESCE((SELECT p.metadata_confidence FROM material_provenances p WHERE (p.material_id = c.material_id OR (c.material_id IS NULL AND p.candidate_id = c.id)) ORDER BY p.first_seen_at, p.id LIMIT 1), 0) AS metadata_confidence,
       COALESCE((SELECT p.published_confidence FROM material_provenances p WHERE (p.material_id = c.material_id OR (c.material_id IS NULL AND p.candidate_id = c.id)) ORDER BY p.first_seen_at, p.id LIMIT 1), '') AS published_confidence,
       COALESCE((SELECT l.task_id FROM material_archive_links l WHERE l.material_id = c.material_id ORDER BY l.task_id LIMIT 1), '') AS archived_task_id,
       (SELECT COUNT(DISTINCT p.domain_key) FROM material_provenances p WHERE p.material_id = c.material_id AND p.domain_key <> '') AS independent_source_count
FROM discovery_candidates c
LEFT JOIN material m ON m.id = c.material_id
LEFT JOIN material_article_states s ON s.material_id = m.id
LEFT JOIN material_representations r ON r.version_id = m.current_version_id AND r.kind = 'text' AND r.role = 'body';

-- +goose Down
-- Pending candidates cannot be represented by the old binary without creating
-- invalid materials. Refuse rollback until those candidates are handled.
ALTER TABLE discovery_candidates ALTER COLUMN material_id SET NOT NULL;
ALTER TABLE material_provenances ALTER COLUMN material_id SET NOT NULL;
ALTER TABLE material_provenances DROP CONSTRAINT material_provenance_owner;
DROP VIEW discovery_candidate_details;
ALTER TABLE discovery_candidates DROP COLUMN pending_content;
CREATE VIEW discovery_candidate_details AS
SELECT c.*,
       m.title,
       m.summary,
       m.language,
       m.published_at,
       m.topics,
       m.entities,
       s.content_version,
       s.body_changed_at,
       s.content_type,
       s.content_style,
       s.quality_score,
       s.depth_score,
       s.assessment_state,
       s.current_assessment_id,
       s.assessment_error,
       s.eligibility_state,
       s.eligibility_reasons,
       s.enrichment_status,
       s.enrichment_error,
       s.llm_model,
       s.prompt_version,
       s.enriched_at,
       s.score,
       COALESCE(m.authors->>0, '') AS author,
       r.text AS body_text,
       r.content_hash,
       COALESCE(r.word_count, 0) AS word_count,
       COALESCE((SELECT e.model FROM material_embeddings e WHERE e.representation_id = r.id ORDER BY e.updated_at DESC LIMIT 1), '') AS embedding_model,
       COALESCE((SELECT p.source_id FROM material_provenances p WHERE p.material_id = c.material_id ORDER BY p.first_seen_at, p.id LIMIT 1), 0) AS source_id,
       COALESCE((SELECT p.source_name FROM material_provenances p WHERE p.material_id = c.material_id ORDER BY p.first_seen_at, p.id LIMIT 1), '') AS source_name,
       COALESCE((SELECT p.metadata_confidence FROM material_provenances p WHERE p.material_id = c.material_id ORDER BY p.first_seen_at, p.id LIMIT 1), 0) AS metadata_confidence,
       COALESCE((SELECT p.published_confidence FROM material_provenances p WHERE p.material_id = c.material_id ORDER BY p.first_seen_at, p.id LIMIT 1), '') AS published_confidence,
       COALESCE((SELECT l.task_id FROM material_archive_links l WHERE l.material_id = c.material_id ORDER BY l.task_id LIMIT 1), '') AS archived_task_id,
       (SELECT COUNT(DISTINCT p.domain_key) FROM material_provenances p WHERE p.material_id = c.material_id AND p.domain_key <> '') AS independent_source_count
FROM discovery_candidates c
JOIN material m ON m.id = c.material_id
LEFT JOIN material_article_states s ON s.material_id = m.id
LEFT JOIN material_representations r ON r.version_id = m.current_version_id AND r.kind = 'text' AND r.role = 'body';
