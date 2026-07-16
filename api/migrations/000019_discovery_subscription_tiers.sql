-- +goose Up
-- +goose StatementBegin

ALTER TABLE discovery_sources
    ADD COLUMN IF NOT EXISTS user_managed BOOLEAN NOT NULL DEFAULT FALSE;

CREATE INDEX IF NOT EXISTS idx_discovery_sources_user_managed
    ON discovery_sources (user_managed);

WITH manual_representatives AS (
    SELECT MIN(source.id) AS source_id
    FROM discovery_sources AS source
    JOIN discovery_sites AS site ON site.id = source.site_id
    WHERE site.discovery_method IN ('manual_seed', 'legacy_source')
       OR site.status = 'seed'
    GROUP BY source.site_id
)
UPDATE discovery_sources
SET user_managed = TRUE
WHERE id IN (SELECT source_id FROM manual_representatives);

UPDATE discovery_sources AS source
SET priority = CASE
    WHEN site.discovery_method IN ('manual_seed', 'legacy_source') OR site.status = 'seed' THEN 2000
    ELSE 1000
END
FROM discovery_sites AS site
WHERE source.site_id = site.id;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

-- Data-preserving rollback: subscription ownership remains available to older
-- binaries and is not removed automatically.
SELECT 1;

-- +goose StatementEnd
