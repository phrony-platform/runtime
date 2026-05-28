package runtimecmd

import (
	"fmt"
	"log/slog"

	"github.com/jmoiron/sqlx"
	"github.com/phrony-platform/runtime/internal/common"
	"github.com/phrony-platform/runtime/internal/core"
)

type migrateDeps struct {
	loadSettings func() (common.Settings, error)
	openDB       func(common.Settings) (*sqlx.DB, error)
	migrate      func(*sqlx.DB) error
}

var defaultMigrateDeps = migrateDeps{
	loadSettings: common.LoadSettings,
	openDB:       common.OpenDB,
	migrate:      core.Migrate,
}

func runMigrate() error {
	return runMigrateWithDeps(defaultMigrateDeps)
}

func runMigrateWithDeps(deps migrateDeps) error {
	settings, err := deps.loadSettings()
	if err != nil {
		return fmt.Errorf("load settings: %w", err)
	}

	db, err := deps.openDB(settings)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer closeDB(db)

	if err := deps.migrate(db); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}

	slog.Info("database migrated")
	return nil
}
