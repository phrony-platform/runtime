package runtimecmd

import (
	"log/slog"

	"github.com/jmoiron/sqlx"
)

func closeDB(db *sqlx.DB) {
	if err := db.Close(); err != nil {
		slog.Warn("close database", "error", err)
	}
}
