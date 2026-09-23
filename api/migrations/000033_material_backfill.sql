-- +goose Up
INSERT INTO material(id, title, summary, authors, language, published_at, topics, entities, created_at, updated_at)
SELECT id, title, summary, CASE WHEN COALESCE(author, '') = '' THEN '[]'::jsonb ELSE jsonb_build_array(author) END,
       language, published_at, COALESCE(NULLIF(topics::text, ''), '[]')::jsonb,
       COALESCE(NULLIF(entities::text, ''), '[]')::jsonb, COALESCE(created_at, NOW()), COALESCE(updated_at, NOW())
FROM discovery_candidates;
SELECT setval(pg_get_serial_sequence('material', 'id'), COALESCE(MAX(id), 1), COUNT(*) > 0) FROM material;
UPDATE discovery_candidates SET material_id = id;

INSERT INTO material_versions(id, material_id, version, title, summary, authors, language, published_at, created_at)
SELECT id, candidate_id, content_version, title, summary,
       CASE WHEN COALESCE(author, '') = '' THEN '[]'::jsonb ELSE jsonb_build_array(author) END,
       language, published_at, COALESCE(created_at, fetched_at, NOW())
FROM discovery_article_content_versions;
SELECT setval(pg_get_serial_sequence('material_versions', 'id'), COALESCE(MAX(id), 1), COUNT(*) > 0) FROM material_versions;
INSERT INTO material_versions(material_id, version, title, summary, authors, language, published_at, created_at)
SELECT c.id, GREATEST(COALESCE(c.content_version, 0), 1), m.title, m.summary, m.authors, m.language, m.published_at,
       COALESCE(c.extracted_at, c.updated_at, NOW())
FROM discovery_candidates c JOIN material m ON m.id = c.id
WHERE COALESCE(c.body_text, '') <> ''
ON CONFLICT(material_id, version) DO NOTHING;
INSERT INTO material_candidate_versions(candidate_id, content_version, material_id, version_id)
SELECT material_id, version, material_id, id FROM material_versions;
INSERT INTO material_representations(version_id, kind, role, text, content_hash, word_count, extractor, created_at)
SELECT v.id, 'text', 'body', COALESCE(cv.body_text, c.body_text), COALESCE(cv.content_hash, c.content_hash),
       COALESCE(cv.word_count, c.word_count, 0), 'legacy_article', v.created_at
FROM material_versions v JOIN discovery_candidates c ON c.id = v.material_id
LEFT JOIN discovery_article_content_versions cv ON cv.candidate_id = c.id AND cv.content_version = v.version;
UPDATE material m SET current_version_id = v.id
FROM discovery_candidates c JOIN material_versions v ON v.material_id = c.id AND v.version = GREATEST(COALESCE(c.content_version, 0), 1)
WHERE m.id = c.id;
UPDATE discovery_article_content_versions cv SET material_id = c.material_id, material_version_id = v.id
FROM discovery_candidates c, material_versions v
WHERE cv.candidate_id = c.id AND v.material_id = c.material_id AND v.version = cv.content_version;

INSERT INTO material_article_states(material_id, content_version, body_changed_at, content_type, content_style,
    quality_score, depth_score, assessment_state, current_assessment_id, assessment_error, eligibility_state,
    eligibility_reasons, enrichment_status, enrichment_error, llm_model, prompt_version, enriched_at, score, updated_at)
SELECT id, COALESCE(content_version, 0), body_changed_at, content_type, content_style,
    COALESCE(quality_score, 0), COALESCE(depth_score, 0), COALESCE(NULLIF(assessment_state, ''), 'pending'),
    current_assessment_id, assessment_error, COALESCE(NULLIF(eligibility_state, ''), 'unknown'),
    eligibility_reasons, enrichment_status, enrichment_error, llm_model, prompt_version, enriched_at,
    COALESCE(score, 0), COALESCE(updated_at, NOW()) FROM discovery_candidates;

-- The old deduper moved evidence to its representative. Recover the observed
-- target where the saved original URL identifies it; never copy an entire cluster.
INSERT INTO material_provenances(provenance_key, material_id, candidate_id, site_id, source_id, domain_key,
    source_name, discovery_method, original_url, source_page_url, migration_uncertain, first_seen_at, last_seen_at,
    created_at, updated_at)
SELECT p.provenance_key, COALESCE(target.material_id, c.material_id), COALESCE(target.id, c.id), p.site_id,
    p.source_id, COALESCE(site.domain_key, ''), src.name, p.discovery_method, p.original_url, p.source_page_url,
    target.id IS NULL AND p.original_url IS DISTINCT FROM c.url, p.first_seen_at, p.last_seen_at, p.created_at, p.updated_at
FROM discovery_candidate_provenances p JOIN discovery_candidates c ON c.id = p.candidate_id
LEFT JOIN discovery_sites site ON site.id = p.site_id
LEFT JOIN discovery_sources src ON src.id = p.source_id
LEFT JOIN LATERAL (SELECT id, material_id FROM discovery_candidates WHERE url = p.original_url OR normalized_url = p.original_url ORDER BY id LIMIT 1) target ON TRUE;
INSERT INTO material_provenances(provenance_key, material_id, candidate_id, site_id, source_id, domain_key,
    source_name, discovery_method, original_url, title, summary, published_at, metadata_confidence,
    published_confidence, first_seen_at, last_seen_at)
SELECT 'candidate:' || c.id || ':source:' || c.source_id, c.material_id, c.id, src.site_id, src.id,
    COALESCE(site.domain_key, ''), c.source_name, 'legacy_source', c.url, c.title, c.summary, c.published_at,
    COALESCE(c.metadata_confidence, 0), c.published_confidence,
    COALESCE(c.first_seen_at, c.created_at, NOW()), COALESCE(c.last_seen_at, c.updated_at, NOW())
FROM discovery_candidates c LEFT JOIN discovery_sources src ON src.id = c.source_id
LEFT JOIN discovery_sites site ON site.id = src.site_id;

-- archived_task_id was present in some historical GORM installations only.
-- +goose StatementBegin
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'discovery_candidates' AND column_name = 'archived_task_id') THEN
        INSERT INTO material_archive_links(task_id, material_id)
        SELECT archived_task_id, MIN(material_id) FROM discovery_candidates
        WHERE COALESCE(archived_task_id, '') <> '' GROUP BY archived_task_id;
    END IF;
    IF EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'discovery_candidates' AND column_name = 'embedding') THEN
        INSERT INTO material_embeddings(representation_id, model, embedding)
        SELECT r.id, COALESCE(c.embedding_model, ''), c.embedding FROM discovery_candidates c
        JOIN material m ON m.id = c.material_id JOIN material_representations r ON r.version_id = m.current_version_id
        WHERE c.embedding IS NOT NULL;
    END IF;
END $$;
-- +goose StatementEnd

CREATE TEMP TABLE material_archive_mapping ON COMMIT DROP AS
SELECT a.id AS archive_id, COALESCE((SELECT MIN(c.material_id) FROM discovery_candidates c
    WHERE NULLIF(a.source_url, '') IS NOT NULL AND (c.url = a.source_url OR c.normalized_url = a.source_url OR c.final_url = a.source_url)),
    nextval(pg_get_serial_sequence('material', 'id'))) AS material_id
FROM archive_documents a;
INSERT INTO material(id, title, summary, created_at, updated_at)
SELECT map.material_id, a.title, a.summary, COALESCE(a.created_at, NOW()), COALESCE(a.updated_at, NOW())
FROM archive_documents a JOIN material_archive_mapping map ON map.archive_id = a.id
ON CONFLICT(id) DO NOTHING;
UPDATE archive_documents a SET material_id = map.material_id FROM material_archive_mapping map WHERE map.archive_id = a.id;

UPDATE discovery_article_assessments a SET material_id = c.material_id, material_version_id = v.version_id
FROM discovery_candidates c LEFT JOIN material_candidate_versions v ON v.candidate_id = c.id
WHERE a.candidate_id = c.id AND a.content_version = v.content_version;
-- Pre-extraction legacy assessment rows may not have an immutable version.
UPDATE discovery_article_assessments a SET material_id = c.material_id FROM discovery_candidates c
WHERE a.candidate_id = c.id AND a.material_id IS NULL;
UPDATE recommendation_items i SET material_id = c.material_id FROM discovery_candidates c WHERE i.candidate_id = c.id;
UPDATE recommendation_items i SET material_version_id = v.version_id FROM material_candidate_versions v
WHERE i.candidate_id = v.candidate_id AND i.content_version = v.content_version;
UPDATE recommendation_feedbacks f SET material_id = c.material_id FROM discovery_candidates c WHERE f.candidate_id = c.id;
UPDATE assessment_llm_calls a SET material_id = c.material_id FROM discovery_candidates c WHERE a.candidate_id = c.id;
UPDATE article_assessment_workflow_items i SET material_id = v.material_id, material_version_id = v.material_version_id
FROM discovery_article_content_versions v WHERE i.content_version_id = v.id;

ALTER TABLE user_candidate_states RENAME TO user_material_states;
ALTER TABLE user_material_states ADD COLUMN material_id BIGINT REFERENCES material(id) ON DELETE RESTRICT;
UPDATE user_material_states u SET material_id = c.material_id FROM discovery_candidates c WHERE u.candidate_id = c.id;
ALTER TABLE user_material_states ALTER COLUMN material_id SET NOT NULL;
CREATE UNIQUE INDEX idx_user_material_state ON user_material_states(user_id, material_id);
CREATE UNIQUE INDEX idx_recommendation_day_material ON recommendation_items(day_id, material_id) WHERE NOT legacy_duplicate;
CREATE UNIQUE INDEX idx_recommendation_feed_material ON recommendation_items(feed_batch_id, material_id) WHERE NOT legacy_duplicate;

ALTER TABLE discovery_candidates ALTER COLUMN material_id SET NOT NULL;
ALTER TABLE archive_documents ALTER COLUMN material_id SET NOT NULL;
INSERT INTO material_migration_checkpoints(name) VALUES ('legacy-storage-copied');

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Restore the pre-material backup to roll back content identity migration'; END $$;
-- +goose StatementEnd
