package store

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/phrony-platform/runtime/internal/model"
)

func sessionEventMockRows(now time.Time) *sqlmock.Rows {
	return sqlmock.NewRows([]string{"id", "session_id", "type", "payload", "created_at"})
}

func addSessionEventRow(rows *sqlmock.Rows, id int64, sessionID, typ string, payload json.RawMessage, now time.Time) *sqlmock.Rows {
	if len(payload) == 0 {
		payload = json.RawMessage("{}")
	}
	return rows.AddRow(id, sessionID, typ, payload, now)
}

func TestQueries_InsertSessionEvent(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	payload := json.RawMessage(`{"role":"user","content":"hello"}`)
	mock.ExpectQuery(`INSERT INTO session_events`).
		WithArgs("sess-1", string(model.SessionEventUserMessage), payload).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(42)))

	q := New(sqlDB)
	id, err := q.InsertSessionEvent(context.Background(), InsertSessionEventParams{
		SessionID: "sess-1",
		Type:      string(model.SessionEventUserMessage),
		Payload:   payload,
	})
	if err != nil {
		t.Fatalf("InsertSessionEvent: %v", err)
	}
	if id != 42 {
		t.Fatalf("id = %d, want 42", id)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestQueries_InsertSessionEvent_emptyPayloadDefaultsToObject(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	mock.ExpectQuery(`INSERT INTO session_events`).
		WithArgs("sess-1", string(model.SessionEventSessionCompleted), json.RawMessage("{}")).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(1)))

	q := New(sqlDB)
	if _, err := q.InsertSessionEvent(context.Background(), InsertSessionEventParams{
		SessionID: "sess-1",
		Type:      string(model.SessionEventSessionCompleted),
	}); err != nil {
		t.Fatalf("InsertSessionEvent: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestQueries_ListSessionEventsBySessionID_ordering(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	now := time.Now()
	rows := sessionEventMockRows(now)
	addSessionEventRow(rows, 1, "sess-1", string(model.SessionEventUserMessage), json.RawMessage(`{"role":"user","content":"go"}`), now)
	addSessionEventRow(rows, 2, "sess-1", string(model.SessionEventToolCall), json.RawMessage(`{"toolCall":{}}`), now.Add(time.Millisecond))
	addSessionEventRow(rows, 3, "sess-1", string(model.SessionEventPolicyDenied), json.RawMessage(`{"toolResult":{}}`), now.Add(2*time.Millisecond))
	mock.ExpectQuery(`FROM session_events`).WithArgs("sess-1").WillReturnRows(rows)

	q := New(sqlDB)
	list, err := q.ListSessionEventsBySessionID(context.Background(), "sess-1")
	if err != nil {
		t.Fatalf("ListSessionEventsBySessionID: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("len = %d, want 3", len(list))
	}
	wantTypes := []string{
		string(model.SessionEventUserMessage),
		string(model.SessionEventToolCall),
		string(model.SessionEventPolicyDenied),
	}
	for i, ev := range list {
		if ev.ID != int64(i+1) {
			t.Fatalf("event[%d].id = %d, want %d", i, ev.ID, i+1)
		}
		if ev.Type != wantTypes[i] {
			t.Fatalf("event[%d].type = %q, want %q", i, ev.Type, wantTypes[i])
		}
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}
