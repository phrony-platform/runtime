package store

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestQueries_InsertDeployment(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	mock.ExpectQuery(`INSERT INTO deployments`).
		WithArgs("dep-1", "agent-1", "ver-1", "deploy", "alice").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("dep-1"))

	q := New(sqlDB)
	id, err := q.InsertDeployment(context.Background(), "dep-1", "agent-1", "ver-1", "deploy", "alice")
	if err != nil {
		t.Fatalf("InsertDeployment: %v", err)
	}
	if id != "dep-1" {
		t.Fatalf("id = %q, want dep-1", id)
	}
}

func TestQueries_ActiveAgentVersion(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	mock.ExpectQuery(`FROM deployments d`).
		WithArgs("demo", "echo").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "version", "deprecated_at", "retired_at", "archived_at",
		}).AddRow("ver-2", "2.0.0", nil, nil, nil))

	q := New(sqlDB)
	got, err := q.ActiveAgentVersion(context.Background(), "demo", "echo")
	if err != nil {
		t.Fatalf("ActiveAgentVersion: %v", err)
	}
	if got.AgentVersionID != "ver-2" || got.Version != "2.0.0" {
		t.Fatalf("got = %+v", got)
	}
	if got.Deprecated || got.Retired || got.AgentArchived {
		t.Fatal("expected active runnable version")
	}
}

func TestQueries_ActiveAgentVersion_notFound(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	mock.ExpectQuery(`FROM deployments d`).
		WithArgs("demo", "missing").
		WillReturnError(sql.ErrNoRows)

	q := New(sqlDB)
	_, err = q.ActiveAgentVersion(context.Background(), "demo", "missing")
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("err = %v, want sql.ErrNoRows", err)
	}
}

func TestQueries_PreviousActiveVersion(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	mock.ExpectQuery(`WITH active AS`).
		WithArgs("agent-1").
		WillReturnRows(sqlmock.NewRows([]string{"agent_version_id"}).AddRow("ver-1"))

	q := New(sqlDB)
	id, err := q.PreviousActiveVersion(context.Background(), "agent-1")
	if err != nil {
		t.Fatalf("PreviousActiveVersion: %v", err)
	}
	if id != "ver-1" {
		t.Fatalf("id = %q, want ver-1", id)
	}
}

func TestQueries_ListDeployments(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	now := time.Now()
	earlier := now.Add(-time.Hour)
	mock.ExpectQuery(`FROM deployments d`).
		WithArgs("agent-1").
		WillReturnRows(sqlmock.NewRows([]string{"version", "action", "actor", "created_at"}).
			AddRow("2.0.0", "deploy", "alice", now).
			AddRow("1.0.0", "rollback", "bob", earlier))

	q := New(sqlDB)
	rows, err := q.ListDeployments(context.Background(), "agent-1")
	if err != nil {
		t.Fatalf("ListDeployments: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("len(rows) = %d, want 2", len(rows))
	}
	if rows[0].Version != "2.0.0" || rows[0].Action != "deploy" {
		t.Fatalf("rows[0] = %+v", rows[0])
	}
}

func TestQueries_RetireAgentVersion(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	mock.ExpectQuery(`UPDATE agent_versions`).
		WithArgs("agent-1", "1.0.0").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("ver-1"))

	q := New(sqlDB)
	id, err := q.RetireAgentVersion(context.Background(), "agent-1", "1.0.0")
	if err != nil {
		t.Fatalf("RetireAgentVersion: %v", err)
	}
	if id != "ver-1" {
		t.Fatalf("id = %q, want ver-1", id)
	}
}

func TestQueries_GetAgentVersionByLabel(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	published := time.Now()
	manifest := []byte(`{"kind":"Agent"}`)
	mock.ExpectQuery(`FROM agent_versions av`).
		WithArgs("demo", "echo", "1.0.0").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "version", "content_hash", "manifest", "deployed_at", "deprecated_at", "retired_at",
		}).AddRow("ver-1", "1.0.0", "abc", manifest, published, nil, nil))

	q := New(sqlDB)
	got, err := q.GetAgentVersionByLabel(context.Background(), "demo", "echo", "1.0.0")
	if err != nil {
		t.Fatalf("GetAgentVersionByLabel: %v", err)
	}
	if got.ID != "ver-1" || got.Version != "1.0.0" || got.ContentHash != "abc" {
		t.Fatalf("got = %+v", got)
	}
	if string(got.Manifest) != string(manifest) {
		t.Fatalf("manifest = %s", got.Manifest)
	}
	if !got.PublishedAt.Equal(published) {
		t.Fatalf("published_at = %v, want %v", got.PublishedAt, published)
	}
	if got.DeprecatedAt.Valid || got.RetiredAt.Valid {
		t.Fatal("expected no lifecycle timestamps")
	}
}

func TestQueries_ActiveDeploymentDetail(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	deployed := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	mock.ExpectQuery(`FROM deployments d`).
		WithArgs("demo", "echo").
		WillReturnRows(sqlmock.NewRows([]string{"version", "created_at", "actor"}).
			AddRow("2.0.0", deployed, "alice"))

	q := New(sqlDB)
	got, err := q.ActiveDeploymentDetail(context.Background(), "demo", "echo")
	if err != nil {
		t.Fatalf("ActiveDeploymentDetail: %v", err)
	}
	if got.Version != "2.0.0" || got.Actor != "alice" {
		t.Fatalf("got = %+v", got)
	}
	if !got.DeployedAt.Equal(deployed) {
		t.Fatalf("deployed_at = %v, want %v", got.DeployedAt, deployed)
	}
}

func TestQueries_ActiveDeploymentDetail_notFound(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	mock.ExpectQuery(`FROM deployments d`).
		WithArgs("demo", "missing").
		WillReturnError(sql.ErrNoRows)

	q := New(sqlDB)
	_, err = q.ActiveDeploymentDetail(context.Background(), "demo", "missing")
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("err = %v, want sql.ErrNoRows", err)
	}
}

func TestQueries_AgentVersionLabelByID(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	mock.ExpectQuery(`SELECT version FROM agent_versions`).
		WithArgs("ver-1").
		WillReturnRows(sqlmock.NewRows([]string{"version"}).AddRow("1.0.0"))

	q := New(sqlDB)
	label, err := q.AgentVersionLabelByID(context.Background(), "ver-1")
	if err != nil {
		t.Fatalf("AgentVersionLabelByID: %v", err)
	}
	if label != "1.0.0" {
		t.Fatalf("label = %q, want 1.0.0", label)
	}
}

func TestQueries_PreviousActiveVersion_notFound(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	mock.ExpectQuery(`WITH active AS`).
		WithArgs("agent-1").
		WillReturnError(sql.ErrNoRows)

	q := New(sqlDB)
	_, err = q.PreviousActiveVersion(context.Background(), "agent-1")
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("err = %v, want sql.ErrNoRows", err)
	}
}

func TestQueries_ActiveAgentVersion_retired(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	retired := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	mock.ExpectQuery(`FROM deployments d`).
		WithArgs("demo", "echo").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "version", "deprecated_at", "retired_at", "archived_at",
		}).AddRow("ver-1", "1.0.0", nil, retired, nil))

	q := New(sqlDB)
	got, err := q.ActiveAgentVersion(context.Background(), "demo", "echo")
	if err != nil {
		t.Fatalf("ActiveAgentVersion: %v", err)
	}
	if !got.Retired {
		t.Fatal("expected retired active version")
	}
}
