package store

import (
	"context"
	"database/sql"
	"encoding/json"
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
			"id", "agent_version_id", "input", "status", "output", "error", "created_at", "updated_at",
		}).AddRow("sess-1", "ver-1", []byte(`{"q":"hi"}`), "running", []byte(`{"answer":"ok"}`), nil, now, now))

	q := New(sqlDB)
	s, err := q.GetSession(context.Background(), "sess-1")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if s.Status != "running" {
		t.Fatalf("status = %q, want running", s.Status)
	}
	if string(s.Output) != `{"answer":"ok"}` {
		t.Fatalf("output = %s", s.Output)
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
		WithArgs("sess-1", "failed", json.RawMessage(`{"partial":true}`), errMsg).
		WillReturnRows(sqlmock.NewRows([]string{"updated_at"}).AddRow(now))

	q := New(sqlDB)
	updatedAt, err := q.UpdateSession(context.Background(), UpdateSessionParams{
		ID:     "sess-1",
		Status: "failed",
		Output: json.RawMessage(`{"partial":true}`),
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
		WithArgs("sess-1", "awaiting_input", nil, nil).
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
