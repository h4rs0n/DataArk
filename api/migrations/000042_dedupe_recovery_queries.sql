-- +goose Up
-- IS TRUE in the representative gate uses these expression statistics instead
-- of estimating two same-table IDs as unrelated values (~0.5% selectivity).
CREATE STATISTICS article_dedupe_representative_stats
 ON (representation_id=representative_id), applied, run_id FROM article_dedupe_members;
CREATE INDEX idx_article_dedupe_current_representatives
 ON article_dedupe_members(run_id, material_id, version_id)
 INCLUDE (representation_id, candidate_id)
 WHERE applied AND (representation_id=representative_id) IS TRUE;
ANALYZE article_dedupe_members;

-- +goose Down
DROP INDEX idx_article_dedupe_current_representatives;
DROP STATISTICS article_dedupe_representative_stats;
