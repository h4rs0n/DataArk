-- +goose Up
CREATE INDEX idx_river_assessment_material_live ON river_job ((args->>'material_id'), kind)
 WHERE state IN ('available','pending','scheduled','retryable','running') AND COALESCE((args->>'candidate_id')::bigint,0)=0;
CREATE TABLE article_dedupe_fingerprints (
 representation_id bigint NOT NULL REFERENCES material_representations(id) ON DELETE CASCADE,
 algorithm varchar(64) NOT NULL, signature bytea NOT NULL, numbers_hash varchar(64) NOT NULL, characters integer NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(), PRIMARY KEY(representation_id, algorithm)
);
CREATE TABLE article_dedupe_buckets (
 algorithm varchar(64) NOT NULL, band integer NOT NULL, hash varchar(32) NOT NULL,
 representation_id bigint NOT NULL REFERENCES material_representations(id) ON DELETE CASCADE,
 PRIMARY KEY(algorithm, band, hash, representation_id)
);
CREATE INDEX idx_article_dedupe_bucket ON article_dedupe_buckets(algorithm, band, hash);
CREATE TABLE article_dedupe_runs (
 id bigserial PRIMARY KEY, algorithm text NOT NULL, status text NOT NULL,
 high_water_id bigint NOT NULL DEFAULT 0, cursor bigint NOT NULL DEFAULT 0, previous_run_id bigint NOT NULL DEFAULT 0,
 indexed bigint NOT NULL DEFAULT 0, processed bigint NOT NULL DEFAULT 0,
 applied bigint NOT NULL DEFAULT 0, cancelled bigint NOT NULL DEFAULT 0,
 error text NOT NULL DEFAULT '', created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_article_dedupe_runs_status ON article_dedupe_runs(status);
CREATE UNIQUE INDEX idx_article_dedupe_one_build ON article_dedupe_runs(algorithm)
 WHERE status IN ('indexing','previewing','applying','rolling_back');
CREATE TABLE article_dedupe_members (
 run_id bigint NOT NULL REFERENCES article_dedupe_runs(id),
 representation_id bigint NOT NULL REFERENCES material_representations(id),
 material_id bigint NOT NULL REFERENCES material(id), version_id bigint NOT NULL REFERENCES material_versions(id),
 candidate_id bigint NOT NULL DEFAULT 0, representative_id bigint NOT NULL REFERENCES material_representations(id),
 similarity double precision NOT NULL, method text NOT NULL, revision bigint NOT NULL DEFAULT 1,
 baseline_pending boolean NOT NULL DEFAULT false, candidate_count integer NOT NULL DEFAULT 0, duration_ms bigint NOT NULL DEFAULT 0,
 applied boolean NOT NULL DEFAULT false, updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(run_id,representation_id)
);
CREATE INDEX idx_article_dedupe_members_rep ON article_dedupe_members(run_id,representative_id);
CREATE INDEX idx_article_dedupe_members_material ON article_dedupe_members(material_id);
CREATE INDEX idx_article_dedupe_members_unapplied ON article_dedupe_members(run_id) WHERE NOT applied;
CREATE TABLE article_dedupe_changes (
 id bigserial PRIMARY KEY, run_id bigint NOT NULL REFERENCES article_dedupe_runs(id),
 candidate_id bigint NOT NULL REFERENCES discovery_candidates(id), version_id bigint NOT NULL,
 revision bigint NOT NULL, before text NOT NULL, after text NOT NULL,
 UNIQUE(run_id,candidate_id)
);
CREATE TABLE article_dedupe_settings (id bigint PRIMARY KEY, active_run_id bigint NOT NULL DEFAULT 0, mode text NOT NULL DEFAULT 'shadow');
CREATE INDEX idx_discovery_candidates_dedupe_pending ON discovery_candidates(id)
 WHERE processing_state='ready' AND dedupe_state<>'ready';
INSERT INTO article_dedupe_settings(id) VALUES(1);
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN
    -- article_dedupe_changes is the only log that can undo applied projections.
    -- Dropping it while duplicate_cluster_id/representative_id/eligibility_state
    -- are still applied would suppress those articles permanently.
    IF EXISTS (SELECT 1 FROM article_dedupe_changes)
       OR EXISTS (SELECT 1 FROM discovery_candidates WHERE duplicate_cluster_id LIKE 'idx-%') THEN
        RAISE EXCEPTION 'Roll back the active dedupe run (POST /api/admin/discovery/dedupe/runs/{id}/rollback) or restore a backup before dropping the dedupe rollback log';
    END IF;
END $$;
-- +goose StatementEnd
DROP INDEX idx_discovery_candidates_dedupe_pending;
DROP INDEX idx_river_assessment_material_live;
DROP TABLE article_dedupe_changes;
DROP TABLE article_dedupe_settings;
DROP TABLE article_dedupe_members;
DROP TABLE article_dedupe_runs;
DROP TABLE article_dedupe_buckets;
DROP TABLE article_dedupe_fingerprints;
