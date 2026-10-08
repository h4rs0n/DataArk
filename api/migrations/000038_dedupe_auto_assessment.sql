-- +goose Up
ALTER TABLE article_dedupe_runs ADD COLUMN auto_apply boolean NOT NULL DEFAULT false;
ALTER TABLE article_dedupe_settings ADD COLUMN pending_assessment boolean NOT NULL DEFAULT false;
-- +goose Down
ALTER TABLE article_dedupe_settings DROP COLUMN pending_assessment;
ALTER TABLE article_dedupe_runs DROP COLUMN auto_apply;
