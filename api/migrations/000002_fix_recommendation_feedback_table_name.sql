-- +goose Up
-- +goose StatementBegin
DO $$
BEGIN
    IF to_regclass('public.recommendation_feedbacks') IS NULL THEN
        IF to_regclass('public.recommendation_feedback') IS NOT NULL THEN
            ALTER TABLE recommendation_feedback RENAME TO recommendation_feedbacks;
        ELSE
            CREATE TABLE recommendation_feedbacks (
                id BIGSERIAL PRIMARY KEY,
                user_id BIGINT NOT NULL,
                recommendation_item_id BIGINT NOT NULL REFERENCES recommendation_items(id) ON DELETE CASCADE,
                candidate_id BIGINT NOT NULL REFERENCES discovery_candidates(id) ON DELETE CASCADE,
                action VARCHAR(32) NOT NULL,
                metadata JSONB,
                created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
                reverted_at TIMESTAMPTZ
            );
        END IF;
    END IF;
END
$$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DO $$
BEGIN
    IF to_regclass('public.recommendation_feedbacks') IS NOT NULL
        AND to_regclass('public.recommendation_feedback') IS NULL THEN
        ALTER TABLE recommendation_feedbacks RENAME TO recommendation_feedback;
    END IF;
END
$$;
-- +goose StatementEnd
