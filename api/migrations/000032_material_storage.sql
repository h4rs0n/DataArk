-- +goose Up
CREATE TABLE material (
    id BIGSERIAL PRIMARY KEY,
    title TEXT, summary TEXT, authors JSONB NOT NULL DEFAULT '[]', language TEXT,
    published_at TIMESTAMPTZ, topics JSONB NOT NULL DEFAULT '[]', entities JSONB NOT NULL DEFAULT '[]',
    current_version_id BIGINT, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE TABLE material_versions (
    id BIGSERIAL PRIMARY KEY, material_id BIGINT NOT NULL REFERENCES material(id) ON DELETE RESTRICT,
    version BIGINT NOT NULL, title TEXT, summary TEXT, authors JSONB NOT NULL DEFAULT '[]', language TEXT,
    published_at TIMESTAMPTZ, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(material_id, version), UNIQUE(material_id, id)
);
ALTER TABLE material ADD CONSTRAINT fk_material_current_version
    FOREIGN KEY(id, current_version_id) REFERENCES material_versions(material_id, id) DEFERRABLE INITIALLY DEFERRED;
CREATE TABLE material_representations (
    id BIGSERIAL PRIMARY KEY, version_id BIGINT NOT NULL REFERENCES material_versions(id) ON DELETE CASCADE,
    kind TEXT NOT NULL, role TEXT NOT NULL, text TEXT, content_hash TEXT, hash_algorithm TEXT NOT NULL DEFAULT 'sha256-text-v1',
    word_count INTEGER NOT NULL DEFAULT 0, extractor TEXT, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(version_id, kind, role)
);
CREATE INDEX idx_material_representation_hash ON material_representations(content_hash);
CREATE TABLE material_identities (
    id BIGSERIAL PRIMARY KEY, material_id BIGINT NOT NULL REFERENCES material(id) ON DELETE RESTRICT,
    kind TEXT NOT NULL, identity_key TEXT NOT NULL, value TEXT NOT NULL, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(kind, identity_key)
);
CREATE INDEX idx_material_identity_material ON material_identities(material_id);
CREATE TABLE material_redirects (
    id BIGINT PRIMARY KEY, material_id BIGINT NOT NULL REFERENCES material(id) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), CHECK(id <> material_id)
);
CREATE TABLE material_provenances (
    id BIGSERIAL PRIMARY KEY, provenance_key TEXT NOT NULL UNIQUE,
    material_id BIGINT NOT NULL REFERENCES material(id) ON DELETE RESTRICT,
    candidate_id BIGINT REFERENCES discovery_candidates(id) ON DELETE SET NULL,
    site_id BIGINT REFERENCES discovery_sites(id) ON DELETE SET NULL,
    source_id BIGINT REFERENCES discovery_sources(id) ON DELETE SET NULL,
    domain_key TEXT NOT NULL DEFAULT '', source_name TEXT, discovery_method TEXT,
    original_url TEXT, source_page_url TEXT, title TEXT, summary TEXT, published_at TIMESTAMPTZ,
    metadata_confidence INTEGER NOT NULL DEFAULT 0, published_confidence TEXT,
    migration_uncertain BOOLEAN NOT NULL DEFAULT FALSE,
    first_seen_at TIMESTAMPTZ NOT NULL, last_seen_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_material_provenance_domain ON material_provenances(material_id, domain_key);
CREATE INDEX idx_material_provenance_candidate ON material_provenances(candidate_id);
CREATE INDEX idx_material_provenance_source ON material_provenances(source_id);
CREATE TABLE material_article_states (
    material_id BIGINT PRIMARY KEY REFERENCES material(id) ON DELETE CASCADE,
    content_version BIGINT NOT NULL DEFAULT 0, body_changed_at TIMESTAMPTZ,
    content_type TEXT, content_style TEXT, quality_score DOUBLE PRECISION NOT NULL DEFAULT 0,
    depth_score DOUBLE PRECISION NOT NULL DEFAULT 0,
    assessment_state TEXT NOT NULL DEFAULT 'pending', current_assessment_id BIGINT REFERENCES discovery_article_assessments(id),
    assessment_error TEXT, eligibility_state TEXT NOT NULL DEFAULT 'unknown', eligibility_reasons TEXT,
    enrichment_status TEXT, enrichment_error TEXT, llm_model TEXT, prompt_version TEXT, enriched_at TIMESTAMPTZ,
    score DOUBLE PRECISION NOT NULL DEFAULT 0, updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_material_article_eligibility ON material_article_states(eligibility_state, assessment_state);
CREATE TABLE material_candidate_versions (
    candidate_id BIGINT NOT NULL REFERENCES discovery_candidates(id) ON DELETE RESTRICT,
    content_version BIGINT NOT NULL, material_id BIGINT NOT NULL REFERENCES material(id) ON DELETE RESTRICT,
    version_id BIGINT NOT NULL REFERENCES material_versions(id) ON DELETE RESTRICT,
    PRIMARY KEY(candidate_id, content_version)
);
CREATE TABLE material_archive_links (
    task_id TEXT PRIMARY KEY, material_id BIGINT NOT NULL REFERENCES material(id) ON DELETE RESTRICT
);
CREATE TABLE material_embeddings (
    representation_id BIGINT NOT NULL REFERENCES material_representations(id) ON DELETE CASCADE,
    model TEXT NOT NULL, updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), PRIMARY KEY(representation_id, model)
);
-- +goose StatementBegin
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_type WHERE typname = 'vector') THEN
        ALTER TABLE material_embeddings ADD COLUMN embedding vector;
    END IF;
END $$;
-- +goose StatementEnd
CREATE TABLE material_migration_checkpoints (name TEXT PRIMARY KEY, completed_at TIMESTAMPTZ NOT NULL DEFAULT NOW());
ALTER TABLE discovery_candidates ADD COLUMN material_id BIGINT REFERENCES material(id) ON DELETE RESTRICT;
CREATE INDEX idx_discovery_candidates_material ON discovery_candidates(material_id);
ALTER TABLE archive_documents ADD COLUMN material_id BIGINT REFERENCES material(id) ON DELETE RESTRICT;
CREATE INDEX idx_archive_documents_material ON archive_documents(material_id);

-- Preserve immutable extraction IDs so existing owner gold labels stay valid.
ALTER TABLE discovery_article_content_versions ADD COLUMN material_id BIGINT REFERENCES material(id) ON DELETE RESTRICT;
ALTER TABLE discovery_article_content_versions ADD COLUMN material_version_id BIGINT REFERENCES material_versions(id) ON DELETE RESTRICT;
ALTER TABLE discovery_article_assessments ADD COLUMN material_id BIGINT REFERENCES material(id) ON DELETE RESTRICT;
ALTER TABLE discovery_article_assessments ADD COLUMN material_version_id BIGINT REFERENCES material_versions(id) ON DELETE RESTRICT;
ALTER TABLE recommendation_items ADD COLUMN material_id BIGINT REFERENCES material(id) ON DELETE RESTRICT;
ALTER TABLE recommendation_items ADD COLUMN material_version_id BIGINT REFERENCES material_versions(id) ON DELETE RESTRICT;
ALTER TABLE recommendation_items ADD COLUMN legacy_duplicate BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE recommendation_feedbacks ADD COLUMN material_id BIGINT REFERENCES material(id) ON DELETE RESTRICT;
ALTER TABLE assessment_llm_calls ADD COLUMN material_id BIGINT;
ALTER TABLE article_assessment_workflow_items ADD COLUMN material_id BIGINT REFERENCES material(id) ON DELETE RESTRICT;
ALTER TABLE article_assessment_workflow_items ADD COLUMN material_version_id BIGINT REFERENCES material_versions(id) ON DELETE RESTRICT;

-- +goose Down
-- Content merges cannot be reversed without the maintenance-window backup.
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Restore the pre-material database backup and binary to roll back'; END $$;
-- +goose StatementEnd
