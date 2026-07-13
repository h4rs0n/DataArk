-- +goose Up

CREATE TABLE IF NOT EXISTS discovery_legacy_candidate_state_reviews (
    id BIGSERIAL PRIMARY KEY,
    candidate_id BIGINT NOT NULL UNIQUE REFERENCES discovery_candidates(id) ON DELETE CASCADE,
    legacy_status VARCHAR(32) NOT NULL,
    resolution VARCHAR(32) NOT NULL DEFAULT 'pending',
    notes TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_discovery_legacy_candidate_state_reviews_legacy_status ON discovery_legacy_candidate_state_reviews(legacy_status);
CREATE INDEX IF NOT EXISTS idx_discovery_legacy_candidate_state_reviews_resolution ON discovery_legacy_candidate_state_reviews(resolution);

INSERT INTO discovery_legacy_candidate_state_reviews(candidate_id, legacy_status, resolution, notes, created_at, updated_at)
SELECT id, status, 'pending',
       'legacy global state has no reliable user identity; retained for owner review',
       COALESCE(first_seen_at, created_at, NOW()), COALESCE(updated_at, NOW())
FROM discovery_candidates
WHERE status IN ('read', 'ignored', 'archived')
ON CONFLICT (candidate_id) DO UPDATE
SET legacy_status = EXCLUDED.legacy_status,
    updated_at = EXCLUDED.updated_at;

-- +goose Down

-- Data-preserving rollback: per-user state and unresolved legacy-state review
-- rows remain available while older binaries continue reading legacy status.
SELECT 1;
