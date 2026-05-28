package core

import (
	"errors"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestMigrate_success(t *testing.T) {
	db, mock := testGORMDB(t)
	expectAutoMigrate(mock)
	expectSchemaVersionSeed(mock)

	if err := Migrate(db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestMigrate_autoMigrateFailed(t *testing.T) {
	db, mock := testGORMDB(t)
	mock.ExpectQuery(`SELECT count\(\*\) FROM information_schema\.tables`).
		WillReturnError(errors.New("migrate failed"))

	err := Migrate(db)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "auto migrate") {
		t.Fatalf("expected auto migrate error, got %v", err)
	}
}

func TestMigrate_seedFailed(t *testing.T) {
	db, mock := testGORMDB(t)
	expectAutoMigrate(mock)
	mock.ExpectQuery(`SELECT \* FROM "runtime_meta"`).
		WillReturnError(errors.New("seed failed"))

	err := Migrate(db)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "seed schema_version") {
		t.Fatalf("expected seed error, got %v", err)
	}
}

func expectAutoMigrate(mock sqlmock.Sqlmock) {
	mock.ExpectQuery(`SELECT count\(\*\) FROM information_schema\.tables`).
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	mock.ExpectExec(`CREATE TABLE "runtime_meta"`).
		WillReturnResult(sqlmock.NewResult(0, 0))
}

func expectSchemaVersionSeed(mock sqlmock.Sqlmock) {
	mock.ExpectQuery(`SELECT \* FROM "runtime_meta"`).
		WithArgs(schemaVersionKey, 1).
		WillReturnRows(sqlmock.NewRows([]string{"key", "value"}))
	mock.ExpectBegin()
	mock.ExpectExec(`INSERT INTO "runtime_meta"`).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()
}
