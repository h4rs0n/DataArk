-- +goose Up

-- 关闭残留 Sitemap 来源并暂停 sitemap 回溯；不删除历史候选。
UPDATE discovery_sources
SET enabled = FALSE,
    next_due_at = NULL,
    next_fetch_at = NULL,
    updated_at = CURRENT_TIMESTAMP
WHERE endpoint_type = 'sitemap' OR type = 'sitemap';

UPDATE discovery_backfill_states
SET status = 'paused',
    completion_reason = 'sitemap_removed',
    next_batch_at = NULL,
    updated_at = CURRENT_TIMESTAMP
WHERE strategy = 'sitemap'
  AND status <> 'completed';

-- +goose Down

-- Data-preserving rollback: disabled sitemap sources and paused cursors remain.
SELECT 1;
