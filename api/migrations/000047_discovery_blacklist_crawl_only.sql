-- +goose Up
-- Crawl restrictions previously overwrote content eligibility. Retain the
-- assessment history, but let normal assessment recovery reactivate a valid
-- result using the configured model and quality threshold.
UPDATE material_article_states s
SET eligibility_state = 'unknown',
    eligibility_reasons = '',
    assessment_state = CASE WHEN EXISTS (
        SELECT 1 FROM material m JOIN material_representations r ON r.version_id = m.current_version_id
        WHERE m.id = s.material_id AND r.kind = 'text' AND r.role = 'body' AND r.text <> ''
    ) THEN 'pending' ELSE s.assessment_state END,
    current_assessment_id = NULL,
    updated_at = CURRENT_TIMESTAMP
WHERE s.eligibility_reasons = 'domain_blacklist';

UPDATE discovery_candidates
SET pending_content = pending_content || '{"eligibility_state":"unknown","eligibility_reasons":""}'::jsonb,
    updated_at = CURRENT_TIMESTAMP
WHERE pending_content->>'eligibility_reasons' = 'domain_blacklist';

-- Only accepted, extracted bodies can restore a candidate to ready. A body on
-- a historical placeholder alone is not evidence of successful extraction.
UPDATE discovery_candidates c
SET processing_state = 'ready',
    processing_error_type = '',
    processing_error = '',
    next_processing_at = NULL,
    dedupe_state = 'pending',
    updated_at = CURRENT_TIMESTAMP
FROM material m
WHERE c.material_id = m.id AND c.extracted_at IS NOT NULL
  AND c.processing_error_type = 'domain_blacklist'
  AND c.processing_state <> 'ready'
  AND EXISTS (SELECT 1 FROM material_representations r WHERE r.version_id = m.current_version_id
              AND r.kind = 'text' AND r.role = 'body' AND r.text <> '');

-- Keep candidates without accepted content in the crawl pipeline. This also
-- repairs redirects previously classified as content-ineligible.
UPDATE discovery_candidates c
SET processing_state = CASE WHEN EXISTS (
        SELECT 1 FROM discovery_domain_blacklist_entries b
        WHERE c.crawl_host = b.domain OR c.crawl_host LIKE '%.' || b.domain
    ) THEN 'domain_blocked' ELSE 'fetch_pending' END,
    processing_error_type = CASE WHEN EXISTS (
        SELECT 1 FROM discovery_domain_blacklist_entries b
        WHERE c.crawl_host = b.domain OR c.crawl_host LIKE '%.' || b.domain
    ) THEN 'domain_blacklist' ELSE '' END,
    processing_error = CASE WHEN EXISTS (
        SELECT 1 FROM discovery_domain_blacklist_entries b
        WHERE c.crawl_host = b.domain OR c.crawl_host LIKE '%.' || b.domain
    ) THEN c.processing_error ELSE '' END,
    next_processing_at = CASE WHEN EXISTS (
        SELECT 1 FROM discovery_domain_blacklist_entries b
        WHERE c.crawl_host = b.domain OR c.crawl_host LIKE '%.' || b.domain
    ) THEN NULL ELSE CURRENT_TIMESTAMP END,
    updated_at = CURRENT_TIMESTAMP
WHERE c.processing_error_type = 'domain_blacklist' AND c.processing_state <> 'ready';

-- +goose Down
-- This is a data repair. Downgrading must not reintroduce content restrictions
-- or discard assessments/content restored by the newer application.
SELECT 1;
