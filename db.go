package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"

	"github.com/bafto/tracker/db"
	_ "github.com/mattn/go-sqlite3"
)

// setupDB opens the sqlite connection, runs migrations, and prepares the
// sqlc queries, returning a ready-to-use Store and a cleanup func that
// closes both the prepared queries and the underlying connection.
func setupDB(ctx context.Context, connString string, logger *slog.Logger) (*DBStore, func() error, error) {
	conn, err := sql.Open("sqlite3", connString)
	if err != nil {
		return nil, nil, fmt.Errorf("sql.Open: %w", err)
	}

	if err := conn.PingContext(ctx); err != nil {
		conn.Close()
		return nil, nil, fmt.Errorf("ping db: %w", err)
	}

	if err := migrateDB(conn, logger); err != nil {
		conn.Close()
		return nil, nil, fmt.Errorf("migrate db: %w", err)
	}

	queries, err := db.Prepare(ctx, conn)
	if err != nil {
		conn.Close()
		return nil, nil, fmt.Errorf("prepare queries: %w", err)
	}

	store := NewStore(conn, queries, logger)

	closeDB := func() error {
		return errors.Join(queries.Close(), conn.Close())
	}

	return store, closeDB, nil
}
