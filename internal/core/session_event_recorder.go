package core

import (
	"context"
	"encoding/json"
	"log/slog"

	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
	"github.com/phrony-platform/runtime/internal/executor"
	"github.com/phrony-platform/runtime/internal/model"
	"github.com/phrony-platform/runtime/internal/policy"
	"github.com/phrony-platform/runtime/internal/provider"
	"github.com/phrony-platform/runtime/internal/store"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// EventRecorder appends ordered audit events to the events log. Every write is
// best-effort: a persistence failure is logged but never breaks the live turn
// (mirrors ToolInvocationRecorder).
type EventRecorder struct {
	Q *store.Queries
}

func newEventRecorder(q *store.Queries) *EventRecorder {
	if q == nil {
		return nil
	}
	return &EventRecorder{Q: q}
}

func newSessionEventRecorder(q *store.Queries) *EventRecorder {
	return newEventRecorder(q)
}

// Record appends a single audit event in insertion order.
func (r *EventRecorder) Record(ctx context.Context, sessionID string, t model.SessionEventType, payload json.RawMessage) {
	if r == nil || r.Q == nil || sessionID == "" {
		return
	}
	evType := legacySessionEventType(t)
	ev := EventInput{
		SessionID: sessionID,
		Type:      evType,
		Actor:     actorForLegacySessionEvent(t),
		Payload:   payload,
	}
	switch t {
	case model.SessionEventSessionCompleted:
		ev.Session = &EventSessionProjection{Status: model.SessionStatusCompleted}
	case model.SessionEventSessionFailed:
		msg := sessionFailedMessageFromPayload(payload)
		var errText *string
		if msg != "" {
			errText = &msg
		}
		ev.Session = &EventSessionProjection{
			Status: model.SessionStatusFailed,
			Error:  errText,
		}
	case model.SessionEventSessionCancelled:
		ev.Session = &EventSessionProjection{Status: model.SessionStatusCancelled}
	case model.SessionEventToolCall, model.SessionEventToolResult, model.SessionEventPolicyDenied, model.SessionEventApprovalRequired:
		ev.SkipProjection = true
	}
	if _, _, err := appendEventAuto(ctx, r.Q, ev); err != nil {
		slog.Error("record event", "session_id", sessionID, "type", evType, "error", err)
	}
}

// RecordServerMsg persists a wire-backed event by marshalling the exact gRPC
// server message, so attach replay can re-send it verbatim.
func (r *EventRecorder) RecordServerMsg(ctx context.Context, sessionID string, t model.SessionEventType, msg *runtimev1.RunSessionInteractiveServerMsg) {
	if r == nil || r.Q == nil || msg == nil {
		return
	}
	r.Record(ctx, sessionID, t, marshalSessionEventProto(msg))
}

// marshalSessionEventProto is the single chokepoint that turns a proto message
// into an event payload, keeping the on-disk shape aligned with the wire.
func marshalSessionEventProto(m proto.Message) json.RawMessage {
	b, err := protojson.Marshal(m)
	if err != nil {
		return json.RawMessage("{}")
	}
	return json.RawMessage(b)
}

// serverMsgFromSessionEvent reconstructs a gRPC server message from a wire-backed
// event payload for attach replay.
func serverMsgFromSessionEvent(payload json.RawMessage) (*runtimev1.RunSessionInteractiveServerMsg, error) {
	var msg runtimev1.RunSessionInteractiveServerMsg
	if err := protojson.Unmarshal(payload, &msg); err != nil {
		return nil, err
	}
	return &msg, nil
}

// conversationMessageFromSessionEvent decodes a user_message / assistant_message
// payload back into the conversation message shape.
func conversationMessageFromSessionEvent(payload json.RawMessage) (*runtimev1.InteractiveConversationMessage, error) {
	var msg runtimev1.InteractiveConversationMessage
	if err := protojson.Unmarshal(payload, &msg); err != nil {
		return nil, err
	}
	return &msg, nil
}

func userMessagePayload(text string) json.RawMessage {
	return marshalSessionEventProto(&runtimev1.InteractiveConversationMessage{
		Role:    provider.RoleUser,
		Content: text,
	})
}

func assistantMessagePayload(content, stopReason string, usage provider.TokenUsage, durationMs int64) json.RawMessage {
	return marshalSessionEventProto(&runtimev1.InteractiveConversationMessage{
		Role:           provider.RoleAssistant,
		Content:        content,
		StopReason:     stopReason,
		TurnUsage:      tokenUsageToProto(usage),
		TurnDurationMs: durationMs,
	})
}

// approvalDecidedPayload is the audit record for a resolved approval. It is not
// re-emitted on attach (the decision is reflected by the following tool result
// or session failure) but preserves who decided and why.
type approvalDecidedPayload struct {
	ApprovalID string `json:"approval_id"`
	CallID     string `json:"call_id,omitempty"`
	Approved   bool   `json:"approved"`
	DecidedBy  string `json:"decided_by,omitempty"`
	Comment    string `json:"comment,omitempty"`
	OnReject   string `json:"on_reject,omitempty"`
}

// replaySessionEventLog re-emits the persisted timeline for an attaching client.
// Conversation text is replayed via session_started.history (folded from the log).
// Wire-backed tool/approval steps are sent in insertion order; JSON-backed tool
// events are synthesized into wire messages when needed.
func replaySessionEventLog(ctx context.Context, q *store.Queries, events sessionEventSink, sessionID, skipApprovalID string) error {
	if q == nil || events == nil || sessionID == "" {
		return nil
	}
	log, err := q.ListEventsBySession(ctx, sessionID)
	if err != nil {
		slog.Error("replay session events", "session_id", sessionID, "error", err)
		return nil
	}
	for _, ev := range log {
		msg, ok := replayWireMsgFromEvent(ev)
		if !ok || msg == nil {
			continue
		}
		if ar := msg.GetApprovalRequired(); ar != nil && skipApprovalID != "" && ar.GetApprovalId() == skipApprovalID {
			continue
		}
		if err := events.Send(msg); err != nil {
			return err
		}
	}
	return nil
}

func replayWireMsgFromEvent(ev store.Event) (*runtimev1.RunSessionInteractiveServerMsg, bool) {
	switch ev.Type {
	case EventToolRequested, EventToolCompleted, EventToolPolicyDenied, EventApprovalRequired:
	default:
		return nil, false
	}
	if msg, err := serverMsgFromSessionEvent(ev.Payload); err == nil {
		switch {
		case msg.GetToolCall() != nil, msg.GetToolResult() != nil, msg.GetApprovalRequired() != nil:
			return msg, true
		}
	}
	return synthesizeReplayMsgFromEvent(ev)
}

func synthesizeReplayMsgFromEvent(ev store.Event) (*runtimev1.RunSessionInteractiveServerMsg, bool) {
	callID := ""
	if ev.CallID != nil {
		callID = *ev.CallID
	}
	switch ev.Type {
	case EventToolRequested:
		var body struct {
			Tool    string          `json:"tool"`
			Version string          `json:"version"`
			Args    json.RawMessage `json:"args"`
		}
		if err := json.Unmarshal(ev.Payload, &body); err != nil || body.Tool == "" {
			return nil, false
		}
		if len(body.Args) == 0 {
			body.Args = json.RawMessage("{}")
		}
		return toolCallServerMsg(executor.ToolCallEvent{
			CallID:  callID,
			Tool:    body.Tool,
			Version: body.Version,
			Args:    body.Args,
		}, nil), true
	case EventToolCompleted, EventToolPolicyDenied:
		var body struct {
			Result       json.RawMessage `json:"result"`
			ErrorMessage string          `json:"error_message"`
			Error        string          `json:"error"`
		}
		if err := json.Unmarshal(ev.Payload, &body); err != nil {
			return nil, false
		}
		errMsg := body.ErrorMessage
		if errMsg == "" {
			errMsg = body.Error
		}
		if msg, err := serverMsgFromSessionEvent(ev.Payload); err == nil && msg.GetToolResult() != nil {
			return msg, true
		}
		return toolResultServerMsg(executor.ToolResultEvent{
			CallID:       callID,
			Payload:      body.Result,
			ErrorMessage: errMsg,
			Denied:       ev.Type == EventToolPolicyDenied,
		}), true
	case EventApprovalRequired:
		var open store.InsertApprovalParams
		if err := json.Unmarshal(ev.Payload, &open); err != nil {
			return nil, false
		}
		args := open.Args
		if len(args) == 0 {
			args = json.RawMessage("{}")
		}
		return approvalRequiredServerMsg(policy.ApprovalRequest{
			ApprovalID: open.ID,
			SessionID:  open.SessionID,
			CallID:     open.CallID,
			Reason:     open.Reason,
			Tool:       open.Tool,
			Version:    open.Version,
			Args:       args,
		}), true
	default:
		return nil, false
	}
}

// pendingApprovalIDForReplay returns the id of the session's still-pending
// approval, or "" when none is pending. Used so replaySessionEventLog does not
// duplicate the prompt the attach handlers re-send for the live pending approval.
func pendingApprovalIDForReplay(ctx context.Context, q *store.Queries, sessionID string) string {
	if q == nil {
		return ""
	}
	pending, err := q.GetPendingApprovalBySession(ctx, sessionID)
	if err != nil {
		return ""
	}
	return pending.ID
}

// recordApprovalDecided appends the audit entry for a resolved approval.
func recordApprovalDecided(ctx context.Context, q *store.Queries, row store.Approval, approved bool, decidedBy, comment string, received int) {
	ev := approvalDecidedEventInput(row, approved, decidedBy, comment, received)
	if _, _, err := appendEventAuto(ctx, q, ev); err != nil {
		slog.Error("record approval decided", "session_id", row.SessionID, "approval_id", row.ID, "error", err)
	}
}

func marshalSessionEventJSON(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage("{}")
	}
	return json.RawMessage(b)
}
