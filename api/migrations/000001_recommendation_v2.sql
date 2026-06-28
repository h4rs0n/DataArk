-- +goose Up
-- +goose StatementBegin
DO $$
BEGIN
    CREATE EXTENSION IF NOT EXISTS vector;
EXCEPTION
    WHEN undefined_file THEN
        RAISE NOTICE 'pgvector extension is not installed; recommendation embedding column will be skipped';
END
$$;
-- +goose StatementEnd

ALTER TABLE discovery_sources
    ADD COLUMN IF NOT EXISTS etag VARCHAR(1024),
    ADD COLUMN IF NOT EXISTS last_modified VARCHAR(1024),
    ADD COLUMN IF NOT EXISTS failure_count INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS next_fetch_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS crawl_config JSONB;

ALTER TABLE discovery_sources
    ALTER COLUMN crawl_config TYPE JSONB
    USING CASE WHEN crawl_config IS NULL OR crawl_config::TEXT = '' THEN NULL ELSE crawl_config::JSONB END;

ALTER TABLE discovery_candidates
    ADD COLUMN IF NOT EXISTS canonical_url VARCHAR(2048),
    ADD COLUMN IF NOT EXISTS normalized_url VARCHAR(2048),
    ADD COLUMN IF NOT EXISTS author VARCHAR(255),
    ADD COLUMN IF NOT EXISTS body_text TEXT,
    ADD COLUMN IF NOT EXISTS language VARCHAR(32),
    ADD COLUMN IF NOT EXISTS word_count INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS content_hash VARCHAR(128),
    ADD COLUMN IF NOT EXISTS dedupe_key VARCHAR(128),
    ADD COLUMN IF NOT EXISTS duplicate_cluster_id VARCHAR(128),
    ADD COLUMN IF NOT EXISTS topics JSONB,
    ADD COLUMN IF NOT EXISTS entities JSONB,
    ADD COLUMN IF NOT EXISTS content_type VARCHAR(64),
    ADD COLUMN IF NOT EXISTS content_style VARCHAR(64),
    ADD COLUMN IF NOT EXISTS quality_score DOUBLE PRECISION NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS depth_score DOUBLE PRECISION NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS enrichment_status VARCHAR(32),
    ADD COLUMN IF NOT EXISTS enrichment_error TEXT,
    ADD COLUMN IF NOT EXISTS embedding_model VARCHAR(255),
    ADD COLUMN IF NOT EXISTS llm_model VARCHAR(255),
    ADD COLUMN IF NOT EXISTS prompt_version VARCHAR(64),
    ADD COLUMN IF NOT EXISTS enriched_at TIMESTAMPTZ;

ALTER TABLE discovery_candidates
    ALTER COLUMN topics TYPE JSONB
    USING CASE WHEN topics IS NULL OR topics::TEXT = '' THEN NULL ELSE topics::JSONB END,
    ALTER COLUMN entities TYPE JSONB
    USING CASE WHEN entities IS NULL OR entities::TEXT = '' THEN NULL ELSE entities::JSONB END;

-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_type WHERE typname = 'vector') THEN
        ALTER TABLE discovery_candidates ADD COLUMN IF NOT EXISTS embedding vector;
    END IF;
END
$$;
-- +goose StatementEnd

CREATE TABLE IF NOT EXISTS recommendation_settings (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL UNIQUE,
    daily_limit INTEGER NOT NULL DEFAULT 10,
    timezone VARCHAR(64) NOT NULL,
    generation_time VARCHAR(16) NOT NULL,
    candidate_window_days INTEGER NOT NULL DEFAULT 30,
    exploration_rate DOUBLE PRECISION NOT NULL DEFAULT 0.15,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS recommendation_days (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL,
    recommendation_date DATE NOT NULL,
    timezone VARCHAR(64) NOT NULL,
    status VARCHAR(32) NOT NULL,
    requested_count INTEGER NOT NULL DEFAULT 10,
    actual_count INTEGER NOT NULL DEFAULT 0,
    profile_version BIGINT,
    llm_model VARCHAR(255),
    prompt_version VARCHAR(64),
    generated_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (user_id, recommendation_date)
);

CREATE TABLE IF NOT EXISTS recommendation_items (
    id BIGSERIAL PRIMARY KEY,
    day_id BIGINT NOT NULL REFERENCES recommendation_days(id) ON DELETE CASCADE,
    user_id BIGINT NOT NULL,
    candidate_id BIGINT NOT NULL REFERENCES discovery_candidates(id) ON DELETE CASCADE,
    dedupe_key VARCHAR(128),
    rank INTEGER NOT NULL,
    retrieval_score DOUBLE PRECISION,
    rerank_score DOUBLE PRECISION,
    final_score DOUBLE PRECISION,
    reason TEXT,
    reason_metadata JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (day_id, candidate_id),
    UNIQUE (user_id, candidate_id)
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_recommendation_user_dedupe
ON recommendation_items(user_id, dedupe_key)
WHERE dedupe_key IS NOT NULL;

CREATE TABLE IF NOT EXISTS recommendation_feedback (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL,
    recommendation_item_id BIGINT NOT NULL REFERENCES recommendation_items(id) ON DELETE CASCADE,
    candidate_id BIGINT NOT NULL REFERENCES discovery_candidates(id) ON DELETE CASCADE,
    action VARCHAR(32) NOT NULL,
    metadata JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    reverted_at TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS user_block_rules (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL,
    rule_type VARCHAR(32) NOT NULL,
    rule_value VARCHAR(255) NOT NULL,
    active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_user_block_rules_lookup
ON user_block_rules(user_id, active, rule_type, rule_value);

CREATE TABLE IF NOT EXISTS user_recommendation_profiles (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL UNIQUE,
    positive_embedding JSONB,
    negative_embedding JSONB,
    topic_weights JSONB,
    source_weights JSONB,
    style_weights JSONB,
    depth_preference DOUBLE PRECISION,
    exploration_rate DOUBLE PRECISION,
    profile_version BIGINT NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- +goose Down
DROP TABLE IF EXISTS user_recommendation_profiles;
DROP INDEX IF EXISTS idx_user_block_rules_lookup;
DROP TABLE IF EXISTS user_block_rules;
DROP TABLE IF EXISTS recommendation_feedback;
DROP INDEX IF EXISTS uq_recommendation_user_dedupe;
DROP TABLE IF EXISTS recommendation_items;
DROP TABLE IF EXISTS recommendation_days;
DROP TABLE IF EXISTS recommendation_settings;

ALTER TABLE discovery_candidates
    DROP COLUMN IF EXISTS enriched_at,
    DROP COLUMN IF EXISTS prompt_version,
    DROP COLUMN IF EXISTS llm_model,
    DROP COLUMN IF EXISTS embedding_model,
    DROP COLUMN IF EXISTS enrichment_error,
    DROP COLUMN IF EXISTS enrichment_status,
    DROP COLUMN IF EXISTS embedding,
    DROP COLUMN IF EXISTS depth_score,
    DROP COLUMN IF EXISTS quality_score,
    DROP COLUMN IF EXISTS content_style,
    DROP COLUMN IF EXISTS content_type,
    DROP COLUMN IF EXISTS entities,
    DROP COLUMN IF EXISTS topics,
    DROP COLUMN IF EXISTS duplicate_cluster_id,
    DROP COLUMN IF EXISTS dedupe_key,
    DROP COLUMN IF EXISTS content_hash,
    DROP COLUMN IF EXISTS word_count,
    DROP COLUMN IF EXISTS language,
    DROP COLUMN IF EXISTS body_text,
    DROP COLUMN IF EXISTS author,
    DROP COLUMN IF EXISTS normalized_url,
    DROP COLUMN IF EXISTS canonical_url;

ALTER TABLE discovery_sources
    DROP COLUMN IF EXISTS crawl_config,
    DROP COLUMN IF EXISTS next_fetch_at,
    DROP COLUMN IF EXISTS failure_count,
    DROP COLUMN IF EXISTS last_modified,
    DROP COLUMN IF EXISTS etag;
