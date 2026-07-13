-- +goose Up

ALTER TABLE users
    ADD COLUMN IF NOT EXISTS role VARCHAR(32) NOT NULL DEFAULT 'member';

UPDATE users SET role = 'owner' WHERE username = 'admin' AND role <> 'owner';
CREATE INDEX IF NOT EXISTS idx_users_role ON users(role);

CREATE TABLE IF NOT EXISTS discovery_sites (
    id BIGSERIAL PRIMARY KEY,
    root_url VARCHAR(2048) NOT NULL,
    host_key VARCHAR(512) NOT NULL UNIQUE,
    display_name VARCHAR(255),
    status VARCHAR(32) NOT NULL DEFAULT 'observing',
    discovery_method VARCHAR(64) NOT NULL DEFAULT 'unknown',
    graph_depth INTEGER NOT NULL DEFAULT 0,
    crawl_allowed BOOLEAN NOT NULL DEFAULT TRUE,
    robots_status VARCHAR(32) NOT NULL DEFAULT 'unknown',
    first_discovered_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_referenced_at TIMESTAMPTZ,
    last_article_at TIMESTAMPTZ,
    last_validated_at TIMESTAMPTZ,
    next_graph_scan_at TIMESTAMPTZ,
    operational_pause VARCHAR(64),
    operational_details TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (status IN ('seed', 'observing', 'active', 'paused', 'blocked', 'non_blog'))
);

CREATE INDEX IF NOT EXISTS idx_discovery_sites_status ON discovery_sites(status);
CREATE INDEX IF NOT EXISTS idx_discovery_sites_graph_depth ON discovery_sites(graph_depth);
CREATE INDEX IF NOT EXISTS idx_discovery_sites_next_graph_scan_at ON discovery_sites(next_graph_scan_at);

ALTER TABLE discovery_sources
    ADD COLUMN IF NOT EXISTS site_id BIGINT,
    ADD COLUMN IF NOT EXISTS endpoint_type VARCHAR(32) NOT NULL DEFAULT 'legacy',
    ADD COLUMN IF NOT EXISTS priority INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS last_attempt_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS last_success_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS next_due_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS backoff_until TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS backoff_reason VARCHAR(64);

CREATE INDEX IF NOT EXISTS idx_discovery_sources_site_id ON discovery_sources(site_id);
CREATE INDEX IF NOT EXISTS idx_discovery_sources_endpoint_type ON discovery_sources(endpoint_type);
CREATE INDEX IF NOT EXISTS idx_discovery_sources_next_due_at ON discovery_sources(next_due_at);

-- +goose StatementBegin
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_discovery_sources_site') THEN
        ALTER TABLE discovery_sources
            ADD CONSTRAINT fk_discovery_sources_site
            FOREIGN KEY (site_id) REFERENCES discovery_sites(id) ON DELETE SET NULL;
    END IF;
END
$$;
-- +goose StatementEnd

ALTER TABLE discovery_candidates
    ADD COLUMN IF NOT EXISTS final_url VARCHAR(2048),
    ADD COLUMN IF NOT EXISTS content_version BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS body_changed_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS representative_id BIGINT,
    ADD COLUMN IF NOT EXISTS processing_state VARCHAR(32) NOT NULL DEFAULT 'discovered',
    ADD COLUMN IF NOT EXISTS eligibility_state VARCHAR(32) NOT NULL DEFAULT 'unknown',
    ADD COLUMN IF NOT EXISTS eligibility_reasons TEXT,
    ADD COLUMN IF NOT EXISTS first_seen_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_discovery_candidates_processing_state ON discovery_candidates(processing_state);
CREATE INDEX IF NOT EXISTS idx_discovery_candidates_eligibility_state ON discovery_candidates(eligibility_state);
CREATE INDEX IF NOT EXISTS idx_discovery_candidates_representative_id ON discovery_candidates(representative_id);
CREATE INDEX IF NOT EXISTS idx_discovery_candidates_first_seen_at ON discovery_candidates(first_seen_at);

-- +goose StatementBegin
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_discovery_candidates_representative') THEN
        ALTER TABLE discovery_candidates
            ADD CONSTRAINT fk_discovery_candidates_representative
            FOREIGN KEY (representative_id) REFERENCES discovery_candidates(id) ON DELETE SET NULL;
    END IF;
END
$$;
-- +goose StatementEnd

CREATE TABLE IF NOT EXISTS discovery_site_edges (
    id BIGSERIAL PRIMARY KEY,
    edge_key VARCHAR(64) NOT NULL UNIQUE,
    from_site_id BIGINT NOT NULL REFERENCES discovery_sites(id) ON DELETE CASCADE,
    to_site_id BIGINT NOT NULL REFERENCES discovery_sites(id) ON DELETE CASCADE,
    source_page_url VARCHAR(2048) NOT NULL,
    anchor_text VARCHAR(1024),
    relation_type VARCHAR(64) NOT NULL,
    evidence_summary TEXT,
    confidence DOUBLE PRECISION NOT NULL DEFAULT 0,
    first_seen_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_seen_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    active BOOLEAN NOT NULL DEFAULT TRUE,
    graph_depth INTEGER NOT NULL DEFAULT 0,
    pending_reason VARCHAR(64),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (from_site_id <> to_site_id OR relation_type <> '')
);

CREATE INDEX IF NOT EXISTS idx_discovery_site_edges_from_site_id ON discovery_site_edges(from_site_id);
CREATE INDEX IF NOT EXISTS idx_discovery_site_edges_to_site_id ON discovery_site_edges(to_site_id);
CREATE INDEX IF NOT EXISTS idx_discovery_site_edges_relation_type ON discovery_site_edges(relation_type);
CREATE INDEX IF NOT EXISTS idx_discovery_site_edges_last_seen_at ON discovery_site_edges(last_seen_at);

CREATE TABLE IF NOT EXISTS discovery_candidate_provenances (
    id BIGSERIAL PRIMARY KEY,
    provenance_key VARCHAR(64) NOT NULL UNIQUE,
    candidate_id BIGINT NOT NULL REFERENCES discovery_candidates(id) ON DELETE CASCADE,
    site_id BIGINT NOT NULL REFERENCES discovery_sites(id) ON DELETE CASCADE,
    source_id BIGINT REFERENCES discovery_sources(id) ON DELETE SET NULL,
    discovery_method VARCHAR(64) NOT NULL,
    original_url VARCHAR(2048) NOT NULL,
    source_page_url VARCHAR(2048),
    first_seen_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_seen_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_discovery_candidate_provenances_candidate_id ON discovery_candidate_provenances(candidate_id);
CREATE INDEX IF NOT EXISTS idx_discovery_candidate_provenances_site_id ON discovery_candidate_provenances(site_id);
CREATE INDEX IF NOT EXISTS idx_discovery_candidate_provenances_source_id ON discovery_candidate_provenances(source_id);

CREATE TABLE IF NOT EXISTS discovery_fetch_runs (
    id BIGSERIAL PRIMARY KEY,
    job_id VARCHAR(64),
    site_id BIGINT REFERENCES discovery_sites(id) ON DELETE SET NULL,
    source_id BIGINT NOT NULL REFERENCES discovery_sources(id) ON DELETE CASCADE,
    started_at TIMESTAMPTZ NOT NULL,
    finished_at TIMESTAMPTZ,
    status VARCHAR(32) NOT NULL,
    http_status INTEGER,
    not_modified BOOLEAN NOT NULL DEFAULT FALSE,
    new_count INTEGER NOT NULL DEFAULT 0,
    duplicate_count INTEGER NOT NULL DEFAULT 0,
    failure_count INTEGER NOT NULL DEFAULT 0,
    error_category VARCHAR(64),
    error_summary TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_discovery_fetch_runs_source_id ON discovery_fetch_runs(source_id);
CREATE INDEX IF NOT EXISTS idx_discovery_fetch_runs_site_id ON discovery_fetch_runs(site_id);
CREATE INDEX IF NOT EXISTS idx_discovery_fetch_runs_status ON discovery_fetch_runs(status);
CREATE INDEX IF NOT EXISTS idx_discovery_fetch_runs_started_at ON discovery_fetch_runs(started_at);

CREATE TABLE IF NOT EXISTS discovery_backfill_states (
    id BIGSERIAL PRIMARY KEY,
    site_id BIGINT NOT NULL REFERENCES discovery_sites(id) ON DELETE CASCADE,
    strategy VARCHAR(32) NOT NULL,
    cursor TEXT,
    batch_number BIGINT NOT NULL DEFAULT 0,
    status VARCHAR(32) NOT NULL DEFAULT 'pending',
    earliest_covered TIMESTAMPTZ,
    latest_covered TIMESTAMPTZ,
    urls_seen BIGINT NOT NULL DEFAULT 0,
    articles_found BIGINT NOT NULL DEFAULT 0,
    duplicate_count BIGINT NOT NULL DEFAULT 0,
    failure_count BIGINT NOT NULL DEFAULT 0,
    last_success_at TIMESTAMPTZ,
    next_batch_at TIMESTAMPTZ,
    completion_reason VARCHAR(128),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (site_id, strategy)
);

CREATE INDEX IF NOT EXISTS idx_discovery_backfill_states_status ON discovery_backfill_states(status);
CREATE INDEX IF NOT EXISTS idx_discovery_backfill_states_next_batch_at ON discovery_backfill_states(next_batch_at);

CREATE TABLE IF NOT EXISTS discovery_article_assessments (
    id BIGSERIAL PRIMARY KEY,
    candidate_id BIGINT NOT NULL REFERENCES discovery_candidates(id) ON DELETE CASCADE,
    content_version BIGINT NOT NULL,
    assessor VARCHAR(64) NOT NULL,
    assessor_version VARCHAR(64) NOT NULL,
    policy_version VARCHAR(64) NOT NULL,
    information_density DOUBLE PRECISION NOT NULL DEFAULT 0,
    originality DOUBLE PRECISION NOT NULL DEFAULT 0,
    completeness DOUBLE PRECISION NOT NULL DEFAULT 0,
    evidence DOUBLE PRECISION NOT NULL DEFAULT 0,
    readability DOUBLE PRECISION NOT NULL DEFAULT 0,
    depth DOUBLE PRECISION NOT NULL DEFAULT 0,
    evergreen_value DOUBLE PRECISION NOT NULL DEFAULT 0,
    overall_quality DOUBLE PRECISION NOT NULL DEFAULT 0,
    confidence DOUBLE PRECISION NOT NULL DEFAULT 0,
    reasons TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (candidate_id, content_version, assessor, assessor_version, policy_version)
);

CREATE INDEX IF NOT EXISTS idx_discovery_article_assessments_candidate_id ON discovery_article_assessments(candidate_id);
CREATE INDEX IF NOT EXISTS idx_discovery_article_assessments_overall_quality ON discovery_article_assessments(overall_quality);

CREATE TABLE IF NOT EXISTS user_candidate_states (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    candidate_id BIGINT NOT NULL REFERENCES discovery_candidates(id) ON DELETE CASCADE,
    first_exposed_at TIMESTAMPTZ,
    last_exposed_at TIMESTAMPTZ,
    exposure_count BIGINT NOT NULL DEFAULT 0,
    opened_at TIMESTAMPTZ,
    read_at TIMESTAMPTZ,
    deep_read_at TIMESTAMPTZ,
    archived_at TIMESTAMPTZ,
    current_feedback VARCHAR(32),
    feedback_set_at TIMESTAMPTZ,
    feedback_revoked TIMESTAMPTZ,
    migrated_from VARCHAR(64),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (user_id, candidate_id)
);

CREATE INDEX IF NOT EXISTS idx_user_candidate_states_last_exposed_at ON user_candidate_states(last_exposed_at);
CREATE INDEX IF NOT EXISTS idx_user_candidate_states_current_feedback ON user_candidate_states(current_feedback);

ALTER TABLE recommendation_days
    ADD COLUMN IF NOT EXISTS policy_version VARCHAR(64) NOT NULL DEFAULT 'v2',
    ADD COLUMN IF NOT EXISTS shortage_reasons TEXT,
    ADD COLUMN IF NOT EXISTS published_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS audit_version BIGINT NOT NULL DEFAULT 1;

ALTER TABLE recommendation_items
    ADD COLUMN IF NOT EXISTS snapshot_title VARCHAR(1024),
    ADD COLUMN IF NOT EXISTS snapshot_url VARCHAR(2048),
    ADD COLUMN IF NOT EXISTS snapshot_summary TEXT,
    ADD COLUMN IF NOT EXISTS snapshot_author VARCHAR(255),
    ADD COLUMN IF NOT EXISTS snapshot_source VARCHAR(255),
    ADD COLUMN IF NOT EXISTS snapshot_published_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS pool_type VARCHAR(32),
    ADD COLUMN IF NOT EXISTS exploration_reason TEXT,
    ADD COLUMN IF NOT EXISTS assessment_id BIGINT REFERENCES discovery_article_assessments(id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS profile_version BIGINT,
    ADD COLUMN IF NOT EXISTS model_version VARCHAR(255),
    ADD COLUMN IF NOT EXISTS supplemental BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS supplemented_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS audit_version BIGINT NOT NULL DEFAULT 1;

ALTER TABLE recommendation_items
    DROP CONSTRAINT IF EXISTS recommendation_items_user_id_candidate_id_key;
DROP INDEX IF EXISTS idx_recommendation_items_user_candidate;
DROP INDEX IF EXISTS uq_recommendation_user_dedupe;

CREATE INDEX IF NOT EXISTS idx_recommendation_items_user_id ON recommendation_items(user_id);
CREATE INDEX IF NOT EXISTS idx_recommendation_items_assessment_id ON recommendation_items(assessment_id);
CREATE INDEX IF NOT EXISTS idx_recommendation_items_pool_type ON recommendation_items(pool_type);

-- +goose Down
-- v3 is intentionally additive. Roll back by disabling v3 feature flags; keep
-- graph, provenance, assessment, per-user state and immutable snapshot data.
-- Destructive removal requires a separately reviewed migration after count
-- reconciliation, so Goose Down is a data-preserving no-op.
SELECT 1;
