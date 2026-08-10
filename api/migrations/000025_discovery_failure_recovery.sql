-- +goose Up
-- +goose StatementBegin

-- Older endpoint discovery treated WordPress REST and oEmbed declarations as
-- feeds because their media types contained JSON or XML. Preserve the source
-- and fetch history, but stop scheduling only those automatically discovered
-- oEmbed and core WordPress REST v2 metadata endpoints.
UPDATE discovery_sources
SET enabled = FALSE,
    next_fetch_at = NULL,
    next_due_at = NULL,
    backoff_until = NULL,
    backoff_reason = 'invalid_feed_metadata',
    last_error = 'quarantined automatically discovered WordPress metadata endpoint',
    updated_at = NOW()
WHERE user_managed = FALSE
  AND endpoint_type = 'feed'
  AND (
    LOWER(url) LIKE '%/wp-json/oembed/%'
    OR LOWER(url) LIKE '%/wp-json/wp/v2/%'
  );

-- The new fetch boundary accepts RSS 1.0 RDF and raises the bounded feed body
-- limit from two MiB to eight MiB. Give only sources affected by those two
-- compatibility changes one immediate clean attempt; fetch-run history remains
-- untouched for before/after analysis.
UPDATE discovery_sources
SET failure_count = 0,
    next_fetch_at = NOW(),
    next_due_at = NOW(),
    backoff_until = NULL,
    backoff_reason = '',
    last_error = '',
    updated_at = NOW()
WHERE enabled = TRUE
  AND endpoint_type IN ('feed', 'rsshub')
  AND (
    (backoff_reason = 'content_type' AND LOWER(last_error) LIKE '%application/rdf+xml%')
    OR backoff_reason = 'body_too_large'
  );

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

-- Re-enable only rows carrying this migration's quarantine marker. Failure
-- history in discovery_fetch_runs is intentionally retained.
UPDATE discovery_sources
SET enabled = TRUE,
    failure_count = 0,
    next_fetch_at = NOW(),
    next_due_at = NOW(),
    backoff_until = NULL,
    backoff_reason = '',
    last_error = '',
    updated_at = NOW()
WHERE user_managed = FALSE
  AND endpoint_type = 'feed'
  AND backoff_reason = 'invalid_feed_metadata';

-- +goose StatementEnd
