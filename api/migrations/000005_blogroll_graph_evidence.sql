-- +goose Up

ALTER TABLE discovery_site_edges
    ADD COLUMN IF NOT EXISTS detection_rule VARCHAR(64),
    ADD COLUMN IF NOT EXISTS context_summary TEXT;

CREATE INDEX IF NOT EXISTS idx_discovery_site_edges_detection_rule
    ON discovery_site_edges(detection_rule);

ALTER TABLE discovery_sites
    ADD COLUMN IF NOT EXISTS activated_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_discovery_sites_activated_at
    ON discovery_sites(activated_at);

-- +goose Down
-- Graph evidence is retained on rollback to preserve discovery provenance.
SELECT 1;
