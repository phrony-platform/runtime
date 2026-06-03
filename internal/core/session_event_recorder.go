package core

import (
	"context"
	"encoding/json"
	"log/slog"

	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
	"github.com/phrony-platform/runtime/internal/model"
	"github.com/phrony-platform/runtime/internal/provider"
	"github.com/phrony-platform/runtime/internal/store"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// SessionEventRecorder appends ordered audit events to session_events. Every
// write is best-effort: a persistence failure is logged but never breaks the
// live turn (mirrors ToolInvocationRecorder).
type SessionEventRecorder struct {
	Q *store.Queries
}

func newSessionEventRecorder(q *store.Queries) *SessionEventRecorder {
	if q == nil {
		return nil
	}
	return &SessionEventRecorder{Q: q}
}

// Record appends a single audit event in insertion order.
func (r *SessionEventRecorder) Record(ctx context.Context, sessionID string, t model.SessionEventType, payload json.RawMessage) {
	if r == nil || r.Q == nil || sessionID == "" {
		return
	}
	if _, err := r.Q.InsertSessionEvent(ctx, store.InsertSessionEventParams{
		SessionID: sessionID,
		Type:      string(t),
		Payload:   payload,
	}); err != nil {
		slog.Error("record session event", "session_id", sessionID, "type", string(t), "error", err)
	}
}

// RecordServerMsg persists a wire-backed event by marshalling the exact gRPC
// server message, so attach replay can re-send it verbatim.
func (r *SessionEventRecorder) RecordServerMsg(ctx context.Context, sessionID string, t model.SessionEventType, msg *runtimev1.RunSessionInteractiveServerMsg) {
	if r == nil || r.Q == nil || msg == nil {
		return
	}
	r.Record(ctx, sessionID, t, marshalSessionEventProto(msg))
}

// marshalSessionEventProto is the single chokepoint that turns a proto message
// into a session_events payload, keeping the on-disk shape aligned with the wire.
func marshalSessionEventProto(m proto.Message) json.RawMessage {
	b, err := protojson.Marshal(m)
	if err != nil {
		return json.RawMessage("{}")
	}
	return json.RawMessage(b)
}

// serverMsgFromSessionEvent reconstructs a gRPC server message from a wire-backed
// session_events payload for attach replay.
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
// It sends the wire-backed steps (tool_call / tool_result / policy_denied /
// approval_required) in insertion order so re-attach shows tool activity that
// happened before this client joined. Conversation text is replayed via
// session_started.history, and lifecycle / approval_decided entries are audit
// only, so they are skipped here. skipApprovalID suppresses the currently pending
// approval, which the caller surfaces separately, to avoid a double prompt.
func replaySessionEventLog(ctx context.Context, q *store.Queries, events sessionEventSink, sessionID, skipApprovalID string) error {
	if q == nil || events == nil || sessionID == "" {
		return nil
	}
	log, err := q.ListSessionEventsBySessionID(ctx, sessionID)
	if err != nil {
		slog.Error("replay session events", "session_id", sessionID, "error", err)
		return nil
	}
	for _, ev := range log {
		switch model.SessionEventType(ev.Type) {
		case model.SessionEventToolCall, model.SessionEventToolResult, model.SessionEventPolicyDenied:
			msg, err := serverMsgFromSessionEvent(ev.Payload)
			if err != nil {
				continue
			}
			if err := events.Send(msg); err != nil {
				return err
			}
		case model.SessionEventApprovalRequired:
			msg, err := serverMsgFromSessionEvent(ev.Payload)
			if err != nil {
				continue
			}
			if ar := msg.GetApprovalRequired(); ar != nil && skipApprovalID != "" && ar.GetApprovalId() == skipApprovalID {
				continue
			}
			if err := events.Send(msg); err != nil {
				return err
			}
		}
	}
	return nil
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
func recordApprovalDecided(ctx context.Context, q *store.Queries, row store.Approval, approved bool, decidedBy, comment string) {
	newSessionEventRecorder(q).Record(ctx, row.SessionID, model.SessionEventApprovalDecided, marshalSessionEventJSON(approvalDecidedPayload{
		ApprovalID: row.ID,
		CallID:     row.CallID,
		Approved:   approved,
		DecidedBy:  decidedBy,
		Comment:    comment,
		OnReject:   row.OnReject,
	}))
}

func marshalSessionEventJSON(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage("{}")
	}
	return json.RawMessage(b)
}
