package core

import (
	"errors"
	"fmt"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jmoiron/sqlx"
	"github.com/phrony-platform/runtime/migrations"
)

// SchemaMetaVersionKey is the runtime_meta key for the human-facing schema version label.
const SchemaMetaVersionKey = "schema_version"

// Migrate applies pending SQL migrations from migrations/*.sql (embedded).
// Already-applied versions are recorded in schema_migrations and skipped.
func Migrate(db *sqlx.DB) error {
	if db == nil {
		return fmt.Errorf("database is nil")
	}

	source, err := iofs.New(migrations.FS, ".")
	if err != nil {
		return fmt.Errorf("load migrations: %w", err)
	}

	driver, err := postgres.WithInstance(db.DB, &postgres.Config{})
	if err != nil {
		return fmt.Errorf("postgres migrate driver: %w", err)
	}

	m, err := migrate.NewWithInstance("iofs", source, "postgres", driver)
	if err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	// Do not call m.Close(): WithInstance shares db.DB and migrate closes it on Close.

	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("migrate up: %w", err)
	}
	return nil
}
