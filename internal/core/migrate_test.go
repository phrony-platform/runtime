package core

import (
	"errors"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestMigrate_success(t *testing.T) {
	db, mock := testSQLxDB(t)
	expectCreateRuntimeMeta(mock)
	expectSchemaVersionSeed(mock)

	if err := Migrate(db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestMigrate_createTableFailed(t *testing.T) {
	db, mock := testSQLxDB(t)
	mock.ExpectExec(`CREATE TABLE IF NOT EXISTS runtime_meta`).
		WillReturnError(errors.New("migrate failed"))

	err := Migrate(db)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "create runtime_meta") {
		t.Fatalf("expected create table error, got %v", err)
	}
}

func TestMigrate_seedFailed(t *testing.T) {
	db, mock := testSQLxDB(t)
	expectCreateRuntimeMeta(mock)
	mock.ExpectExec(`INSERT INTO runtime_meta`).
		WithArgs(SchemaMetaVersionKey, schemaVersionValue).
		WillReturnError(errors.New("seed failed"))

	err := Migrate(db)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "seed schema_version") {
		t.Fatalf("expected seed error, got %v", err)
	}
}

func expectCreateRuntimeMeta(mock sqlmock.Sqlmock) {
	mock.ExpectExec(`CREATE TABLE IF NOT EXISTS runtime_meta`).
		WillReturnResult(sqlmock.NewResult(0, 0))
}

func expectSchemaVersionSeed(mock sqlmock.Sqlmock) {
	mock.ExpectExec(`INSERT INTO runtime_meta`).
		WithArgs(SchemaMetaVersionKey, schemaVersionValue).
		WillReturnResult(sqlmock.NewResult(1, 1))
}
