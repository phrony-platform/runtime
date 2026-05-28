package runtimecmd

import (
	"errors"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/jmoiron/sqlx"
	"github.com/phrony-platform/runtime/internal/common"
	"github.com/phrony-platform/runtime/internal/core"
)

func TestRunMigrate_success(t *testing.T) {
	restore := stubRuntimeDB(t)
	defer restore()
	t.Setenv("RUNTIME_DATABASE_URL", "postgres://unused")

	if err := runMigrate(); err != nil {
		t.Fatalf("runMigrate: %v", err)
	}
}

func TestRunMigrate_loadSettingsFailed(t *testing.T) {
	t.Setenv("RUNTIME_DATABASE_URL", "")

	err := runMigrateWithDeps(migrateDeps{
		loadSettings: common.LoadSettings,
		openDB:       common.OpenDB,
		migrate:      core.Migrate,
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "load settings") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunMigrate_openDBFailed(t *testing.T) {
	t.Setenv("RUNTIME_DATABASE_URL", "postgres://unused")

	err := runMigrateWithDeps(migrateDeps{
		loadSettings: common.LoadSettings,
		openDB: func(common.Settings) (*sqlx.DB, error) {
			return nil, errors.New("open failed")
		},
		migrate: core.Migrate,
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "open database") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunMigrate_migrateFailed(t *testing.T) {
	restore := stubRuntimeDB(t)
	defer restore()
	t.Setenv("RUNTIME_DATABASE_URL", "postgres://unused")

	err := runMigrateWithDeps(migrateDeps{
		loadSettings: common.LoadSettings,
		openDB:       common.OpenDB,
		migrate: func(*sqlx.DB) error {
			return errors.New("migrate failed")
		},
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "migrate:") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func stubRuntimeDB(t *testing.T) func() {
	t.Helper()
	return common.SwapConnectPostgres(func(string) (*sqlx.DB, error) {
		sqlDB, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New: %v", err)
		}
		t.Cleanup(func() { _ = sqlDB.Close() })
		mock.ExpectExec(`CREATE TABLE IF NOT EXISTS runtime_meta`).WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectExec(`INSERT INTO runtime_meta`).WillReturnResult(sqlmock.NewResult(1, 1))
		return sqlx.NewDb(sqlDB, "pgx"), nil
	})
}
