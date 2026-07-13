-- +goose Up

ALTER TABLE discovery_candidates
    ADD COLUMN IF NOT EXISTS processing_attempts BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS processing_error TEXT,
    ADD COLUMN IF NOT EXISTS processing_error_type VARCHAR(64),
    ADD COLUMN IF NOT EXISTS next_processing_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS fetched_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS extracted_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS dedupe_state VARCHAR(32) NOT NULL DEFAULT 'pending',
    ADD COLUMN IF NOT EXISTS assessment_state VARCHAR(32) NOT NULL DEFAULT 'pending';

CREATE INDEX IF NOT EXISTS idx_discovery_candidates_processing_error_type ON discovery_candidates(processing_error_type);
CREATE INDEX IF NOT EXISTS idx_discovery_candidates_next_processing_at ON discovery_candidates(next_processing_at);
CREATE INDEX IF NOT EXISTS idx_discovery_candidates_dedupe_state ON discovery_candidates(dedupe_state);
CREATE INDEX IF NOT EXISTS idx_discovery_candidates_assessment_state ON discovery_candidates(assessment_state);

UPDATE discovery_candidates
SET processing_state = 'fetch_pending',
    eligibility_state = 'unknown',
    updated_at = NOW()
WHERE processing_state IS NULL OR processing_state = '' OR processing_state = 'discovered';

CREATE TABLE IF NOT EXISTS discovery_article_content_versions (
    id BIGSERIAL PRIMARY KEY,
    candidate_id BIGINT NOT NULL REFERENCES discovery_candidates(id) ON DELETE CASCADE,
    content_version BIGINT NOT NULL,
    content_hash VARCHAR(128) NOT NULL,
    final_url VARCHAR(2048),
    canonical_url VARCHAR(2048),
    title VARCHAR(1024),
    summary TEXT,
    author VARCHAR(255),
    body_text TEXT,
    language VARCHAR(32),
    word_count INTEGER NOT NULL DEFAULT 0,
    published_at TIMESTAMPTZ,
    fetched_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (candidate_id, content_version)
);

CREATE INDEX IF NOT EXISTS idx_discovery_article_content_versions_candidate_id ON discovery_article_content_versions(candidate_id);
CREATE INDEX IF NOT EXISTS idx_discovery_article_content_versions_content_hash ON discovery_article_content_versions(content_hash);

-- +goose Down

-- Data-preserving rollback: older binaries ignore article processing audit
-- columns and immutable extracted-content versions.
SELECT 1;
