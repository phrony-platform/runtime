package store

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func eventMockRows() *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"id", "session_id", "root_session_id", "seq", "ts", "type",
		"turn", "call_id", "child_session_id", "actor", "payload",
	})
}

func addEventRow(rows *sqlmock.Rows, id int64, sessionID, rootID string, seq int, ts time.Time, typ, actor string, payload json.RawMessage) *sqlmock.Rows {
	if len(payload) == 0 {
		payload = json.RawMessage("{}")
	}
	return rows.AddRow(id, sessionID, rootID, seq, ts, typ, nil, nil, nil, actor, payload)
}

func TestQueries_InsertEvent(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	payload := json.RawMessage(`{"content":"hello"}`)
	mock.ExpectQuery(`INSERT INTO events`).
		WithArgs("sess-1", "root-1", 1, "message.user", nil, nil, nil, "user", payload).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(42)))

	q := New(sqlDB)
	id, err := q.InsertEvent(context.Background(), InsertEventParams{
		SessionID:     "sess-1",
		RootSessionID: "root-1",
		Seq:           1,
		Type:          "message.user",
		Actor:         "user",
		Payload:       payload,
	})
	if err != nil {
		t.Fatalf("InsertEvent: %v", err)
	}
	if id != 42 {
		t.Fatalf("id = %d, want 42", id)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestQueries_InsertEvent_emptyPayloadDefaultsToObject(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	mock.ExpectQuery(`INSERT INTO events`).
		WithArgs("sess-1", "root-1", 2, "session.completed", nil, nil, nil, "", json.RawMessage("{}")).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(1)))

	q := New(sqlDB)
	if _, err := q.InsertEvent(context.Background(), InsertEventParams{
		SessionID:     "sess-1",
		RootSessionID: "root-1",
		Seq:           2,
		Type:          "session.completed",
	}); err != nil {
		t.Fatalf("InsertEvent: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestQueries_NextSessionSeq(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	mock.ExpectQuery(`UPDATE sessions`).
		WithArgs("sess-1").
		WillReturnRows(sqlmock.NewRows([]string{"event_seq"}).AddRow(3))

	q := New(sqlDB)
	seq, err := q.NextSessionSeq(context.Background(), "sess-1")
	if err != nil {
		t.Fatalf("NextSessionSeq: %v", err)
	}
	if seq != 3 {
		t.Fatalf("seq = %d, want 3", seq)
	}
}

func TestQueries_ListEventsBySession_ordering(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	now := time.Now()
	rows := eventMockRows()
	addEventRow(rows, 1, "sess-1", "root-1", 1, now, "message.user", "user", json.RawMessage(`{"content":"go"}`))
	addEventRow(rows, 2, "sess-1", "root-1", 2, now.Add(time.Millisecond), "tool.requested", "agent", json.RawMessage(`{"tool":"search"}`))
	mock.ExpectQuery(`FROM events`).WithArgs("sess-1").WillReturnRows(rows)

	q := New(sqlDB)
	list, err := q.ListEventsBySession(context.Background(), "sess-1")
	if err != nil {
		t.Fatalf("ListEventsBySession: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("len = %d, want 2", len(list))
	}
	if list[0].Seq != 1 || list[1].Seq != 2 {
		t.Fatalf("seq order = [%d, %d], want [1, 2]", list[0].Seq, list[1].Seq)
	}
}

func TestQueries_ListEventsByRoot_mergedTimeline(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	now := time.Now()
	rows := eventMockRows()
	addEventRow(rows, 1, "root-1", "root-1", 1, now, "message.user", "user", json.RawMessage(`{}`))
	addEventRow(rows, 2, "child-1", "root-1", 1, now.Add(time.Millisecond), "session.started", "system", json.RawMessage(`{}`))
	mock.ExpectQuery(`FROM events`).WithArgs("root-1").WillReturnRows(rows)

	q := New(sqlDB)
	list, err := q.ListEventsByRoot(context.Background(), "root-1")
	if err != nil {
		t.Fatalf("ListEventsByRoot: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("len = %d, want 2", len(list))
	}
	if list[0].SessionID != "root-1" || list[1].SessionID != "child-1" {
		t.Fatalf("session order = [%q, %q]", list[0].SessionID, list[1].SessionID)
	}
}
