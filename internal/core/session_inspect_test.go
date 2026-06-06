package core

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
	"github.com/phrony-platform/runtime/internal/executor"
	"github.com/phrony-platform/runtime/internal/model"
	"github.com/phrony-platform/runtime/internal/sessionids"
	"github.com/phrony-platform/runtime/internal/store"
	"google.golang.org/grpc/codes"
)

const inspectTestManifestJSON = `{
	"apiVersion":"phrony.com/v1",
	"kind":"Agent",
	"metadata":{"name":"echo-agent","namespace":"demo","version":"1.2.0"},
	"spec":{
		"purpose":"p",
		"instructions":{"text":"System."},
		"model":{"provider":"stub","name":"stub-script"},
		"limits":{"max_tokens_per_run":1000,"max_wall_clock_seconds":300}
	}
}`

func expectInspectSessionRootMocks(mock sqlmock.Sqlmock, sessionID string, now time.Time) {
	mock.ExpectQuery(`FROM sessions`).WithArgs(sessionID).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "agent_version_id", "input", "status", "output", "error", "history", "created_at", "updated_at",
		}).AddRow(sessionID, "ver-1", []byte(`{"q":"hi"}`), model.SessionStatusCompleted,
			[]byte(`{"message":"ok","stop_reason":"end_turn","turn_usage":{"input_tokens":10,"output_tokens":5},"session_usage":{"input_tokens":10,"output_tokens":5},"turns":[{"stop_reason":"end_turn","turn_usage":{"input_tokens":10,"output_tokens":5},"turn_duration_ms":250}]}`),
			nil, []byte(`[{"role":"user","content":"hi"},{"role":"assistant","content":"ok"}]`), now, now))

	mock.ExpectQuery(`parent_session_id, bundle_version_id, depth`).WithArgs(sessionID).
		WillReturnRows(sqlmock.NewRows([]string{"parent_session_id", "bundle_version_id", "depth"}).
			AddRow(nil, nil, 0))

	mock.ExpectQuery(`FROM agent_versions av`).WithArgs("ver-1").
		WillReturnRows(sqlmock.NewRows([]string{"namespace", "name", "version", "manifest"}).
			AddRow("demo", "echo-agent", "1.2.0", []byte(inspectTestManifestJSON)))

	mock.ExpectQuery(`FROM session_evidence`).WithArgs(sessionID).
		WillReturnError(sql.ErrNoRows)

	mock.ExpectQuery(`FROM session_events`).WithArgs(sessionID).
		WillReturnRows(sqlmock.NewRows([]string{"id", "session_id", "type", "payload", "created_at"}).
			AddRow(int64(1), sessionID, string(model.SessionEventUserMessage), []byte(`{"role":"user","content":"hi"}`), now))

	mock.ExpectQuery(`FROM tool_invocations`).WithArgs(sessionID).
		WillReturnRows(sqlmock.NewRows([]string{
			"call_id", "session_id", "agent_version_id", "turn", "tool", "version", "args",
			"result", "status", "worker_identity", "image_digest", "descriptor_hash",
			"manifest_content_hash", "attempt", "error_code", "error_message",
			"usage_input_tokens", "usage_output_tokens", "usage_estimated",
			"created_at", "updated_at", "dispatched_at", "completed_at",
		}).AddRow(
			"call-1", sessionID, "ver-1", 1, "tools.echo", "v1", []byte(`{"x":1}`),
			[]byte(`{"ok":true}`), model.ToolInvocationSucceeded,
			"worker-1", "sha256:abc", "desc-hash", "manifest-hash", 1, nil, nil,
			3, 7, false,
			now, now, now, now,
		))

	mock.ExpectQuery(`FROM approvals`).WithArgs("", "", sessionID, "", "").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "session_id", "call_id", "status", "route", "reason", "decided_by", "comment",
			"created_at", "decided_at",
			"tool", "version", "args", "authority_ref", "policy_name",
			"approvals_required", "approvals_received", "comprehension_required",
			"on_reject", "on_modify", "expires_at", "policy_runtime",
		}))
}

func TestRuntime_InspectSession_validation(t *testing.T) {
	srv := &runtimeServer{db: mustTestDB(t)}
	_, err := srv.InspectSession(context.Background(), &runtimev1.InspectSessionRequest{})
	assertGRPCCode(t, err, codes.InvalidArgument)
}

func TestRuntime_InspectSession_notFound(t *testing.T) {
	db, mock := testSQLxDB(t)
	srv := &runtimeServer{db: db}
	mock.ExpectQuery(`FROM sessions`).WithArgs("missing").WillReturnError(sql.ErrNoRows)

	_, err := srv.InspectSession(context.Background(), &runtimev1.InspectSessionRequest{SessionId: "missing"})
	assertGRPCCode(t, err, codes.NotFound)
}

func TestRuntime_InspectSession_success(t *testing.T) {
	db, mock := testSQLxDB(t)
	srv := &runtimeServer{db: db}
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)

	mock.ExpectQuery(`FROM sessions`).WithArgs("root-sess").WillReturnRows(sqlmock.NewRows([]string{
		"id", "agent_version_id", "input", "status", "output", "error", "history", "created_at", "updated_at",
	}).AddRow("root-sess", "ver-1", []byte(`{}`), model.SessionStatusCompleted, nil, nil, []byte(`[]`), now, now))

	mock.ExpectQuery(`WITH RECURSIVE descendants`).WithArgs("root-sess").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("root-sess"))

	expectInspectSessionRootMocks(mock, "root-sess", now)

	resp, err := srv.InspectSession(context.Background(), &runtimev1.InspectSessionRequest{SessionId: "root-sess"})
	if err != nil {
		t.Fatalf("InspectSession: %v", err)
	}
	sess := resp.GetSession()
	if sess.GetId() != "root-sess" {
		t.Fatalf("id = %q", sess.GetId())
	}
	if sess.GetAgent().GetNamespace() != "demo" || sess.GetAgent().GetModelProvider() != "stub" {
		t.Fatalf("agent = %+v", sess.GetAgent())
	}
	if sess.GetOutput().GetMessage() != "ok" || sess.GetOutput().GetTurns()[0].GetTurnDurationMs() != 250 {
		t.Fatalf("output = %+v", sess.GetOutput())
	}
	if len(sess.GetHistory()) != 2 {
		t.Fatalf("history len = %d", len(sess.GetHistory()))
	}
	if len(sess.GetInvocations()) != 1 || sess.GetInvocations()[0].GetQueueDelayMs() < 0 {
		t.Fatalf("invocations = %+v", sess.GetInvocations())
	}
	if len(sess.GetTimeline()) < 2 {
		t.Fatalf("timeline = %+v", sess.GetTimeline())
	}
	if sess.GetSessionEndedAtUnixMs() != now.UnixMilli() {
		t.Fatalf("session_ended_at_unix_ms = %d, want %d", sess.GetSessionEndedAtUnixMs(), now.UnixMilli())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestRuntime_InspectSession_treeWithChild(t *testing.T) {
	db, mock := testSQLxDB(t)
	srv := &runtimeServer{db: db}
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	parent := "root-sess"
	child := "child-sess"

	mock.ExpectQuery(`FROM sessions`).WithArgs(parent).WillReturnRows(sqlmock.NewRows([]string{
		"id", "agent_version_id", "input", "status", "output", "error", "history", "created_at", "updated_at",
	}).AddRow(parent, "ver-1", []byte(`{}`), model.SessionStatusCompleted, nil, nil, []byte(`[]`), now, now))

	mock.ExpectQuery(`WITH RECURSIVE descendants`).WithArgs(parent).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(parent).AddRow(child))

	expectInspectSessionRootMocks(mock, parent, now)

	mock.ExpectQuery(`FROM sessions`).WithArgs(child).WillReturnRows(sqlmock.NewRows([]string{
		"id", "agent_version_id", "input", "status", "output", "error", "history", "created_at", "updated_at",
	}).AddRow(child, "ver-2", []byte(`{}`), model.SessionStatusCompleted, nil, nil, []byte(`[]`), now, now))

	mock.ExpectQuery(`parent_session_id, bundle_version_id, depth`).WithArgs(child).
		WillReturnRows(sqlmock.NewRows([]string{"parent_session_id", "bundle_version_id", "depth"}).
			AddRow(parent, nil, 1))

	mock.ExpectQuery(`FROM agent_versions av`).WithArgs("ver-2").
		WillReturnRows(sqlmock.NewRows([]string{"namespace", "name", "version", "manifest"}).
			AddRow("demo", "child-agent", "1.0.0", []byte(inspectTestManifestJSON)))

	mock.ExpectQuery(`FROM session_evidence`).WithArgs(child).WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery(`FROM session_events`).WithArgs(child).
		WillReturnRows(sqlmock.NewRows([]string{"id", "session_id", "type", "payload", "created_at"}))
	mock.ExpectQuery(`FROM tool_invocations`).WithArgs(child).
		WillReturnRows(sqlmock.NewRows([]string{
			"call_id", "session_id", "agent_version_id", "turn", "tool", "version", "args",
			"result", "status", "worker_identity", "image_digest", "descriptor_hash",
			"manifest_content_hash", "attempt", "error_code", "error_message",
			"usage_input_tokens", "usage_output_tokens", "usage_estimated",
			"created_at", "updated_at", "dispatched_at", "completed_at",
		}))
	mock.ExpectQuery(`FROM approvals`).WithArgs("", "", child, "", "").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "session_id", "call_id", "status", "route", "reason", "decided_by", "comment",
			"created_at", "decided_at",
			"tool", "version", "args", "authority_ref", "policy_name",
			"approvals_required", "approvals_received", "comprehension_required",
			"on_reject", "on_modify", "expires_at", "policy_runtime",
		}))

	resp, err := srv.InspectSession(context.Background(), &runtimev1.InspectSessionRequest{SessionId: parent})
	if err != nil {
		t.Fatalf("InspectSession: %v", err)
	}
	if len(resp.GetSession().GetChildren()) != 1 || resp.GetSession().GetChildren()[0].GetId() != child {
		t.Fatalf("children = %+v", resp.GetSession().GetChildren())
	}
	if resp.GetSession().GetChildren()[0].GetParentSessionId() != parent {
		t.Fatalf("child parent = %q", resp.GetSession().GetChildren()[0].GetParentSessionId())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestSessionEventSummary_agentDelegation(t *testing.T) {
	agent := agentDelegatingAgent()
	agent.Spec.Tools[0].Agent.Version = "1.0.0"
	msg := toolCallServerMsg(executor.ToolCallEvent{
		CallID:  "call-delegate-1",
		Tool:    "support.billing",
		Version: "1.0.0",
		Args:    json.RawMessage(`{"task":"explain refund"}`),
	}, agent)
	payload := marshalSessionEventProto(msg)

	got := sessionEventSummary(string(model.SessionEventToolCall), payload)
	wantChild := sessionids.ChildFromCallID("call-delegate-1")
	want := "agent_delegation target=support.billing@1.0.0 child_session_id=" + wantChild + " call_id=call-delegate-1"
	if got != want {
		t.Fatalf("summary = %q, want %q", got, want)
	}
}

func TestBuildInspectTimeline_orderingAndGaps(t *testing.T) {
	t1 := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	t2 := t1.Add(2 * time.Second)
	events := []*runtimev1.SessionEventEntry{
		{Id: 1, Type: string(model.SessionEventUserMessage), CreatedAt: formatTime(t1), Payload: json.RawMessage(`{"role":"user","content":"hi"}`)},
		{Id: 2, Type: string(model.SessionEventAssistantMessage), CreatedAt: formatTime(t2), Payload: json.RawMessage(`{"role":"assistant","content":"ok"}`)},
	}
	invocations := []*runtimev1.ToolInvocationEntry{
		{
			CallId:              "call-1",
			Tool:                "tools.echo",
			Version:             "v1",
			Status:              model.ToolInvocationSucceeded,
			CreatedAt:           formatTime(t1),
			DispatchedAt:        formatTime(t1.Add(100 * time.Millisecond)),
			CompletedAt:         formatTime(t2),
			QueueDelayMs:        100,
			ExecutionDurationMs: 400,
		},
	}
	timeline := buildInspectTimeline(events, invocations, nil)
	if len(timeline) < 4 {
		t.Fatalf("timeline len = %d", len(timeline))
	}
	if timeline[0].GetGapMs() != 0 {
		t.Fatalf("first gap_ms = %d, want 0", timeline[0].GetGapMs())
	}
	foundGap := false
	for _, entry := range timeline {
		if entry.GetGapMs() == 2000 {
			foundGap = true
			break
		}
	}
	if !foundGap {
		t.Fatalf("expected 500ms gap in timeline: %+v", timeline)
	}
}

func TestToolInvocationToProto_delays(t *testing.T) {
	created := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	dispatched := created.Add(2 * time.Second)
	completed := dispatched.Add(3 * time.Second)
	entry := toolInvocationToProto(storeToolInvocationForTest(created, dispatched, completed))
	if entry.GetQueueDelayMs() != 2000 {
		t.Fatalf("queue_delay_ms = %d", entry.GetQueueDelayMs())
	}
	if entry.GetExecutionDurationMs() != 3000 {
		t.Fatalf("execution_duration_ms = %d", entry.GetExecutionDurationMs())
	}
	if entry.GetTotalDurationMs() != 5000 {
		t.Fatalf("total_duration_ms = %d", entry.GetTotalDurationMs())
	}
}

func storeToolInvocationForTest(created, dispatched, completed time.Time) store.ToolInvocation {
	return store.ToolInvocation{
		CallID:         "call-1",
		SessionID:      "sess-1",
		AgentVersionID: "ver-1",
		Turn:           1,
		Tool:           "tools.echo",
		Version:        "v1",
		Status:         model.ToolInvocationSucceeded,
		CreatedAt:      created,
		UpdatedAt:      completed,
		DispatchedAt:   &dispatched,
		CompletedAt:    &completed,
	}
}
