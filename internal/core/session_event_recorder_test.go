package core

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
	"github.com/phrony-platform/runtime/internal/executor"
	"github.com/phrony-platform/runtime/internal/policy"
	"github.com/phrony-platform/runtime/internal/provider"
	"github.com/phrony-platform/runtime/internal/store"
)

type captureSessionEventSink struct {
	msgs []*runtimev1.RunSessionInteractiveServerMsg
}

func (s *captureSessionEventSink) Send(msg *runtimev1.RunSessionInteractiveServerMsg) error {
	s.msgs = append(s.msgs, msg)
	return nil
}

func sessionEventLogRows(now time.Time, events ...store.Event) *sqlmock.Rows {
	rows := sqlmock.NewRows([]string{
		"id", "session_id", "root_session_id", "seq", "ts", "type",
		"turn", "call_id", "child_session_id", "actor", "payload",
	})
	for _, ev := range events {
		payload := ev.Payload
		if len(payload) == 0 {
			payload = json.RawMessage("{}")
		}
		var turn any
		if ev.Turn != nil {
			turn = *ev.Turn
		}
		var callID any
		if ev.CallID != nil {
			callID = *ev.CallID
		}
		var childSessionID any
		if ev.ChildSessionID != nil {
			childSessionID = *ev.ChildSessionID
		}
		rootID := ev.RootSessionID
		if rootID == "" {
			rootID = ev.SessionID
		}
		ts := ev.TS
		if ts.IsZero() {
			ts = now
		}
		rows.AddRow(ev.ID, ev.SessionID, rootID, ev.Seq, ts, ev.Type, turn, callID, childSessionID, ev.Actor, payload)
	}
	return rows
}

func TestReplaySessionEventLog_replaysWireBackedEventsInOrder(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	callMsg := toolCallServerMsg(executor.ToolCallEvent{
		CallID: "call-1", Tool: "weather.get-forecast", Version: "1.0.0", Args: json.RawMessage(`{}`),
	}, nil)
	resultMsg := toolResultServerMsg(executor.ToolResultEvent{
		CallID: "call-1", Payload: json.RawMessage(`{"temp":72}`),
	})
	approvalMsg := approvalRequiredServerMsg(policy.ApprovalRequest{
		ApprovalID: "appr-1", CallID: "call-1", Tool: "weather.get-forecast", Version: "1.0.0",
	})

	now := time.Now()
	mock.ExpectQuery(`FROM events`).WithArgs("sess-1").WillReturnRows(sessionEventLogRows(now,
		store.Event{ID: 1, SessionID: "sess-1", Seq: 1, Type: EventMessageUser, Payload: userMessagePayload("hi")},
		store.Event{ID: 2, SessionID: "sess-1", Seq: 2, Type: EventToolRequested, Payload: marshalSessionEventProto(callMsg)},
		store.Event{ID: 3, SessionID: "sess-1", Seq: 3, Type: EventToolCompleted, Payload: marshalSessionEventProto(resultMsg)},
		store.Event{ID: 4, SessionID: "sess-1", Seq: 4, Type: EventApprovalRequired, Payload: marshalSessionEventProto(approvalMsg)},
		store.Event{ID: 5, SessionID: "sess-1", Seq: 5, Type: EventApprovalDecided, Payload: json.RawMessage(`{"approval_id":"appr-1","approved":false}`)},
		store.Event{ID: 6, SessionID: "sess-1", Seq: 6, Type: EventSessionCompleted, Payload: json.RawMessage(`{"stop_reason":"end_turn"}`)},
	))

	sink := &captureSessionEventSink{}
	if err := replaySessionEventLog(context.Background(), store.New(sqlDB), sink, "sess-1", ""); err != nil {
		t.Fatalf("replaySessionEventLog: %v", err)
	}
	if len(sink.msgs) != 3 {
		t.Fatalf("replayed %d messages, want 3 wire-backed events", len(sink.msgs))
	}
	if sink.msgs[0].GetToolCall().GetCallId() != "call-1" {
		t.Fatalf("first replay = %+v, want tool_call", sink.msgs[0])
	}
	if sink.msgs[1].GetToolResult().GetCallId() != "call-1" {
		t.Fatalf("second replay = %+v, want tool_result", sink.msgs[1])
	}
	if sink.msgs[2].GetApprovalRequired().GetApprovalId() != "appr-1" {
		t.Fatalf("third replay = %+v, want approval_required", sink.msgs[2])
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestReplaySessionEventLog_skipsPendingApproval(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	approvalMsg := approvalRequiredServerMsg(policy.ApprovalRequest{
		ApprovalID: "appr-pending", CallID: "call-1", Tool: "weather.get-forecast", Version: "1.0.0",
	})
	now := time.Now()
	mock.ExpectQuery(`FROM events`).WithArgs("sess-1").WillReturnRows(sessionEventLogRows(now,
		store.Event{ID: 1, SessionID: "sess-1", Seq: 1, Type: EventApprovalRequired, Payload: marshalSessionEventProto(approvalMsg)},
	))

	sink := &captureSessionEventSink{}
	if err := replaySessionEventLog(context.Background(), store.New(sqlDB), sink, "sess-1", "appr-pending"); err != nil {
		t.Fatalf("replaySessionEventLog: %v", err)
	}
	if len(sink.msgs) != 0 {
		t.Fatalf("replayed %d messages, want pending approval skipped", len(sink.msgs))
	}
}

func TestServerMsgFromSessionEvent_roundTrip(t *testing.T) {
	orig := toolCallServerMsg(executor.ToolCallEvent{
		CallID: "call-rt", Tool: "weather.get-forecast", Version: "1.0.0", Args: json.RawMessage(`{"city":"NYC"}`),
	}, nil)
	payload := marshalSessionEventProto(orig)
	got, err := serverMsgFromSessionEvent(payload)
	if err != nil {
		t.Fatalf("serverMsgFromSessionEvent: %v", err)
	}
	if got.GetToolCall().GetCallId() != "call-rt" {
		t.Fatalf("call_id = %q, want call-rt", got.GetToolCall().GetCallId())
	}
	if got.GetToolCall().GetTool() != "weather.get-forecast" {
		t.Fatalf("tool = %q", got.GetToolCall().GetTool())
	}
}

func TestConversationMessageFromSessionEvent_roundTrip(t *testing.T) {
	payload := assistantMessagePayload("segment one", "tool_use", provider.TokenUsage{InputTokens: 10, OutputTokens: 5}, 42)
	got, err := conversationMessageFromSessionEvent(payload)
	if err != nil {
		t.Fatalf("conversationMessageFromSessionEvent: %v", err)
	}
	if got.GetContent() != "segment one" {
		t.Fatalf("content = %q", got.GetContent())
	}
	if got.GetStopReason() != "tool_use" {
		t.Fatalf("stop_reason = %q", got.GetStopReason())
	}
}
