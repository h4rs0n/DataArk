-- +goose Up
-- Candidate references below are historical entry points. Material is the owner.
-- +goose StatementBegin
DO $$
DECLARE relation_name TEXT; constraint_name TEXT;
BEGIN
    FOREACH relation_name IN ARRAY ARRAY['user_material_states', 'discovery_article_assessments', 'recommendation_items', 'recommendation_feedbacks'] LOOP
        FOR constraint_name IN SELECT conname FROM pg_constraint
            WHERE conrelid = relation_name::regclass AND contype = 'f' AND confrelid = 'discovery_candidates'::regclass LOOP
            EXECUTE format('ALTER TABLE %I DROP CONSTRAINT %I', relation_name, constraint_name);
        END LOOP;
        EXECUTE format('ALTER TABLE %I ALTER COLUMN candidate_id DROP NOT NULL', relation_name);
        EXECUTE format('ALTER TABLE %I ADD CONSTRAINT %I FOREIGN KEY(candidate_id) REFERENCES discovery_candidates(id) ON DELETE SET NULL', relation_name, relation_name || '_candidate_audit');
    END LOOP;
END $$;
-- +goose StatementEnd
ALTER TABLE user_material_states DROP CONSTRAINT IF EXISTS user_candidate_states_user_id_candidate_id_key;
DROP INDEX IF EXISTS idx_user_candidate_state;
CREATE UNIQUE INDEX idx_material_assessment_version ON discovery_article_assessments(material_id, content_version, assessor, assessor_version, policy_version);
CREATE INDEX idx_material_assessment_owner ON discovery_article_assessments(material_id, material_version_id);
CREATE INDEX idx_recommendation_item_material ON recommendation_items(material_id);
CREATE INDEX idx_recommendation_feedback_material ON recommendation_feedbacks(material_id);
INSERT INTO material_article_states(material_id)
SELECT id FROM material ON CONFLICT(material_id) DO NOTHING;
CREATE TABLE material_ingestion_issues (
    archive_document_id BIGINT PRIMARY KEY REFERENCES archive_documents(id) ON DELETE CASCADE,
    error TEXT NOT NULL, updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Restore the pre-material backup and binary'; END $$;
-- +goose StatementEnd
