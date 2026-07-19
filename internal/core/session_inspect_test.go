package core

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
	"github.com/phrony-platform/runtime/internal/executor"
	"github.com/phrony-platform/runtime/internal/model"
	"github.com/phrony-platform/runtime/internal/sessionids"
	"github.com/phrony-platform/runtime/internal/store"
	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/structpb"
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
	output := json.RawMessage(`{"message":"ok","stop_reason":"end_turn","turn_usage":{"input_tokens":10,"output_tokens":5},"session_usage":{"input_tokens":10,"output_tokens":5},"turns":[{"stop_reason":"end_turn","turn_usage":{"input_tokens":10,"output_tokens":5},"turn_duration_ms":250}]}`)
	events := foldEventsFromOutputJSON(sessionID, output)
	events[0].Payload = userMessagePayload("hi")

	mock.ExpectQuery(`FROM sessions`).WithArgs(sessionID).
		WillReturnRows(sessionMockRows(sessionID, "ver-1", model.SessionStatusCompleted, []byte(`{"q":"hi"}`), nil, sessionID, len(events), now, now))

	mock.ExpectQuery(`parent_session_id, bundle_version_id, depth`).WithArgs(sessionID).
		WillReturnRows(sqlmock.NewRows([]string{"parent_session_id", "bundle_version_id", "depth"}).
			AddRow(nil, nil, 0))

	expectListEventsBySession(mock, sessionID, events, now)

	mock.ExpectQuery(`FROM agent_versions av`).WithArgs("ver-1").
		WillReturnRows(sqlmock.NewRows([]string{"namespace", "name", "version", "manifest"}).
			AddRow("demo", "echo-agent", "1.2.0", []byte(inspectTestManifestJSON)))

	expectListEventsBySession(mock, sessionID, events, now)
}

func expectInspectSessionTimelineMocks(mock sqlmock.Sqlmock, sessionID string, now time.Time, withInvocation bool) {
	invRows := sqlmock.NewRows([]string{
		"call_id", "session_id", "agent_version_id", "turn", "tool", "version", "args",
		"result", "status", "worker_identity", "image_digest", "descriptor_hash",
		"manifest_content_hash", "attempt", "error_code", "error_message",
		"usage_input_tokens", "usage_output_tokens", "usage_estimated",
		"created_at", "updated_at", "dispatched_at", "completed_at",
	})
	if withInvocation {
		invRows = invRows.AddRow(
			"call-1", sessionID, "ver-1", 1, "tools.echo", "v1", []byte(`{"x":1}`),
			[]byte(`{"ok":true}`), model.ToolInvocationSucceeded,
			"worker-1", "sha256:abc", "desc-hash", "manifest-hash", 1, nil, nil,
			3, 7, false,
			now, now, now, now,
		)
	}
	mock.ExpectQuery(`FROM tool_invocations`).WithArgs(sessionID).WillReturnRows(invRows)

	mock.ExpectQuery(`FROM approvals`).WithArgs("", "", sessionID, "", "").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "session_id", "call_id", "status", "route", "reason", "decided_by", "comment",
			"created_at", "decided_at",
			"tool", "version", "args", "authority_ref", "policy_name",
			"approvals_required", "approvals_received", "comprehension_required",
			"on_reject", "on_modify", "expires_at", "policy_runtime",
		}))
}

func mustInspectProtoValue(t *testing.T, raw string) *structpb.Value {
	t.Helper()
	v, err := jsonToProtoValue([]byte(raw))
	if err != nil {
		t.Fatalf("jsonToProtoValue: %v", err)
	}
	return v
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

	mock.ExpectQuery(`FROM sessions`).WithArgs("root-sess").WillReturnRows(sessionMockRows(
		"root-sess", "ver-1", model.SessionStatusCompleted, []byte(`{}`), nil, "root-sess", 2, now, now,
	))

	mock.ExpectQuery(`WITH RECURSIVE descendants`).WithArgs("root-sess").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("root-sess"))

	expectInspectSessionRootMocks(mock, "root-sess", now)

	parentEv := store.Event{ID: 1, SessionID: "root-sess", RootSessionID: "root-sess", Seq: 1, TS: now, Type: EventMessageUser, Actor: ActorUser, Payload: userMessagePayload("hi")}
	expectListEventsByRoot(mock, "root-sess", []store.Event{parentEv}, now)
	expectInspectSessionTimelineMocks(mock, "root-sess", now, true)

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
	if sess.GetInput() == nil || sess.GetInput().GetStructValue() == nil {
		t.Fatalf("input = %+v", sess.GetInput())
	}
	if len(sess.GetChildren()) != 0 {
		t.Fatalf("children = %+v", sess.GetChildren())
	}
	timeline := resp.GetTimeline()
	if len(timeline) == 0 {
		t.Fatal("expected timeline")
	}
	if timeline[0].GetTsUnixMs() == 0 {
		t.Fatalf("timeline missing ts_unix_ms: %+v", timeline[0])
	}
	foundReadableEvent := false
	foundInvocation := false
	for _, entry := range timeline {
		if entry.GetSource() == "event" && entry.GetEvent() != nil && entry.GetEvent().GetPayload() != nil {
			foundReadableEvent = true
		}
		if entry.GetSource() == "invocation" {
			foundInvocation = true
			if entry.GetInvocation() == nil || entry.GetInvocation().GetArgs() == nil {
				t.Fatalf("invocation missing readable args: %+v", entry.GetInvocation())
			}
		}
	}
	if !foundReadableEvent {
		t.Fatal("expected readable event payload in timeline")
	}
	if !foundInvocation {
		t.Fatal("expected invocation milestones in timeline")
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

	mock.ExpectQuery(`FROM sessions`).WithArgs(parent).WillReturnRows(sessionMockRows(
		parent, "ver-1", model.SessionStatusCompleted, []byte(`{}`), nil, parent, 2, now, now,
	))

	mock.ExpectQuery(`WITH RECURSIVE descendants`).WithArgs(parent).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(parent).AddRow(child))

	expectInspectSessionRootMocks(mock, parent, now)

	mock.ExpectQuery(`FROM sessions`).WithArgs(child).WillReturnRows(sessionMockRows(
		child, "ver-2", model.SessionStatusCompleted, []byte(`{}`), nil, parent, 1, now, now,
	))

	mock.ExpectQuery(`parent_session_id, bundle_version_id, depth`).WithArgs(child).
		WillReturnRows(sqlmock.NewRows([]string{"parent_session_id", "bundle_version_id", "depth"}).
			AddRow(parent, nil, 1))

	expectListEventsBySession(mock, child, nil, now)

	mock.ExpectQuery(`FROM agent_versions av`).WithArgs("ver-2").
		WillReturnRows(sqlmock.NewRows([]string{"namespace", "name", "version", "manifest"}).
			AddRow("demo", "child-agent", "1.0.0", []byte(inspectTestManifestJSON)))

	expectListEventsBySession(mock, child, nil, now)

	parentEv := store.Event{ID: 1, SessionID: parent, RootSessionID: parent, Seq: 1, TS: now, Type: EventMessageUser, Actor: ActorUser, Payload: userMessagePayload("parent")}
	childEv := store.Event{ID: 2, SessionID: child, RootSessionID: parent, Seq: 1, TS: now.Add(time.Millisecond), Type: EventSessionStarted, Actor: ActorSystem, Payload: json.RawMessage(`{}`)}
	mock.ExpectQuery(`FROM events`).WithArgs(parent).
		WillReturnRows(sessionEventLogRows(now, parentEv, childEv))

	expectInspectSessionTimelineMocks(mock, parent, now, false)
	expectInspectSessionTimelineMocks(mock, child, now, false)

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
	timeline := resp.GetTimeline()
	if len(timeline) != 2 {
		t.Fatalf("timeline len = %d, want 2", len(timeline))
	}
	if timeline[0].GetSessionId() != parent || timeline[1].GetSessionId() != child {
		t.Fatalf("timeline order = [%q, %q]", timeline[0].GetSessionId(), timeline[1].GetSessionId())
	}
	if timeline[1].GetDepth() != 1 {
		t.Fatalf("child depth = %d, want 1", timeline[1].GetDepth())
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

	got := sessionEventSummary(EventToolRequested, payload)
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
		{Id: 1, Type: EventMessageUser, TsUnixMs: t1.UnixMilli(), Seq: 1, Payload: mustInspectProtoValue(t, `{"role":"user","content":"hi"}`)},
		{Id: 2, Type: EventMessageAssistant, TsUnixMs: t2.UnixMilli(), Seq: 2, Payload: mustInspectProtoValue(t, `{"role":"assistant","content":"ok"}`)},
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
		t.Fatalf("expected 2000ms gap in timeline: %+v", timeline)
	}
	// Same timestamp must not reorder by kind string; event id wins.
	tie1 := time.Date(2026, 1, 1, 13, 0, 0, 0, time.UTC)
	tieEvents := []*runtimev1.SessionEventEntry{
		{Id: 10, Type: EventToolCompleted, TsUnixMs: tie1.UnixMilli(), CreatedAt: formatTime(tie1)},
		{Id: 5, Type: EventMessageUser, TsUnixMs: tie1.UnixMilli(), CreatedAt: formatTime(tie1)},
	}
	tieTimeline := buildInspectTimeline(tieEvents, nil, nil)
	if tieTimeline[0].GetEvent().GetId() != 5 {
		t.Fatalf("tie order = id %d, want lower event id first", tieTimeline[0].GetEvent().GetId())
	}
}

func TestBuildEventTimelineOrder_orderedByTsAndID(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	events := []store.Event{
		{ID: 3, SessionID: "child", RootSessionID: "root", Seq: 1, TS: now.Add(2 * time.Millisecond), Type: EventSessionStarted, Actor: ActorSystem, Payload: json.RawMessage(`{}`)},
		{ID: 1, SessionID: "root", RootSessionID: "root", Seq: 1, TS: now, Type: EventMessageUser, Actor: ActorUser, Payload: userMessagePayload("go")},
		{ID: 2, SessionID: "root", RootSessionID: "root", Seq: 2, TS: now.Add(time.Millisecond), Type: EventToolRequested, Actor: ActorAgent, Payload: json.RawMessage(`{}`)},
	}
	protoEvents, err := sessionEventsToProto(events)
	if err != nil {
		t.Fatalf("sessionEventsToProto: %v", err)
	}
	depth := map[string]int32{"root": 0, "child": 1}
	items := make([]inspectTimelineItem, 0, len(events))
	for i, ev := range events {
		items = append(items, inspectTimelineItem{
			timestamp: ev.TS,
			sortKey:   ev.ID,
			entry: &runtimev1.InspectTimelineEntry{
				Timestamp: formatTime(ev.TS),
				TsUnixMs:  ev.TS.UnixMilli(),
				Seq:       int32(ev.Seq),
				SessionId: ev.SessionID,
				Depth:     depth[ev.SessionID],
				Source:    "event",
				Kind:      ev.Type,
				Event:     protoEvents[i],
			},
		})
	}
	merged := finalizeInspectTimeline(items)
	if len(merged) != 3 {
		t.Fatalf("len = %d", len(merged))
	}
	if merged[0].GetSessionId() != "root" || merged[1].GetSessionId() != "root" || merged[2].GetSessionId() != "child" {
		t.Fatalf("order = [%q,%q,%q]", merged[0].GetSessionId(), merged[1].GetSessionId(), merged[2].GetSessionId())
	}
	if merged[2].GetDepth() != 1 {
		t.Fatalf("child depth = %d", merged[2].GetDepth())
	}
	if merged[0].GetEvent().GetPayload() == nil {
		t.Fatal("expected readable payload")
	}
}

func TestToolInvocationToProto_delays(t *testing.T) {
	created := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	dispatched := created.Add(2 * time.Second)
	completed := dispatched.Add(3 * time.Second)
	entry, err := toolInvocationToProto(storeToolInvocationForTest(created, dispatched, completed))
	if err != nil {
		t.Fatalf("toolInvocationToProto: %v", err)
	}
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

func TestJSONToProtoValue(t *testing.T) {
	v, err := jsonToProtoValue([]byte(`{"a":1,"b":"x"}`))
	if err != nil {
		t.Fatalf("jsonToProtoValue: %v", err)
	}
	if v.GetStructValue().Fields["a"].GetNumberValue() != 1 {
		t.Fatalf("a = %+v", v)
	}
	empty, err := jsonToProtoValue(nil)
	if err != nil || empty != nil {
		t.Fatalf("empty = %v err=%v", empty, err)
	}
}

func TestJSONToProtoValue_expandsNestedBase64JSON(t *testing.T) {
	msg := toolCallServerMsg(executor.ToolCallEvent{
		CallID:  "call-1",
		Tool:    "orders.lookup-order",
		Version: "1.0.0",
		Args:    json.RawMessage(`{"order_id":"1"}`),
	}, nil)
	payload := marshalSessionEventProto(msg)

	v, err := jsonToProtoValue(payload)
	if err != nil {
		t.Fatalf("jsonToProtoValue: %v", err)
	}
	toolCall := v.GetStructValue().Fields["toolCall"].GetStructValue()
	if toolCall == nil {
		t.Fatalf("payload = %v", protoValueJSON(v))
	}
	args := toolCall.Fields["args"]
	if args == nil || args.GetStructValue() == nil {
		t.Fatalf("args still not nested JSON: %v", protoValueJSON(v))
	}
	if got := args.GetStructValue().Fields["order_id"].GetStringValue(); got != "1" {
		t.Fatalf("order_id = %q", got)
	}

	entry := &runtimev1.SessionEventEntry{Payload: v}
	out, err := protojson.Marshal(entry)
	if err != nil {
		t.Fatalf("protojson: %v", err)
	}
	if strings.Contains(string(out), "eyJ") {
		t.Fatalf("protojson still contains base64 args: %s", out)
	}
	if !strings.Contains(string(out), `"order_id":"1"`) && !strings.Contains(string(out), `"order_id": "1"`) {
		t.Fatalf("protojson missing readable args: %s", out)
	}

	resultMsg := &runtimev1.RunSessionInteractiveServerMsg{
		Body: &runtimev1.RunSessionInteractiveServerMsg_ToolResult{
			ToolResult: &runtimev1.RunSessionInteractiveToolResult{
				CallId:  "call-1",
				Payload: []byte(`{"ok":true}`),
			},
		},
	}
	resultVal, err := jsonToProtoValue(marshalSessionEventProto(resultMsg))
	if err != nil {
		t.Fatalf("tool result: %v", err)
	}
	tr := resultVal.GetStructValue().Fields["toolResult"].GetStructValue()
	if tr.Fields["payload"].GetStructValue().Fields["ok"].GetBoolValue() != true {
		t.Fatalf("tool result payload = %v", protoValueJSON(resultVal))
	}
}

func TestJSONToProtoValue_leavesOpaqueBase64(t *testing.T) {
	opaque := base64.StdEncoding.EncodeToString([]byte{0x00, 0x01, 0xff})
	raw := []byte(`{"payload":"` + opaque + `"}`)
	v, err := jsonToProtoValue(raw)
	if err != nil {
		t.Fatalf("jsonToProtoValue: %v", err)
	}
	if got := v.GetStructValue().Fields["payload"].GetStringValue(); got != opaque {
		t.Fatalf("payload = %q, want opaque base64 left intact", got)
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
