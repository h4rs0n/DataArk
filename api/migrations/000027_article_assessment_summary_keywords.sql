-- +goose Up

-- 评估行增加摘要与关键字，供 article-value-v4 不可变落库；历史 v3 行保持为空。
ALTER TABLE discovery_article_assessments
    ADD COLUMN IF NOT EXISTS summary TEXT,
    ADD COLUMN IF NOT EXISTS keywords TEXT;

-- +goose Down

-- Data-preserving rollback: summary and keywords remain available to newer
-- binaries while older binaries ignore them.
SELECT 1;
