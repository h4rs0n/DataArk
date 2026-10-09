-- +goose Up
CREATE TABLE discovery_crawl_settings (
    id bigint PRIMARY KEY CHECK (id = 1),
    auto_start boolean NOT NULL DEFAULT false
);
INSERT INTO discovery_crawl_settings (id) VALUES (1);

-- +goose Down
DROP TABLE discovery_crawl_settings;
