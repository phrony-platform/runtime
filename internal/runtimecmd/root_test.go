package runtimecmd

import (
	"strings"
	"testing"

	"github.com/jmoiron/sqlx"
)

func TestRunRoot_migrateMissingDatabaseURL(t *testing.T) {
	t.Setenv("RUNTIME_DATABASE_URL", "")

	if err := runRoot([]string{"migrate"}); err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestRunRoot_migrateSuccess(t *testing.T) {
	restore := stubRuntimeDB(t)
	defer restore()
	t.Setenv("RUNTIME_DATABASE_URL", "postgres://unused")

	prev := defaultMigrateDeps.migrate
	defaultMigrateDeps.migrate = func(*sqlx.DB) error { return nil }
	t.Cleanup(func() { defaultMigrateDeps.migrate = prev })

	if err := runRoot([]string{"migrate"}); err != nil {
		t.Fatalf("runRoot: %v", err)
	}
}

func TestRunRoot_serveInvalidListenAddr(t *testing.T) {
	restore := stubRuntimeDB(t)
	defer restore()
	t.Setenv("RUNTIME_DATABASE_URL", "postgres://unused")
	t.Setenv("RUNTIME_GRPC_ADDR", "127.0.0.1:-1")

	err := runRoot([]string{"serve", "--skip-migrate"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestRunRoot_unknownCommand(t *testing.T) {
	err := runRoot([]string{"unknown"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "unknown") {
		t.Fatalf("unexpected error: %v", err)
	}
}
