-- +goose Up
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS "slots" (
	id          INTEGER NOT NULL,
	epoch	    INTEGER NOT NULL,
	pool_id	    TEXT NOT NULL,
	slot_qty	INTEGER NOT NULL,
	slots	    TEXT NOT NULL,
	hash	    TEXT NOT NULL,
	PRIMARY KEY("id" AUTOINCREMENT),
	UNIQUE("epoch","pool_id")
);
-- +goose StatementEnd