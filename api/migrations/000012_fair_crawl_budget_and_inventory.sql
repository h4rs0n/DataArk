-- +goose Up

CREATE TABLE IF NOT EXISTS discovery_site_operational_stats (
    id BIGSERIAL PRIMARY KEY,
    site_id BIGINT NOT NULL UNIQUE REFERENCES discovery_sites(id) ON DELETE CASCADE,
    independent_inbound_sites BIGINT NOT NULL DEFAULT 0,
    fetch_attempts BIGINT NOT NULL DEFAULT 0,
    fetch_successes BIGINT NOT NULL DEFAULT 0,
    not_modified_fetches BIGINT NOT NULL DEFAULT 0,
    parse_successes BIGINT NOT NULL DEFAULT 0,
    candidate_count BIGINT NOT NULL DEFAULT 0,
    eligible_candidate_count BIGINT NOT NULL DEFAULT 0,
    duplicate_candidate_count BIGINT NOT NULL DEFAULT 0,
    extracted_candidate_count BIGINT NOT NULL DEFAULT 0,
    positive_feedback_articles BIGINT NOT NULL DEFAULT 0,
    backfill_urls_seen BIGINT NOT NULL DEFAULT 0,
    backfill_articles_found BIGINT NOT NULL DEFAULT 0,
    last_computed_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_discovery_site_operational_stats_last_computed_at ON discovery_site_operational_stats(last_computed_at);

CREATE TABLE IF NOT EXISTS discovery_source_schedule_decisions (
    id BIGSERIAL PRIMARY KEY,
    source_id BIGINT NOT NULL UNIQUE REFERENCES discovery_sources(id) ON DELETE CASCADE,
    site_id BIGINT REFERENCES discovery_sites(id) ON DELETE SET NULL,
    basis VARCHAR(32) NOT NULL,
    base_interval_seconds BIGINT NOT NULL DEFAULT 0,
    chosen_interval_seconds BIGINT NOT NULL DEFAULT 0,
    explanation TEXT,
    next_due_at TIMESTAMPTZ NOT NULL,
    computed_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_discovery_source_schedule_decisions_site_id ON discovery_source_schedule_decisions(site_id);
CREATE INDEX IF NOT EXISTS idx_discovery_source_schedule_decisions_basis ON discovery_source_schedule_decisions(basis);
CREATE INDEX IF NOT EXISTS idx_discovery_source_schedule_decisions_next_due_at ON discovery_source_schedule_decisions(next_due_at);

-- Named health, graph, yield and coverage counters remain independent. There
-- is deliberately no source quality score and no eligibility multiplier.

-- +goose Down

-- Data-preserving rollback: operational counters and scheduling explanations
-- remain available for audit while older binaries ignore these additive tables.
SELECT 1;
