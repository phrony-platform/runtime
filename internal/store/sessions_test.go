package store

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestQueries_InsertSession(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	mock.ExpectQuery(`INSERT INTO sessions`).
		WithArgs("sess-1", "ver-1", []byte("{}"), "pending", nil, 0, "", "sess-1").
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

func TestQueries_GetSession(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	now := time.Now()
	mock.ExpectQuery(`FROM sessions`).
		WithArgs("sess-1").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "agent_version_id", "input", "status", "error", "root_session_id", "event_seq", "created_at", "updated_at",
		}).AddRow("sess-1", "ver-1", []byte(`{"q":"hi"}`), "running", nil, "sess-1", 0, now, now))

	q := New(sqlDB)
	s, err := q.GetSession(context.Background(), "sess-1")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if s.Status != "running" {
		t.Fatalf("status = %q, want running", s.Status)
	}
	if s.RootSessionID != "sess-1" {
		t.Fatalf("root_session_id = %q, want sess-1", s.RootSessionID)
	}
}

func TestQueries_GetSession_notFound(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	mock.ExpectQuery(`FROM sessions`).
		WithArgs("missing").
		WillReturnError(sql.ErrNoRows)

	q := New(sqlDB)
	_, err = q.GetSession(context.Background(), "missing")
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("err = %v, want sql.ErrNoRows", err)
	}
}

func TestQueries_UpdateSession(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	now := time.Now()
	errMsg := "provider timeout"
	mock.ExpectQuery(`UPDATE sessions`).
		WithArgs("sess-1", "failed", errMsg).
		WillReturnRows(sqlmock.NewRows([]string{"updated_at"}).AddRow(now))

	q := New(sqlDB)
	updatedAt, err := q.UpdateSession(context.Background(), UpdateSessionParams{
		ID:     "sess-1",
		Status: "failed",
		Error:  &errMsg,
	})
	if err != nil {
		t.Fatalf("UpdateSession: %v", err)
	}
	if !updatedAt.Equal(now) {
		t.Fatalf("updated_at = %v, want %v", updatedAt, now)
	}
}

func TestQueries_UpdateSession_statusOnly(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	now := time.Now()
	mock.ExpectQuery(`UPDATE sessions`).
		WithArgs("sess-1", "awaiting_input", nil).
		WillReturnRows(sqlmock.NewRows([]string{"updated_at"}).AddRow(now))

	q := New(sqlDB)
	_, err = q.UpdateSession(context.Background(), UpdateSessionParams{
		ID:     "sess-1",
		Status: "awaiting_input",
	})
	if err != nil {
		t.Fatalf("UpdateSession: %v", err)
	}
}

func sessionListRowColumns() []string {
	return []string{
		"id", "agent_version_id", "status", "created_at", "updated_at", "bundle_version_id",
		"agent_namespace", "agent_name", "agent_version",
		"bundle_namespace", "bundle_name", "bundle_version",
	}
}

func TestQueries_ListSessionsByAgentVersionID(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	now := time.Now()
	mock.ExpectQuery(`FROM sessions`).
		WithArgs("ver-1", "awaiting_input", false, "", "").
		WillReturnRows(sqlmock.NewRows(sessionListRowColumns()).
			AddRow("sess-2", "ver-1", "awaiting_input", now, now, nil, "demo", "echo", "1.0.0", nil, nil, nil).
			AddRow("sess-1", "ver-1", "awaiting_input", now.Add(-time.Hour), now, nil, "demo", "echo", "1.0.0", nil, nil, nil))

	q := New(sqlDB)
	rows, err := q.ListSessions(context.Background(), ListSessionsParams{
		AgentVersionID: "ver-1",
		Status:         "awaiting_input",
	})
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("len(rows) = %d, want 2", len(rows))
	}
	if rows[0].ID != "sess-2" {
		t.Fatalf("rows[0].id = %q, want sess-2", rows[0].ID)
	}
}

func TestQueries_CancelSession(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	mock.ExpectQuery(`UPDATE sessions`).
		WithArgs("sess-1").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("sess-1"))

	q := New(sqlDB)
	id, err := q.CancelSession(context.Background(), "sess-1")
	if err != nil {
		t.Fatalf("CancelSession: %v", err)
	}
	if id != "sess-1" {
		t.Fatalf("id = %q, want sess-1", id)
	}
}

func TestQueries_CancelSession_notFound(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	mock.ExpectQuery(`UPDATE sessions`).
		WithArgs("sess-done").
		WillReturnError(sql.ErrNoRows)

	q := New(sqlDB)
	_, err = q.CancelSession(context.Background(), "sess-done")
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("err = %v, want sql.ErrNoRows", err)
	}
}

func TestQueries_CompleteSession(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	mock.ExpectQuery(`UPDATE sessions`).
		WithArgs("sess-1").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("sess-1"))

	q := New(sqlDB)
	id, err := q.CompleteSession(context.Background(), "sess-1")
	if err != nil {
		t.Fatalf("CompleteSession: %v", err)
	}
	if id != "sess-1" {
		t.Fatalf("id = %q, want sess-1", id)
	}
}

func TestQueries_CompleteSession_notFound(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	mock.ExpectQuery(`UPDATE sessions`).
		WithArgs("sess-done").
		WillReturnError(sql.ErrNoRows)

	q := New(sqlDB)
	_, err = q.CompleteSession(context.Background(), "sess-done")
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("err = %v, want sql.ErrNoRows", err)
	}
}

func TestQueries_ListSessionsByAgentVersionID_allAgents(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	now := time.Now()
	mock.ExpectQuery(`FROM sessions`).
		WithArgs("", "", false, "", "").
		WillReturnRows(sqlmock.NewRows(sessionListRowColumns()).
			AddRow("sess-2", "ver-2", "running", now, now, nil, "demo", "a", "1.0.0", nil, nil, nil).
			AddRow("sess-1", "ver-1", "completed", now.Add(-time.Hour), now, nil, "demo", "b", "1.0.0", nil, nil, nil))

	q := New(sqlDB)
	rows, err := q.ListSessions(context.Background(), ListSessionsParams{})
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("len(rows) = %d, want 2", len(rows))
	}
}

func TestQueries_ListDescendantSessionIDs(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	mock.ExpectQuery(`WITH RECURSIVE descendants`).
		WithArgs("root-sess").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).
			AddRow("root-sess").
			AddRow("child-sess"))

	q := New(sqlDB)
	ids, err := q.ListDescendantSessionIDs(context.Background(), "root-sess")
	if err != nil {
		t.Fatalf("ListDescendantSessionIDs: %v", err)
	}
	if len(ids) != 2 || ids[0] != "root-sess" || ids[1] != "child-sess" {
		t.Fatalf("ids = %v", ids)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestQueries_ListSessionsByAgentVersionID_allStatuses(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	now := time.Now()
	mock.ExpectQuery(`FROM sessions`).
		WithArgs("ver-1", "", false, "", "").
		WillReturnRows(sqlmock.NewRows(sessionListRowColumns()).
			AddRow("sess-1", "ver-1", "completed", now, now, nil, "demo", "echo", "1.0.0", nil, nil, nil))

	q := New(sqlDB)
	rows, err := q.ListSessions(context.Background(), ListSessionsParams{
		AgentVersionID: "ver-1",
	})
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1", len(rows))
	}
}

func TestQueries_ListSessionsByAgentVersionID_includeChildren(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	now := time.Now()
	mock.ExpectQuery(`FROM sessions`).
		WithArgs("ver-1", "", true, "", "").
		WillReturnRows(sqlmock.NewRows(sessionListRowColumns()).
			AddRow("child-sess", "ver-1", "completed", now, now, nil, "demo", "echo", "1.0.0", nil, nil, nil).
			AddRow("root-sess", "ver-1", "completed", now.Add(-time.Hour), now, nil, "demo", "echo", "1.0.0", nil, nil, nil))

	q := New(sqlDB)
	rows, err := q.ListSessions(context.Background(), ListSessionsParams{
		AgentVersionID:  "ver-1",
		IncludeChildren: true,
	})
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("len(rows) = %d, want 2", len(rows))
	}
}
