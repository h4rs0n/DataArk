-- +goose Up
-- +goose StatementBegin

ALTER TABLE discovery_backfill_states
    ADD COLUMN IF NOT EXISTS owner_requested_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_discovery_backfill_states_owner_requested_at
    ON discovery_backfill_states (owner_requested_at);

UPDATE discovery_sources
SET enabled = FALSE,
    user_managed = FALSE,
    next_due_at = NULL,
    next_fetch_at = NULL,
    updated_at = CURRENT_TIMESTAMP
WHERE endpoint_type = 'sitemap' OR type = 'sitemap';

UPDATE discovery_backfill_states
SET status = 'paused',
    completion_reason = 'sitemap_requires_owner_request',
    next_batch_at = NULL,
    updated_at = CURRENT_TIMESTAMP
WHERE strategy = 'sitemap'
  AND owner_requested_at IS NULL;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

-- Data-preserving rollback: disabled endpoints, paused cursors and explicit
-- owner request evidence remain available for audit.
SELECT 1;

-- +goose StatementEnd
