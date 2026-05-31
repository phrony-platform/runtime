package store

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestQueries_ListAgents_allNamespaces(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	created := time.Now()
	mock.ExpectQuery(`FROM agents`).
		WithArgs("").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "namespace", "name", "owner", "archived_at", "created_at",
		}).
			AddRow("agent-1", "alpha", "one", "owner-a", nil, created).
			AddRow("agent-2", "beta", "two", "owner-b", nil, created))

	q := New(sqlDB)
	rows, err := q.ListAgents(context.Background(), "")
	if err != nil {
		t.Fatalf("ListAgents: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("len(rows) = %d, want 2", len(rows))
	}
	if rows[0].Namespace != "alpha" || rows[0].Name != "one" {
		t.Fatalf("rows[0] = %+v, want alpha/one", rows[0])
	}
	if rows[1].Namespace != "beta" {
		t.Fatalf("rows[1].namespace = %q, want beta", rows[1].Namespace)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestQueries_ListAgents_namespaceFilter(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	created := time.Now()
	mock.ExpectQuery(`FROM agents`).
		WithArgs("demo").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "namespace", "name", "owner", "archived_at", "created_at",
		}).AddRow("agent-1", "demo", "echo", "owner-1", nil, created))

	q := New(sqlDB)
	rows, err := q.ListAgents(context.Background(), "demo")
	if err != nil {
		t.Fatalf("ListAgents: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1", len(rows))
	}
	if rows[0].ID != "agent-1" || rows[0].Namespace != "demo" {
		t.Fatalf("row = %+v", rows[0])
	}
}

func TestQueries_ListAgents_empty(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	mock.ExpectQuery(`FROM agents`).
		WithArgs("missing").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "namespace", "name", "owner", "archived_at", "created_at",
		}))

	q := New(sqlDB)
	rows, err := q.ListAgents(context.Background(), "missing")
	if err != nil {
		t.Fatalf("ListAgents: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("len(rows) = %d, want 0", len(rows))
	}
}

func TestQueries_ListAgentVersions(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	older := time.Now().Add(-2 * time.Hour)
	newer := time.Now()
	mock.ExpectQuery(`FROM agent_versions`).
		WithArgs("agent-1").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "version", "content_hash", "deployed_at", "deprecated_at",
		}).
			AddRow("ver-2", "1.1.0", "hash-b", newer, nil).
			AddRow("ver-1", "1.0.0", "hash-a", older, sql.NullTime{Valid: true, Time: newer}))

	q := New(sqlDB)
	rows, err := q.ListAgentVersions(context.Background(), "agent-1")
	if err != nil {
		t.Fatalf("ListAgentVersions: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("len(rows) = %d, want 2", len(rows))
	}
	if rows[0].Version != "1.1.0" || rows[1].Version != "1.0.0" {
		t.Fatalf("versions = %q, %q; want newest first", rows[0].Version, rows[1].Version)
	}
	if !rows[1].DeprecatedAt.Valid {
		t.Fatal("expected deprecated_at on older version row")
	}
}

func TestQueries_ListAgentVersions_empty(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	mock.ExpectQuery(`FROM agent_versions`).
		WithArgs("agent-none").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "version", "content_hash", "deployed_at", "deprecated_at",
		}))

	q := New(sqlDB)
	rows, err := q.ListAgentVersions(context.Background(), "agent-none")
	if err != nil {
		t.Fatalf("ListAgentVersions: %v", err)
	}
	if rows != nil && len(rows) != 0 {
		t.Fatalf("len(rows) = %d, want 0", len(rows))
	}
}

func TestQueries_AgentByID(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	mock.ExpectQuery(`FROM agents`).
		WithArgs("agent-1").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "namespace", "name", "archived_at",
		}).AddRow("agent-1", "demo", "echo", nil))

	q := New(sqlDB)
	row, err := q.AgentByID(context.Background(), "agent-1")
	if err != nil {
		t.Fatalf("AgentByID: %v", err)
	}
	if row.Namespace != "demo" || row.Name != "echo" {
		t.Fatalf("row = %+v", row)
	}
}

func TestQueries_AgentByID_notFound(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	mock.ExpectQuery(`FROM agents`).
		WithArgs("missing").
		WillReturnError(sql.ErrNoRows)

	q := New(sqlDB)
	_, err = q.AgentByID(context.Background(), "missing")
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("err = %v, want sql.ErrNoRows", err)
	}
}

func TestQueries_AgentByNamespaceName(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	mock.ExpectQuery(`FROM agents`).
		WithArgs("demo", "echo").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "namespace", "name", "archived_at",
		}).AddRow("agent-1", "demo", "echo", nil))

	q := New(sqlDB)
	row, err := q.AgentByNamespaceName(context.Background(), "demo", "echo")
	if err != nil {
		t.Fatalf("AgentByNamespaceName: %v", err)
	}
	if row.ID != "agent-1" {
		t.Fatalf("id = %q, want agent-1", row.ID)
	}
}

func TestQueries_AgentByNamespaceName_notFound(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	mock.ExpectQuery(`FROM agents`).
		WithArgs("demo", "missing").
		WillReturnError(sql.ErrNoRows)

	q := New(sqlDB)
	_, err = q.AgentByNamespaceName(context.Background(), "demo", "missing")
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("err = %v, want sql.ErrNoRows", err)
	}
}

func TestQueries_DeprecateAgentVersion(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	mock.ExpectQuery(`UPDATE agent_versions`).
		WithArgs("agent-1", "1.0.0").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("ver-1"))

	q := New(sqlDB)
	id, err := q.DeprecateAgentVersion(context.Background(), "agent-1", "1.0.0")
	if err != nil {
		t.Fatalf("DeprecateAgentVersion: %v", err)
	}
	if id != "ver-1" {
		t.Fatalf("id = %q, want ver-1", id)
	}
}

func TestQueries_DeprecateAgentVersion_notFound(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	mock.ExpectQuery(`UPDATE agent_versions`).
		WithArgs("agent-1", "9.9.9").
		WillReturnError(sql.ErrNoRows)

	q := New(sqlDB)
	_, err = q.DeprecateAgentVersion(context.Background(), "agent-1", "9.9.9")
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("err = %v, want sql.ErrNoRows", err)
	}
}

func TestQueries_ArchiveAgent(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	mock.ExpectQuery(`UPDATE agents`).
		WithArgs("agent-1").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("agent-1"))

	q := New(sqlDB)
	id, err := q.ArchiveAgent(context.Background(), "agent-1")
	if err != nil {
		t.Fatalf("ArchiveAgent: %v", err)
	}
	if id != "agent-1" {
		t.Fatalf("id = %q, want agent-1", id)
	}
}

func TestQueries_DeprecateAllAgentVersions(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	mock.ExpectExec(`UPDATE agent_versions`).
		WithArgs("agent-1").
		WillReturnResult(sqlmock.NewResult(0, 2))

	q := New(sqlDB)
	if err := q.DeprecateAllAgentVersions(context.Background(), "agent-1"); err != nil {
		t.Fatalf("DeprecateAllAgentVersions: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}
