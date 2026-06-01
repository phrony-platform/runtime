package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestQueries_InsertSessionEvidence_idempotent(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	payload := json.RawMessage(`{"owner":"team"}`)
	mock.ExpectQuery(`INSERT INTO session_evidence`).
		WithArgs("sess-1", payload).
		WillReturnRows(sqlmock.NewRows([]string{"session_id"}).AddRow("sess-1"))
	mock.ExpectQuery(`INSERT INTO session_evidence`).
		WithArgs("sess-1", payload).
		WillReturnRows(sqlmock.NewRows([]string{"session_id"}))

	q := New(sqlDB)
	ctx := context.Background()
	if err := q.InsertSessionEvidence(ctx, "sess-1", payload); err != nil {
		t.Fatalf("first insert: %v", err)
	}
	if err := q.InsertSessionEvidence(ctx, "sess-1", payload); err != nil {
		t.Fatalf("second insert: %v", err)
	}
}

func TestQueries_GetSessionEvidence(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	payload := json.RawMessage(`{"labels":{"a":"b"}}`)
	mock.ExpectQuery(`FROM session_evidence`).
		WithArgs("sess-1").
		WillReturnRows(sqlmock.NewRows([]string{"payload"}).AddRow(payload))

	q := New(sqlDB)
	got, err := q.GetSessionEvidence(context.Background(), "sess-1")
	if err != nil {
		t.Fatalf("GetSessionEvidence: %v", err)
	}
	if string(got) != string(payload) {
		t.Fatalf("payload = %s", got)
	}
}

func TestQueries_GetSessionEvidence_notFound(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	mock.ExpectQuery(`FROM session_evidence`).
		WithArgs("sess-missing").
		WillReturnError(sql.ErrNoRows)

	q := New(sqlDB)
	_, err = q.GetSessionEvidence(context.Background(), "sess-missing")
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("err = %v, want sql.ErrNoRows", err)
	}
}
