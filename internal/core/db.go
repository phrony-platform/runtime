package core

import (
	"context"

	"github.com/jmoiron/sqlx"
)

func pingDB(ctx context.Context, db *sqlx.DB) error {
	_, err := db.ExecContext(ctx, "SELECT 1")
	return err
}
