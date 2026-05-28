package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestQueries_GetRuntimeMetaValue(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	mock.ExpectQuery(`SELECT value`).
		WithArgs("schema_version").
		WillReturnRows(sqlmock.NewRows([]string{"value"}).AddRow("2"))

	q := New(sqlDB)
	value, err := q.GetRuntimeMetaValue(context.Background(), "schema_version")
	if err != nil {
		t.Fatalf("GetRuntimeMetaValue: %v", err)
	}
	if value != "2" {
		t.Fatalf("value = %q, want 2", value)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestQueries_GetRuntimeMetaValue_notFound(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	mock.ExpectQuery(`SELECT value`).
		WithArgs("missing").
		WillReturnError(sql.ErrNoRows)

	q := New(sqlDB)
	_, err = q.GetRuntimeMetaValue(context.Background(), "missing")
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("err = %v, want sql.ErrNoRows", err)
	}
}

func TestQueries_UpsertAgent(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	labels := json.RawMessage(`{"env":"test"}`)
	mock.ExpectQuery(`INSERT INTO agents`).
		WithArgs("agent-id", "demo", "echo", "owner-1", labels).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("agent-id"))

	q := New(sqlDB)
	id, err := q.UpsertAgent(context.Background(), UpsertAgentParams{
		ID:        "agent-id",
		Namespace: "demo",
		Name:      "echo",
		Owner:     "owner-1",
		Labels:    labels,
	})
	if err != nil {
		t.Fatalf("UpsertAgent: %v", err)
	}
	if id != "agent-id" {
		t.Fatalf("id = %q, want agent-id", id)
	}
}

func TestQueries_UpsertAgentVersion(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	manifest := json.RawMessage(`{"apiVersion":"phrony.dev/v1"}`)
	mock.ExpectQuery(`INSERT INTO agent_versions`).
		WithArgs("ver-id", "agent-id", "1.0.0", "abc123", manifest).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("ver-id"))

	q := New(sqlDB)
	id, err := q.UpsertAgentVersion(context.Background(), UpsertAgentVersionParams{
		ID:          "ver-id",
		AgentID:     "agent-id",
		Version:     "1.0.0",
		ContentHash: "abc123",
		Manifest:    manifest,
	})
	if err != nil {
		t.Fatalf("UpsertAgentVersion: %v", err)
	}
	if id != "ver-id" {
		t.Fatalf("id = %q, want ver-id", id)
	}
}

func TestQueries_WithTx(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	mock.ExpectBegin()
	tx, err := sqlDB.Begin()
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	t.Cleanup(func() { _ = tx.Rollback() })

	mock.ExpectQuery(`INSERT INTO agents`).
		WithArgs(sqlmock.AnyArg(), "ns", "name", "", sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("from-tx"))

	q := New(sqlDB).WithTx(tx)
	_, err = q.UpsertAgent(context.Background(), UpsertAgentParams{
		ID:        "id-1",
		Namespace: "ns",
		Name:      "name",
		Labels:    json.RawMessage("{}"),
	})
	if err != nil {
		t.Fatalf("UpsertAgent in tx: %v", err)
	}
}
