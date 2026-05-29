package store

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestQueries_LatestAgentVersionID(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	mock.ExpectQuery(`FROM agent_versions av`).
		WithArgs("demo", "echo").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("ver-1"))

	q := New(sqlDB)
	id, err := q.LatestAgentVersionID(context.Background(), "demo", "echo")
	if err != nil {
		t.Fatalf("LatestAgentVersionID: %v", err)
	}
	if id != "ver-1" {
		t.Fatalf("id = %q, want ver-1", id)
	}
}

func TestQueries_AgentVersionIDByLabel(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	mock.ExpectQuery(`FROM agent_versions av`).
		WithArgs("demo", "echo", "1.2.0").
		WillReturnRows(sqlmock.NewRows([]string{"id", "deprecated_at", "archived_at"}).AddRow("ver-1", nil, nil))

	q := New(sqlDB)
	lookup, err := q.AgentVersionIDByLabel(context.Background(), "demo", "echo", "1.2.0")
	if err != nil {
		t.Fatalf("AgentVersionIDByLabel: %v", err)
	}
	if lookup.ID != "ver-1" {
		t.Fatalf("id = %q, want ver-1", lookup.ID)
	}
	if lookup.Deprecated || lookup.AgentArchive {
		t.Fatal("expected runnable version")
	}
}

func TestQueries_AgentVersionIDByLabel_notFound(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	mock.ExpectQuery(`FROM agent_versions av`).
		WithArgs("demo", "echo", "9.9.9").
		WillReturnError(sql.ErrNoRows)

	q := New(sqlDB)
	_, err = q.AgentVersionIDByLabel(context.Background(), "demo", "echo", "9.9.9")
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("err = %v, want sql.ErrNoRows", err)
	}
}

func TestQueries_InsertSession(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	mock.ExpectQuery(`INSERT INTO sessions`).
		WithArgs("sess-1", "ver-1", []byte("{}"), "pending").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("sess-1"))

	q := New(sqlDB)
	id, err := q.InsertSession(context.Background(), InsertSessionParams{
		ID:             "sess-1",
		AgentVersionID: "ver-1",
		Input:          []byte("{}"),
		Status:         "pending",
	})
	if err != nil {
		t.Fatalf("InsertSession: %v", err)
	}
	if id != "sess-1" {
		t.Fatalf("id = %q, want sess-1", id)
	}
}
