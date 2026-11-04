-- +goose Up
CREATE TABLE search_sync_pending (
    material_id BIGINT PRIMARY KEY,
    revision BIGINT NOT NULL DEFAULT 1,
    attempts INT NOT NULL DEFAULT 0,
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    error TEXT NOT NULL DEFAULT ''
);
CREATE INDEX search_sync_pending_due ON search_sync_pending(next_attempt_at, material_id);
CREATE TABLE search_sync_state (
    id INT PRIMARY KEY CHECK (id = 1),
    phase TEXT NOT NULL DEFAULT 'prepare',
    generation TEXT NOT NULL DEFAULT '',
    cursor BIGINT NOT NULL DEFAULT 0,
    scanned BIGINT NOT NULL DEFAULT 0,
    last_success_at TIMESTAMPTZ,
    error TEXT NOT NULL DEFAULT ''
);
INSERT INTO search_sync_state(id) VALUES(1);

-- Deliberately no FK: deleted/redirected material IDs still need index deletion.
-- +goose StatementBegin
CREATE FUNCTION enqueue_search_material(target BIGINT) RETURNS VOID LANGUAGE plpgsql AS $$
BEGIN
    IF target IS NULL THEN RETURN; END IF;
    INSERT INTO search_sync_pending(material_id) VALUES(target)
    ON CONFLICT(material_id) DO UPDATE SET revision = search_sync_pending.revision + 1,
        attempts = 0, next_attempt_at = NOW(), error = '';
END;
$$;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE FUNCTION search_material_changed() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE previous_id BIGINT; current_id BIGINT;
BEGIN
    IF TG_OP <> 'INSERT' THEN
        IF TG_TABLE_NAME IN ('material', 'material_redirects') THEN previous_id := OLD.id; ELSE previous_id := OLD.material_id; END IF;
        PERFORM enqueue_search_material(previous_id);
    END IF;
    IF TG_OP <> 'DELETE' THEN
        IF TG_TABLE_NAME IN ('material', 'material_redirects') THEN current_id := NEW.id; ELSE current_id := NEW.material_id; END IF;
        IF current_id IS DISTINCT FROM previous_id THEN PERFORM enqueue_search_material(current_id); END IF;
        IF TG_TABLE_NAME = 'material_redirects' THEN PERFORM enqueue_search_material(NEW.material_id); END IF;
    END IF;
    RETURN NULL;
END;
$$;
-- +goose StatementEnd
-- +goose StatementBegin
DO $$
DECLARE relation TEXT;
BEGIN
    FOREACH relation IN ARRAY ARRAY['material','material_article_states','archive_documents','material_identities','material_redirects'] LOOP
        EXECUTE format('CREATE TRIGGER search_sync_changed AFTER INSERT OR UPDATE OR DELETE ON %I FOR EACH ROW EXECUTE FUNCTION search_material_changed()', relation);
    END LOOP;
END;
$$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DO $$
DECLARE relation TEXT;
BEGIN
    FOREACH relation IN ARRAY ARRAY['material','material_article_states','archive_documents','material_identities','material_redirects'] LOOP
        EXECUTE format('DROP TRIGGER search_sync_changed ON %I', relation);
    END LOOP;
END;
$$;
-- +goose StatementEnd
DROP FUNCTION search_material_changed();
DROP FUNCTION enqueue_search_material(BIGINT);
DROP TABLE search_sync_pending;
DROP TABLE search_sync_state;
