-- +goose Up

CREATE INDEX IF NOT EXISTS idx_discovery_candidates_normalized_url ON discovery_candidates(normalized_url);
CREATE INDEX IF NOT EXISTS idx_discovery_candidates_canonical_url ON discovery_candidates(canonical_url);
CREATE INDEX IF NOT EXISTS idx_discovery_candidates_final_url ON discovery_candidates(final_url);
CREATE INDEX IF NOT EXISTS idx_discovery_candidates_content_hash ON discovery_candidates(content_hash);
CREATE INDEX IF NOT EXISTS idx_discovery_candidates_duplicate_cluster_id ON discovery_candidates(duplicate_cluster_id);

CREATE TABLE IF NOT EXISTS discovery_duplicate_clusters (
    cluster_id VARCHAR(128) PRIMARY KEY,
    representative_id BIGINT NOT NULL REFERENCES discovery_candidates(id) ON DELETE RESTRICT,
    match_method VARCHAR(32) NOT NULL,
    representative_reason TEXT,
    member_count BIGINT NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_discovery_duplicate_clusters_representative_id ON discovery_duplicate_clusters(representative_id);
CREATE INDEX IF NOT EXISTS idx_discovery_duplicate_clusters_match_method ON discovery_duplicate_clusters(match_method);

CREATE TABLE IF NOT EXISTS discovery_candidate_identities (
    id BIGSERIAL PRIMARY KEY,
    candidate_id BIGINT NOT NULL REFERENCES discovery_candidates(id) ON DELETE CASCADE,
    kind VARCHAR(32) NOT NULL,
    identity_key VARCHAR(128) NOT NULL,
    value TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (candidate_id, kind, identity_key)
);

CREATE INDEX IF NOT EXISTS idx_discovery_candidate_identities_candidate_id ON discovery_candidate_identities(candidate_id);
CREATE INDEX IF NOT EXISTS idx_discovery_candidate_identities_kind ON discovery_candidate_identities(kind);
CREATE INDEX IF NOT EXISTS idx_discovery_candidate_identities_identity_key ON discovery_candidate_identities(identity_key);

CREATE TABLE IF NOT EXISTS discovery_duplicate_review_signals (
    id BIGSERIAL PRIMARY KEY,
    candidate_id BIGINT NOT NULL REFERENCES discovery_candidates(id) ON DELETE CASCADE,
    reporter_user_id BIGINT NOT NULL,
    recommendation_item_id BIGINT NOT NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'pending',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_discovery_duplicate_review_signals_candidate_id ON discovery_duplicate_review_signals(candidate_id);
CREATE INDEX IF NOT EXISTS idx_discovery_duplicate_review_signals_reporter_user_id ON discovery_duplicate_review_signals(reporter_user_id);
CREATE INDEX IF NOT EXISTS idx_discovery_duplicate_review_signals_recommendation_item_id ON discovery_duplicate_review_signals(recommendation_item_id);
CREATE INDEX IF NOT EXISTS idx_discovery_duplicate_review_signals_status ON discovery_duplicate_review_signals(status);

-- +goose Down

-- Data-preserving rollback: aliases, clusters, representative explanations,
-- and duplicate-review signals remain available to newer binaries.
SELECT 1;
