package store

import (
	"context"
	"database/sql"
	"errors"
)

// DBTX is satisfied by both *sql.DB and *sql.Tx, so queries can run inside or
// outside a transaction.
type DBTX interface {
	ExecContext(context.Context, string, ...interface{}) (sql.Result, error)
	QueryContext(context.Context, string, ...interface{}) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...interface{}) *sql.Row
}

// Queries holds the database handle used by the hand-written accessors.
type Queries struct {
	db DBTX
}

func New(db DBTX) *Queries {
	return &Queries{db: db}
}

// WithTx returns a Queries bound to the given transaction.
func (q *Queries) WithTx(tx *sql.Tx) *Queries {
	return &Queries{db: tx}
}

// InTx runs fn inside a transaction when q is backed by *sql.DB. When q is
// already bound to a transaction, fn runs on the same handle without nesting.
func (q *Queries) InTx(ctx context.Context, fn func(context.Context, *Queries) error) error {
	if q == nil {
		return errors.New("queries is nil")
	}
	if _, ok := q.db.(*sql.Tx); ok {
		return fn(ctx, q)
	}
	sqlDB, ok := q.db.(*sql.DB)
	if !ok {
		return fn(ctx, q)
	}
	tx, err := sqlDB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := fn(ctx, q.WithTx(tx)); err != nil {
		return err
	}
	return tx.Commit()
}
