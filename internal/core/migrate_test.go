package core

import (
	"io/fs"
	"strings"
	"testing"

	"github.com/phrony-platform/runtime/migrations"
)

func TestEmbeddedMigrations_present(t *testing.T) {
	t.Parallel()

	var ups, downs int
	err := fs.WalkDir(migrations.FS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		switch {
		case strings.HasSuffix(path, ".up.sql"):
			ups++
		case strings.HasSuffix(path, ".down.sql"):
			downs++
		default:
			t.Errorf("unexpected file in migrations: %s", path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("WalkDir: %v", err)
	}
	if ups == 0 {
		t.Fatal("expected at least one .up.sql migration")
	}
	if ups != downs {
		t.Fatalf("up migrations = %d, down migrations = %d, want equal", ups, downs)
	}
}

func TestMigrate_nilDatabase(t *testing.T) {
	err := Migrate(nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "database is nil") {
		t.Fatalf("error = %v, want nil database", err)
	}
}
