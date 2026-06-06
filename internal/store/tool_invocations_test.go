package store

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/phrony-platform/runtime/internal/model"
)

func TestQueries_InsertToolInvocationDispatched(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	mock.ExpectQuery(`INSERT INTO tool_invocations`).
		WithArgs(
			"call-1", "sess-1", "av-1", 0, "tools.echo", "v1",
			json.RawMessage(`{"x":1}`),
			model.ToolInvocationDispatched,
			"spiffe://worker", "sha256:abc", "desc-hash", "manifest-hash", 1,
		).
		WillReturnRows(sqlmock.NewRows([]string{"call_id"}).AddRow("call-1"))

	q := New(sqlDB)
	id, err := q.InsertToolInvocationDispatched(context.Background(), InsertToolInvocationDispatchedParams{
		CallID:              "call-1",
		SessionID:           "sess-1",
		AgentVersionID:      "av-1",
		Turn:                0,
		Tool:                "tools.echo",
		Version:             "v1",
		Args:                json.RawMessage(`{"x":1}`),
		Status:              model.ToolInvocationDispatched,
		WorkerIdentity:      "spiffe://worker",
		ImageDigest:         "sha256:abc",
		DescriptorHash:      "desc-hash",
		ManifestContentHash: "manifest-hash",
	})
	if err != nil {
		t.Fatalf("InsertToolInvocationDispatched: %v", err)
	}
	if id != "call-1" {
		t.Fatalf("id = %q", id)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestQueries_CompleteToolInvocation(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	now := time.Now()
	mock.ExpectQuery(`UPDATE tool_invocations`).
		WithArgs("call-1", model.ToolInvocationSucceeded, json.RawMessage(`{"ok":true}`), nil, nil, 0, 0, false).
		WillReturnRows(sqlmock.NewRows([]string{"updated_at"}).AddRow(now))

	q := New(sqlDB)
	_, err = q.CompleteToolInvocation(context.Background(), CompleteToolInvocationParams{
		CallID: "call-1",
		Status: model.ToolInvocationSucceeded,
		Result: json.RawMessage(`{"ok":true}`),
	})
	if err != nil {
		t.Fatalf("CompleteToolInvocation: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func toolInvocationMockRows(now time.Time) *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"call_id", "session_id", "agent_version_id", "turn", "tool", "version", "args",
		"result", "status", "worker_identity", "image_digest", "descriptor_hash",
		"manifest_content_hash", "attempt", "error_code", "error_message",
		"usage_input_tokens", "usage_output_tokens", "usage_estimated",
		"created_at", "updated_at", "dispatched_at", "completed_at",
	})
}

func addToolInvocationRow(rows *sqlmock.Rows, callID, sessionID, status string, now time.Time) *sqlmock.Rows {
	return rows.AddRow(
		callID, sessionID, "av-1", 1, "tools.echo", "v1", []byte(`{"x":1}`),
		nil, status,
		"", "", "", "", 1, nil, nil,
		0, 0, false,
		now, now, nil, nil,
	)
}

func TestQueries_InsertToolInvocationPending(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	mock.ExpectQuery(`INSERT INTO tool_invocations`).
		WithArgs("call-p", "sess-1", "av-1", 2, "tools.echo", "v1", json.RawMessage(`{"x":1}`), model.ToolInvocationPending).
		WillReturnRows(sqlmock.NewRows([]string{"call_id"}).AddRow("call-p"))

	q := New(sqlDB)
	id, err := q.InsertToolInvocationPending(context.Background(), InsertToolInvocationPendingParams{
		CallID:         "call-p",
		SessionID:      "sess-1",
		AgentVersionID: "av-1",
		Turn:           2,
		Tool:           "tools.echo",
		Version:        "v1",
		Args:           json.RawMessage(`{"x":1}`),
		Status:         model.ToolInvocationPending,
	})
	if err != nil {
		t.Fatalf("InsertToolInvocationPending: %v", err)
	}
	if id != "call-p" {
		t.Fatalf("id = %q", id)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestQueries_ListToolInvocationsBySessionID(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	now := time.Now()
	rows := toolInvocationMockRows(now)
	addToolInvocationRow(rows, "call-1", "sess-1", model.ToolInvocationSucceeded, now)
	addToolInvocationRow(rows, "call-2", "sess-1", model.ToolInvocationDispatched, now)
	mock.ExpectQuery(`FROM tool_invocations`).WithArgs("sess-1").WillReturnRows(rows)

	q := New(sqlDB)
	list, err := q.ListToolInvocationsBySessionID(context.Background(), "sess-1")
	if err != nil {
		t.Fatalf("ListToolInvocationsBySessionID: %v", err)
	}
	if len(list) != 2 || list[0].CallID != "call-1" || list[1].CallID != "call-2" {
		t.Fatalf("list = %+v", list)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestQueries_ListUnfinishedInvocationsBySession(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	now := time.Now()
	rows := toolInvocationMockRows(now)
	addToolInvocationRow(rows, "call-1", "sess-1", model.ToolInvocationQueued, now)
	mock.ExpectQuery(`FROM tool_invocations`).WithArgs("sess-1").WillReturnRows(rows)

	q := New(sqlDB)
	list, err := q.ListUnfinishedInvocationsBySession(context.Background(), "sess-1")
	if err != nil {
		t.Fatalf("ListUnfinishedInvocationsBySession: %v", err)
	}
	if len(list) != 1 || list[0].CallID != "call-1" {
		t.Fatalf("list = %+v", list)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestQueries_ListSessionsForRecovery(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	now := time.Now()
	mock.ExpectQuery(`FROM sessions`).WillReturnRows(sqlmock.NewRows([]string{
		"id", "agent_version_id", "input", "status", "output", "error", "history", "created_at", "updated_at",
	}).AddRow("sess-1", "av-1", []byte(`{}`), model.SessionStatusAwaitingTool, nil, nil, []byte(`[]`), now, now))

	q := New(sqlDB)
	sessions, err := q.ListSessionsForRecovery(context.Background())
	if err != nil {
		t.Fatalf("ListSessionsForRecovery: %v", err)
	}
	if len(sessions) != 1 || sessions[0].ID != "sess-1" {
		t.Fatalf("sessions = %+v", sessions)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestQueries_GetToolInvocation(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	now := time.Now()
	rows := toolInvocationMockRows(now)
	addToolInvocationRow(rows, "call-1", "sess-1", model.ToolInvocationSucceeded, now)
	mock.ExpectQuery(`FROM tool_invocations`).WithArgs("call-1").WillReturnRows(rows)

	q := New(sqlDB)
	inv, err := q.GetToolInvocation(context.Background(), "call-1")
	if err != nil {
		t.Fatalf("GetToolInvocation: %v", err)
	}
	if inv.CallID != "call-1" || inv.Status != model.ToolInvocationSucceeded {
		t.Fatalf("inv = %+v", inv)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestQueries_MarkToolInvocationIndeterminate(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	mock.ExpectQuery(`UPDATE tool_invocations`).
		WithArgs("call-1", model.ToolInvocationIndeterminate, "indeterminate", "worker dropped").
		WillReturnRows(sqlmock.NewRows([]string{"call_id"}).AddRow("call-1"))

	q := New(sqlDB)
	if err := q.MarkToolInvocationIndeterminate(context.Background(), "call-1", "worker dropped"); err != nil {
		t.Fatalf("MarkToolInvocationIndeterminate: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestQueries_GetAgentVersionContentHash(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	mock.ExpectQuery(`SELECT content_hash`).WithArgs("av-1").
		WillReturnRows(sqlmock.NewRows([]string{"content_hash"}).AddRow("hash-abc"))

	q := New(sqlDB)
	hash, err := q.GetAgentVersionContentHash(context.Background(), "av-1")
	if err != nil {
		t.Fatalf("GetAgentVersionContentHash: %v", err)
	}
	if hash != "hash-abc" {
		t.Fatalf("hash = %q", hash)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}
