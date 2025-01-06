-- +goose Up
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS "block_watcher_state" (
	id          INTEGER NOT NULL,
	epoch	    INTEGER NOT NULL,
	slot	    INTEGER NOT NULL,
	last_update TIMESTAMP NOT NULL,
	PRIMARY KEY("id")
);
-- +goose StatementEnd