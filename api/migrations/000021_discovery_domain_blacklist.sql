-- +goose Up
-- +goose StatementBegin

CREATE TABLE IF NOT EXISTS discovery_domain_blacklist_entries (
    id BIGSERIAL PRIMARY KEY,
    domain VARCHAR(512) NOT NULL UNIQUE,
    reason TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

ALTER TABLE discovery_sources
    ADD COLUMN IF NOT EXISTS crawl_host VARCHAR(512) NOT NULL DEFAULT '';

ALTER TABLE discovery_candidates
    ADD COLUMN IF NOT EXISTS crawl_host VARCHAR(512) NOT NULL DEFAULT '';

UPDATE discovery_sources
SET crawl_host = LOWER(TRIM(TRAILING '.' FROM REGEXP_REPLACE(
    REGEXP_REPLACE(SPLIT_PART(SPLIT_PART(url, '://', 2), '/', 1), '^.*@', ''),
    ':[0-9]+$', ''
)))
WHERE crawl_host = '';

UPDATE discovery_candidates
SET crawl_host = LOWER(TRIM(TRAILING '.' FROM REGEXP_REPLACE(
    REGEXP_REPLACE(SPLIT_PART(SPLIT_PART(url, '://', 2), '/', 1), '^.*@', ''),
    ':[0-9]+$', ''
)))
WHERE crawl_host = '';

CREATE INDEX IF NOT EXISTS idx_discovery_sources_crawl_host
    ON discovery_sources (crawl_host);

CREATE INDEX IF NOT EXISTS idx_discovery_candidates_crawl_host
    ON discovery_candidates (crawl_host);

INSERT INTO discovery_domain_blacklist_entries (domain, reason)
VALUES ('csdn.net', '默认屏蔽：该域名批量抓取持续返回 HTTP 521')
ON CONFLICT (domain) DO NOTHING;

UPDATE discovery_candidates
SET processing_state = 'domain_blocked',
    processing_error_type = 'domain_blacklist',
    processing_error = 'crawl domain is blacklisted: csdn.net',
    eligibility_state = 'unknown',
    eligibility_reasons = 'domain_blacklist',
    next_processing_at = NULL,
    updated_at = CURRENT_TIMESTAMP
WHERE (crawl_host = 'csdn.net' OR crawl_host LIKE '%.csdn.net')
  AND processing_state IN ('discovered', 'fetch_pending', 'fetching', 'extract_pending');

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP INDEX IF EXISTS idx_discovery_candidates_crawl_host;
DROP INDEX IF EXISTS idx_discovery_sources_crawl_host;
ALTER TABLE discovery_candidates DROP COLUMN IF EXISTS crawl_host;
ALTER TABLE discovery_sources DROP COLUMN IF EXISTS crawl_host;
DROP TABLE IF EXISTS discovery_domain_blacklist_entries;

-- +goose StatementEnd
