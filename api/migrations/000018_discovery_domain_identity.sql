-- +goose Up
-- +goose StatementBegin

ALTER TABLE discovery_sites
    ADD COLUMN IF NOT EXISTS domain_key VARCHAR(512) NOT NULL DEFAULT '';

UPDATE discovery_sites
SET domain_key = LOWER(host_key)
WHERE domain_key = '';

CREATE INDEX IF NOT EXISTS idx_discovery_sites_domain_key
    ON discovery_sites (domain_key);

-- Public Suffix List normalization is intentionally completed by the
-- idempotent Go compatibility backfill after this additive migration.

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

-- Data-preserving rollback: domain identity remains available to older
-- binaries and is not removed automatically.
SELECT 1;

-- +goose StatementEnd
