package database

import (
	"context"
	"fmt"
	"io/fs"
	"time"

	"github.com/jmoiron/sqlx"
	_ "github.com/mattn/go-sqlite3"
	"github.com/pressly/goose/v3"
)

type Options struct {
	URL          string
	Path         string
	MaxOpenConns int
}

type database struct {
	*sqlx.DB
	opts Options
}

func NewDatabase(opts Options) *database {
	return &database{
		opts: opts,
	}
}

func (db *database) Connect(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	var err error

	db.DB, err = sqlx.ConnectContext(ctx, "sqlite3", db.opts.Path+db.opts.URL)
	if err != nil {
		return fmt.Errorf("failed to connect to database: %w", err)
	}
	db.DB.SetMaxOpenConns(db.opts.MaxOpenConns)

	return nil
}

func (db *database) MigrateUp(migrationsFS fs.FS) error {
	goose.SetBaseFS(migrationsFS)
	if err := goose.SetDialect("sqlite3"); err != nil {
		return fmt.Errorf("failed to migrate: %w", err)
	}

	if err := goose.Up(db.DB.DB, "."); err != nil {
		return fmt.Errorf("failed to migrate: %w", err)
	}

	return nil
}
