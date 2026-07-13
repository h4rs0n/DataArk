-- +goose Up

ALTER TABLE discovery_fetch_runs
    ADD COLUMN IF NOT EXISTS final_url VARCHAR(2048),
    ADD COLUMN IF NOT EXISTS content_type VARCHAR(255),
    ADD COLUMN IF NOT EXISTS etag VARCHAR(1024),
    ADD COLUMN IF NOT EXISTS last_modified VARCHAR(1024),
    ADD COLUMN IF NOT EXISTS robots_status VARCHAR(32);

-- +goose Down
-- Fetch-run observability is retained on rollback to avoid deleting audit data.
SELECT 1;
