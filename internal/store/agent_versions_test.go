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

func TestQueries_GetAgentVersionManifest(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	manifest := []byte(`{"kind":"Agent"}`)
	mock.ExpectQuery(`FROM agent_versions`).
		WithArgs("ver-1").
		WillReturnRows(sqlmock.NewRows([]string{"manifest"}).AddRow(manifest))

	q := New(sqlDB)
	got, err := q.GetAgentVersionManifest(context.Background(), "ver-1")
	if err != nil {
		t.Fatalf("GetAgentVersionManifest: %v", err)
	}
	if string(got) != string(manifest) {
		t.Fatalf("manifest = %s, want %s", got, manifest)
	}
}
