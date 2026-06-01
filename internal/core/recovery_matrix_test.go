package core

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/phrony-platform/runtime/internal/executor"
	"github.com/phrony-platform/runtime/internal/manifest"
	"github.com/phrony-platform/runtime/internal/model"
	"github.com/phrony-platform/runtime/internal/policy"
	"github.com/phrony-platform/runtime/internal/provider"
	"github.com/phrony-platform/runtime/internal/providertest"
	"github.com/phrony-platform/runtime/internal/store"
	"github.com/phrony-platform/runtime/internal/tooldispatch"
)

type recordingToolDispatcher struct {
	mu    sync.Mutex
	calls []string
	fn    func(call tooldispatch.ToolCall) (tooldispatch.ToolResult, error)
}

func (d *recordingToolDispatcher) Dispatch(_ context.Context, call tooldispatch.ToolCall) (tooldispatch.ToolResult, error) {
	d.mu.Lock()
	d.calls = append(d.calls, call.CallID)
	fn := d.fn
	d.mu.Unlock()
	if fn != nil {
		return fn(call)
	}
	return tooldispatch.ToolResult{CallID: call.CallID, Payload: json.RawMessage(`{"ok":true}`)}, nil
}

func (d *recordingToolDispatcher) callCount() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.calls)
}

func TestRecoverOutstandingToolInvocations_redispatchesPendingAndQueued(t *testing.T) {
	agent := e2eWeatherAgent(nil)
	ver := executor.NewVersionWithProvider("av-1", agent, providertest.DeltaCompleted())
	disp := &recordingToolDispatcher{}

	db, mock := testSQLxDB(t)
	q := store.New(db)
	srv := &runtimeServer{db: db, toolDispatch: disp}

	invocations := []store.ToolInvocation{
		{
			CallID: "call-pending", SessionID: "sess-1", AgentVersionID: "av-1",
			Turn: 1, Tool: "weather.get-forecast", Version: "1.0.0",
			Args: json.RawMessage(`{}`), Status: model.ToolInvocationPending,
		},
		{
			CallID: "call-queued", SessionID: "sess-1", AgentVersionID: "av-1",
			Turn: 1, Tool: "weather.get-forecast", Version: "1.0.0",
			Args: json.RawMessage(`{}`), Status: model.ToolInvocationQueued,
		},
	}

	mock.ExpectQuery(`FROM tool_invocations`).WithArgs("sess-1").
		WillReturnRows(sqlmock.NewRows(nil))
	now := time.Now()
	for _, callID := range []string{"call-pending", "call-queued"} {
		mock.ExpectQuery(`FROM tool_invocations`).WithArgs(callID).
			WillReturnRows(toolInvocationSQLRows(callID, "sess-1", model.ToolInvocationQueued, nil, now))
	}

	err := srv.recoverOutstandingToolInvocations(
		context.Background(), q, ver,
		store.Session{ID: "sess-1", AgentVersionID: "av-1", Status: model.SessionStatusAwaitingTool},
		[]provider.Message{{Role: provider.RoleUser, Content: "hi"}},
		invocations,
		false,
	)
	if err != nil {
		t.Fatalf("recoverOutstandingToolInvocations: %v", err)
	}
	if disp.callCount() != 2 {
		t.Fatalf("dispatch calls = %d, want 2", disp.callCount())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql: %v", err)
	}
}

func TestRecoverDispatchedInvocation_completedInLedger(t *testing.T) {
	db, mock := testSQLxDB(t)
	q := store.New(db)
	reg := tooldispatch.NewWorkerRegistry(tooldispatch.RegistryConfig{LeaseTTL: 20 * time.Millisecond})
	srv := &runtimeServer{db: db, toolRegistry: reg, toolDispatch: &recordingToolDispatcher{}}

	call := tooldispatch.ToolCall{
		CallID: "call-done", SessionID: "sess-1", AgentVersionID: "av-1",
		Tool: "weather.get-forecast", Version: "1.0.0",
	}
	now := time.Now()
	mock.ExpectQuery(`FROM tool_invocations`).WithArgs("call-done").
		WillReturnRows(toolInvocationSQLRows(
			"call-done", "sess-1", model.ToolInvocationSucceeded, []byte(`{"ok":true}`), now,
		))

	err := srv.recoverDispatchedInvocation(
		context.Background(), q, "sess-1", call,
		manifest.SideEffectReadOnly, 50*time.Millisecond,
		policy.NewEvaluator(e2eWeatherAgent(nil)),
	)
	if err != nil {
		t.Fatalf("recoverDispatchedInvocation: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql: %v", err)
	}
}

func TestRecoverDispatchedInvocation_redispatchesReadOnly(t *testing.T) {
	db, mock := testSQLxDB(t)
	q := store.New(db)
	disp := &recordingToolDispatcher{}
	reg := tooldispatch.NewWorkerRegistry(tooldispatch.RegistryConfig{LeaseTTL: 15 * time.Millisecond})
	srv := &runtimeServer{db: db, toolRegistry: reg, toolDispatch: disp}

	call := tooldispatch.ToolCall{
		CallID: "call-redispatch", SessionID: "sess-1", AgentVersionID: "av-1",
		Tool: "weather.get-forecast", Version: "1.0.0",
	}
	now := time.Now()
	rows := toolInvocationSQLRows("call-redispatch", "sess-1", model.ToolInvocationDispatched, nil, now)
	mock.ExpectQuery(`FROM tool_invocations`).WithArgs("call-redispatch").WillReturnRows(rows)
	mock.ExpectQuery(`FROM tool_invocations`).WithArgs("call-redispatch").WillReturnRows(rows)
	mock.ExpectQuery(`FROM tool_invocations`).WithArgs("call-redispatch").WillReturnRows(rows)

	err := srv.recoverDispatchedInvocation(
		context.Background(), q, "sess-1", call,
		manifest.SideEffectReadOnly, 80*time.Millisecond,
		policy.NewEvaluator(e2eWeatherAgent(nil)),
	)
	if err != nil {
		t.Fatalf("recoverDispatchedInvocation: %v", err)
	}
	if disp.callCount() != 1 {
		t.Fatalf("dispatch calls = %d, want 1", disp.callCount())
	}
}

func TestRecoverDispatchedInvocation_nonIdempotentEscalates(t *testing.T) {
	db, mock := testSQLxDB(t)
	q := store.New(db)
	reg := tooldispatch.NewWorkerRegistry(tooldispatch.RegistryConfig{LeaseTTL: 10 * time.Millisecond})
	srv := &runtimeServer{db: db, toolRegistry: reg, toolDispatch: &recordingToolDispatcher{}}

	call := tooldispatch.ToolCall{
		CallID: "call-hitl", SessionID: "sess-1", AgentVersionID: "av-1",
		Tool: "weather.get-forecast", Version: "1.0.0",
	}
	now := time.Now()
	rows := toolInvocationSQLRows("call-hitl", "sess-1", model.ToolInvocationDispatched, nil, now)
	mock.ExpectQuery(`FROM tool_invocations`).WithArgs("call-hitl").WillReturnRows(rows)
	mock.ExpectQuery(`UPDATE tool_invocations`).WithArgs("call-hitl", model.ToolInvocationIndeterminate, "indeterminate", sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"call_id"}).AddRow("call-hitl"))
	mock.ExpectQuery(`FROM tool_invocations`).WithArgs("call-hitl").
		WillReturnRows(toolInvocationSQLRows("call-hitl", "sess-1", model.ToolInvocationIndeterminate, nil, now))
	mock.ExpectQuery(`INSERT INTO approvals`).WillReturnRows(sqlmock.NewRows([]string{"created_at"}).AddRow(now))
	mock.ExpectQuery(`UPDATE sessions`).WithArgs("sess-1", model.SessionStatusAwaitingApproval, sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"updated_at"}).AddRow(now))

	err := srv.recoverDispatchedInvocation(
		context.Background(), q, "sess-1", call,
		manifest.SideEffectNonIdempotentWrite, 60*time.Millisecond,
		policy.NewEvaluator(e2eWeatherAgent(nil)),
	)
	if err != nil {
		t.Fatalf("recoverDispatchedInvocation: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql: %v", err)
	}
}

func toolInvocationSQLRows(callID, sessionID, status string, result []byte, now time.Time) *sqlmock.Rows {
	var resultVal any
	if len(result) > 0 {
		resultVal = result
	}
	return sqlmock.NewRows([]string{
		"call_id", "session_id", "agent_version_id", "turn", "tool", "version", "args",
		"result", "status", "worker_identity", "image_digest", "descriptor_hash",
		"manifest_content_hash", "attempt", "error_code", "error_message",
		"created_at", "updated_at", "dispatched_at", "completed_at",
	}).AddRow(
		callID, sessionID, "av-1", 1, "weather.get-forecast", "1.0.0", []byte(`{}`),
		resultVal, status,
		"", "", "", "", 1, nil, nil,
		now, now, nil, nil,
	)
}
