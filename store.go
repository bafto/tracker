package main

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"

	"github.com/bafto/tracker/db"
)

// DBStore wraps the sqlc-generated queries, manages transactions, and
// translates between DB rows and domain types.
type DBStore struct {
	conn    *sql.DB
	queries *db.Queries
	logger  *slog.Logger
}

func NewStore(conn *sql.DB, queries *db.Queries, logger *slog.Logger) *DBStore {
	return &DBStore{conn: conn, queries: queries, logger: logger.With("component", "store")}
}

type txKey struct{}

func injectTx(ctx context.Context, tx *sql.Tx) context.Context {
	return context.WithValue(ctx, txKey{}, tx)
}

func extractTx(ctx context.Context) *sql.Tx {
	tx, _ := ctx.Value(txKey{}).(*sql.Tx)
	return tx
}

// withinTransaction runs fn inside a DB transaction, committing if fn
// returns nil and rolling back otherwise. If ctx already carries a
// transaction (i.e. this call is nested inside another withinTransaction),
// that transaction is reused and left for the outer call to commit/rollback.
func (s *DBStore) withinTransaction(ctx context.Context, fn func(ctx context.Context) error) error {
	if extractTx(ctx) != nil {
		return fn(ctx)
	}

	tx, err := s.conn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	if err := fn(injectTx(ctx, tx)); err != nil {
		return err
	}

	return tx.Commit()
}

// getQueries returns queries bound to the transaction in ctx, if any,
// otherwise the Store's plain (prepared) queries.
func (s *DBStore) getQueries(ctx context.Context) *db.Queries {
	if tx := extractTx(ctx); tx != nil {
		return s.queries.WithTx(tx)
	}
	return s.queries
}

func mapSlice[T, U any](in []T, f func(T) U) []U {
	out := make([]U, len(in))
	for i, v := range in {
		out[i] = f(v)
	}
	return out
}
