package core

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/phrony-platform/runtime/internal/model"
	"github.com/phrony-platform/runtime/internal/store"
	"github.com/phrony-platform/runtime/internal/telemetry"
	"github.com/phrony-platform/runtime/internal/tooldispatch"
)

// Event taxonomy — dotted names stored in events.type.
const (
	EventSessionStarted    = "session.started"
	EventSessionCompleted  = "session.completed"
	EventSessionFailed     = "session.failed"
	EventSessionCancelled  = "session.cancelled"
	EventMessageUser       = "message.user"
	EventMessageAssistant  = "message.assistant"
	EventToolRequested     = "tool.requested"
	EventToolQueued        = "tool.queued"
	EventToolDispatched    = "tool.dispatched"
	EventToolCompleted     = "tool.completed"
	EventToolIndeterminate = "tool.indeterminate"
	EventToolPolicyDenied  = "tool.policy_denied"
	EventApprovalRequired  = "approval.required"
	EventApprovalVote      = "approval.vote"
	EventApprovalDecided   = "approval.decided"
	EventApprovalEscalated = "approval.escalated"
	EventEvidenceRecorded  = "evidence.recorded"
)

const (
	ActorUser   = "user"
	ActorAgent  = "agent"
	ActorWorker = "worker"
	ActorPolicy = "policy"
	ActorSystem = "system"
)

// EventInput describes one append-only log entry and optional projection side effects.
type EventInput struct {
	SessionID      string
	RootSessionID  string
	Type           string
	Turn           *int
	CallID         *string
	ChildSessionID *string
	Actor          string
	Payload        json.RawMessage
	// SkipProjection inserts the event only; used when another writer already
	// applied the projection (e.g. wire replay duplicates).
	SkipProjection bool

	Tool     *EventToolProjection
	Approval *EventApprovalProjection
	Session  *EventSessionProjection
}

type EventToolProjection struct {
	Call                tooldispatch.ToolCall
	Status              string
	Provenance          *tooldispatch.DispatchProvenance
	Result              tooldispatch.ToolResult
	DispatchErr         error
	IndeterminateReason string
}

type EventApprovalProjection struct {
	Open           *store.InsertApprovalParams
	OpenInvocation *store.InsertToolInvocationPendingParams
	Vote           *store.InsertApprovalVoteParams
	Decide         *store.DecideApprovalParams
	DecideApproved bool
	DecideCallID   string
	DecideOnReject string
	EscalateID     string
	EscalateBy     string
}

type EventSessionProjection struct {
	Status         string
	Error          *string
	UseCancelSQL   bool
	UseCompleteSQL bool
}

// appendEvent increments the per-session sequence, inserts the event row, and
// applies any projection mutation for ev.Type. txQ must be transaction-bound.
func appendEvent(ctx context.Context, txQ *store.Queries, ev EventInput) (id int64, seq int, err error) {
	if txQ == nil {
		return 0, 0, errors.New("queries is nil")
	}
	if ev.SessionID == "" || ev.Type == "" {
		return 0, 0, errors.New("session_id and type are required")
	}
	rootSessionID := ev.RootSessionID
	if rootSessionID == "" {
		session, err := txQ.GetSession(ctx, ev.SessionID)
		if err != nil {
			return 0, 0, err
		}
		rootSessionID = session.RootSessionID
		if rootSessionID == "" {
			rootSessionID = ev.SessionID
		}
	}
	seq, err = txQ.NextSessionSeq(ctx, ev.SessionID)
	if err != nil {
		return 0, 0, err
	}
	id, err = txQ.InsertEvent(ctx, store.InsertEventParams{
		SessionID:      ev.SessionID,
		RootSessionID:  rootSessionID,
		Seq:            seq,
		Type:           ev.Type,
		Turn:           ev.Turn,
		CallID:         ev.CallID,
		ChildSessionID: ev.ChildSessionID,
		Actor:          ev.Actor,
		Payload:        ev.Payload,
	})
	if err != nil {
		return 0, 0, err
	}
	if !ev.SkipProjection {
		if err := applyEventProjection(ctx, txQ, ev); err != nil {
			return 0, 0, err
		}
	}
	return id, seq, nil
}

// appendEventAuto wraps appendEvent in a transaction when q is not already transactional.
func appendEventAuto(ctx context.Context, q *store.Queries, ev EventInput) (int64, int, error) {
	var id int64
	var seq int
	err := q.InTx(ctx, func(ctx context.Context, txQ *store.Queries) error {
		var err error
		id, seq, err = appendEvent(ctx, txQ, ev)
		return err
	})
	return id, seq, err
}

func applyEventProjection(ctx context.Context, q *store.Queries, ev EventInput) error {
	switch ev.Type {
	case EventSessionStarted:
		return nil
	case EventSessionCompleted:
		return applySessionCompletedProjection(ctx, q, ev)
	case EventSessionFailed:
		return applySessionFailedProjection(ctx, q, ev)
	case EventSessionCancelled:
		return applySessionCancelledProjection(ctx, q, ev)
	case EventToolRequested:
		return applyToolRequestedProjection(ctx, q, ev.Tool)
	case EventToolQueued:
		return applyToolQueuedProjection(ctx, q, ev.Tool)
	case EventToolDispatched:
		return applyToolDispatchedProjection(ctx, q, ev.Tool)
	case EventToolCompleted, EventToolPolicyDenied:
		return applyToolCompletedProjection(ctx, q, ev.Tool)
	case EventToolIndeterminate:
		return applyToolIndeterminateProjection(ctx, q, ev.Tool)
	case EventApprovalRequired:
		return applyApprovalRequiredProjection(ctx, q, ev.Approval)
	case EventApprovalVote:
		return applyApprovalVoteProjection(ctx, q, ev.Approval)
	case EventApprovalDecided:
		return applyApprovalDecidedProjection(ctx, q, ev.Approval)
	case EventApprovalEscalated:
		return applyApprovalEscalatedProjection(ctx, q, ev.Approval)
	default:
		return nil
	}
}

func applySessionCompletedProjection(ctx context.Context, q *store.Queries, ev EventInput) error {
	if ev.Session != nil && ev.Session.UseCompleteSQL {
		_, err := q.CompleteSession(ctx, ev.SessionID)
		return err
	}
	status := model.SessionStatusCompleted
	if ev.Session != nil && ev.Session.Status != "" {
		status = ev.Session.Status
	}
	_, err := q.UpdateSession(ctx, store.UpdateSessionParams{
		ID:     ev.SessionID,
		Status: status,
	})
	return err
}

func applySessionFailedProjection(ctx context.Context, q *store.Queries, ev EventInput) error {
	status := model.SessionStatusFailed
	var errText *string
	if ev.Session != nil {
		if ev.Session.Status != "" {
			status = ev.Session.Status
		}
		errText = ev.Session.Error
	}
	if errText == nil {
		if msg := sessionFailedMessageFromPayload(ev.Payload); msg != "" {
			errText = &msg
		}
	}
	_, err := q.UpdateSession(ctx, store.UpdateSessionParams{
		ID:     ev.SessionID,
		Status: status,
		Error:  errText,
	})
	return err
}

func applySessionCancelledProjection(ctx context.Context, q *store.Queries, ev EventInput) error {
	if ev.Session != nil && ev.Session.UseCancelSQL {
		_, err := q.CancelSession(ctx, ev.SessionID)
		return err
	}
	_, err := q.UpdateSession(ctx, store.UpdateSessionParams{
		ID:     ev.SessionID,
		Status: model.SessionStatusCancelled,
	})
	return err
}

func applyToolRequestedProjection(ctx context.Context, q *store.Queries, tool *EventToolProjection) error {
	if tool == nil || tool.Call.CallID == "" {
		return nil
	}
	status := tool.Status
	if status == "" {
		status = model.ToolInvocationPending
	}
	_, err := q.InsertToolInvocationPending(ctx, store.InsertToolInvocationPendingParams{
		CallID:         tool.Call.CallID,
		SessionID:      tool.Call.SessionID,
		AgentVersionID: tool.Call.AgentVersionID,
		Turn:           tool.Call.Turn,
		Tool:           tool.Call.Tool,
		Version:        tool.Call.Version,
		Args:           tool.Call.Args,
		Status:         status,
	})
	return err
}

func applyToolQueuedProjection(ctx context.Context, q *store.Queries, tool *EventToolProjection) error {
	if tool == nil || tool.Call.CallID == "" {
		return nil
	}
	return q.UpdateToolInvocationStatus(ctx, tool.Call.CallID, model.ToolInvocationQueued)
}

func applyToolDispatchedProjection(ctx context.Context, q *store.Queries, tool *EventToolProjection) error {
	if tool == nil || tool.Provenance == nil {
		return nil
	}
	prov := tool.Provenance
	manifestHash := prov.ManifestContentHash
	if manifestHash == "" && prov.Call.AgentVersionID != "" {
		hash, err := q.GetAgentVersionContentHash(ctx, prov.Call.AgentVersionID)
		if err != nil && !errors.Is(err, context.Canceled) {
			return err
		}
		manifestHash = hash
	}
	_, err := q.InsertToolInvocationDispatched(ctx, store.InsertToolInvocationDispatchedParams{
		CallID:              prov.Call.CallID,
		SessionID:           prov.Call.SessionID,
		AgentVersionID:      prov.Call.AgentVersionID,
		Turn:                prov.Call.Turn,
		Tool:                prov.Call.Tool,
		Version:             prov.Call.Version,
		Args:                prov.Call.Args,
		Status:              model.ToolInvocationDispatched,
		WorkerIdentity:      prov.Worker.WorkloadIdentity,
		ImageDigest:         prov.Worker.ImageDigest,
		DescriptorHash:      prov.DescriptorHash,
		ManifestContentHash: manifestHash,
	})
	return err
}

func applyToolCompletedProjection(ctx context.Context, q *store.Queries, tool *EventToolProjection) error {
	if tool == nil || tool.Call.CallID == "" {
		return nil
	}
	call := tool.Call
	res := tool.Result
	dispatchErr := tool.DispatchErr

	status := model.ToolInvocationSucceeded
	var result json.RawMessage
	var errCode, errMsg *string

	switch {
	case dispatchErr != nil:
		status = model.ToolInvocationFailed
		code := "dispatch_error"
		msg := dispatchErr.Error()
		errCode = &code
		errMsg = &msg
		if tooldispatch.IsIntegrityError(dispatchErr) {
			if ie, ok := dispatchErr.(*tooldispatch.IntegrityError); ok {
				errCode = strPtr(string(ie.Violation))
			}
		}
	case res.Err != nil:
		status = model.ToolInvocationFailed
		errCode = strPtr(res.Err.Code)
		errMsg = strPtr(res.Err.Message)
	default:
		if len(res.Payload) > 0 {
			result = res.Payload
		} else {
			result = json.RawMessage("{}")
		}
	}

	usageInput, usageOutput, usageEstimated := usageFieldsFromToolResult(res)
	_, err := q.CompleteToolInvocation(ctx, store.CompleteToolInvocationParams{
		CallID:            call.CallID,
		Status:            status,
		Result:            result,
		ErrorCode:         errCode,
		ErrorMessage:      errMsg,
		UsageInputTokens:  usageInput,
		UsageOutputTokens: usageOutput,
		UsageEstimated:    usageEstimated,
	})
	return err
}

func applyToolIndeterminateProjection(ctx context.Context, q *store.Queries, tool *EventToolProjection) error {
	if tool == nil || tool.Call.CallID == "" {
		return nil
	}
	reason := tool.IndeterminateReason
	if reason == "" {
		reason = tooldispatch.ErrIndeterminate.Error()
	}
	return q.MarkToolInvocationIndeterminate(ctx, tool.Call.CallID, reason)
}

func applyApprovalRequiredProjection(ctx context.Context, q *store.Queries, approval *EventApprovalProjection) error {
	if approval == nil {
		return nil
	}
	if approval.OpenInvocation != nil {
		if _, err := q.InsertToolInvocationPending(ctx, *approval.OpenInvocation); err != nil {
			return err
		}
	}
	if approval.Open != nil {
		_, err := q.InsertApproval(ctx, *approval.Open)
		return err
	}
	return nil
}

func applyApprovalVoteProjection(ctx context.Context, q *store.Queries, approval *EventApprovalProjection) error {
	if approval == nil || approval.Vote == nil {
		return nil
	}
	_, err := q.InsertApprovalVote(ctx, *approval.Vote)
	return err
}

func applyApprovalDecidedProjection(ctx context.Context, q *store.Queries, approval *EventApprovalProjection) error {
	if approval == nil || approval.Decide == nil {
		return nil
	}
	if _, err := q.DecideApproval(ctx, *approval.Decide); err != nil {
		return err
	}
	if approval.DecideCallID != "" {
		invStatus := model.ToolInvocationFailed
		if approval.DecideApproved {
			invStatus = model.ToolInvocationPending
		}
		if !approval.DecideApproved && approval.DecideOnReject == "fail" {
			invStatus = model.ToolInvocationFailed
		}
		_ = q.UpdateToolInvocationStatus(ctx, approval.DecideCallID, invStatus)
	}
	return nil
}

func applyApprovalEscalatedProjection(ctx context.Context, q *store.Queries, approval *EventApprovalProjection) error {
	if approval == nil || approval.EscalateID == "" {
		return nil
	}
	by := approval.EscalateBy
	if by == "" {
		by = "system"
	}
	_, err := q.MarkApprovalEscalated(ctx, approval.EscalateID, by)
	return err
}

func sessionFailedMessageFromPayload(payload json.RawMessage) string {
	var body struct {
		Message string `json:"message"`
	}
	if len(payload) == 0 {
		return ""
	}
	if err := json.Unmarshal(payload, &body); err != nil {
		return ""
	}
	return body.Message
}

func legacySessionEventType(t model.SessionEventType) string {
	switch t {
	case model.SessionEventUserMessage:
		return EventMessageUser
	case model.SessionEventAssistantMessage:
		return EventMessageAssistant
	case model.SessionEventToolCall:
		return EventToolRequested
	case model.SessionEventToolResult:
		return EventToolCompleted
	case model.SessionEventPolicyDenied:
		return EventToolPolicyDenied
	case model.SessionEventApprovalRequired:
		return EventApprovalRequired
	case model.SessionEventApprovalDecided:
		return EventApprovalDecided
	case model.SessionEventSessionCompleted:
		return EventSessionCompleted
	case model.SessionEventSessionFailed:
		return EventSessionFailed
	case model.SessionEventSessionCancelled:
		return EventSessionCancelled
	default:
		return string(t)
	}
}

func actorForLegacySessionEvent(t model.SessionEventType) string {
	switch t {
	case model.SessionEventUserMessage:
		return ActorUser
	case model.SessionEventAssistantMessage, model.SessionEventToolCall:
		return ActorAgent
	case model.SessionEventToolResult:
		return ActorWorker
	case model.SessionEventPolicyDenied, model.SessionEventApprovalRequired:
		return ActorPolicy
	default:
		return ActorSystem
	}
}

func intPtrTurn(turn int) *int {
	if turn <= 0 {
		return nil
	}
	return &turn
}

func strPtrIf(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func toolEventInput(sessionID string, typ string, call tooldispatch.ToolCall, payload json.RawMessage) EventInput {
	ev := EventInput{
		SessionID: sessionID,
		Type:      typ,
		Actor:     ActorAgent,
		Payload:   payload,
		Tool:      &EventToolProjection{Call: call},
	}
	if call.Turn > 0 {
		ev.Turn = intPtrTurn(call.Turn)
	}
	if call.CallID != "" {
		ev.CallID = &call.CallID
	}
	return ev
}

func sessionLifecycleEvent(sessionID, typ string, payload json.RawMessage, proj *EventSessionProjection) EventInput {
	return EventInput{
		SessionID: sessionID,
		Type:      typ,
		Actor:     ActorSystem,
		Payload:   payload,
		Session:   proj,
	}
}

func sessionFailedEvent(sessionID, message string) EventInput {
	errText := message
	return sessionLifecycleEvent(sessionID, EventSessionFailed, marshalSessionEventJSON(map[string]string{
		"message": message,
	}), &EventSessionProjection{
		Status: model.SessionStatusFailed,
		Error:  &errText,
	})
}

func sessionCompletedEvent(sessionID string, payload json.RawMessage, useCompleteSQL bool) EventInput {
	return sessionLifecycleEvent(sessionID, EventSessionCompleted, payload, &EventSessionProjection{
		Status:         model.SessionStatusCompleted,
		UseCompleteSQL: useCompleteSQL,
	})
}

func sessionCancelledEvent(sessionID string, useCancelSQL bool) EventInput {
	return sessionLifecycleEvent(sessionID, EventSessionCancelled, json.RawMessage("{}"), &EventSessionProjection{
		Status:       model.SessionStatusCancelled,
		UseCancelSQL: useCancelSQL,
	})
}

func approvalDecidedEventInput(row store.Approval, approved bool, decidedBy, comment string, received int) EventInput {
	status := model.ApprovalStatusDenied
	if approved {
		status = model.ApprovalStatusApproved
	}
	payload := marshalSessionEventJSON(approvalDecidedPayload{
		ApprovalID: row.ID,
		CallID:     row.CallID,
		Approved:   approved,
		DecidedBy:  decidedBy,
		Comment:    comment,
		OnReject:   row.OnReject,
	})
	var callID *string
	if row.CallID != "" {
		callID = &row.CallID
	}
	return EventInput{
		SessionID: row.SessionID,
		Type:      EventApprovalDecided,
		CallID:    callID,
		Actor:     ActorSystem,
		Payload:   payload,
		Approval: &EventApprovalProjection{
			Decide: &store.DecideApprovalParams{
				ID:                row.ID,
				Status:            status,
				DecidedBy:         decidedBy,
				Comment:           comment,
				ApprovalsReceived: received,
			},
			DecideApproved: approved,
			DecideCallID:   row.CallID,
			DecideOnReject: row.OnReject,
		},
	}
}

func toolRequestedPayload(call tooldispatch.ToolCall, status string) json.RawMessage {
	body := map[string]any{
		"tool":    call.Tool,
		"version": call.Version,
		"args":    json.RawMessage(call.Args),
		"turn":    call.Turn,
	}
	if wireName := strings.TrimSpace(call.WireName); wireName != "" {
		body["wire_name"] = wireName
	}
	if status != "" {
		body["status"] = status
	}
	b, err := json.Marshal(body)
	if err != nil {
		return json.RawMessage("{}")
	}
	return json.RawMessage(b)
}

func toolCompletedPayload(call tooldispatch.ToolCall, res tooldispatch.ToolResult, dispatchErr error) json.RawMessage {
	body := map[string]any{"call_id": call.CallID}
	if dispatchErr != nil {
		body["error"] = dispatchErr.Error()
	} else if res.Err != nil {
		body["error_code"] = res.Err.Code
		body["error_message"] = res.Err.Message
	} else if len(res.Payload) > 0 {
		body["result"] = json.RawMessage(res.Payload)
	}
	b, err := json.Marshal(body)
	if err != nil {
		return json.RawMessage("{}")
	}
	return json.RawMessage(b)
}

func appendSessionStarted(ctx context.Context, txQ *store.Queries, sessionID, rootSessionID string, input json.RawMessage) error {
	_, _, err := appendEvent(ctx, txQ, EventInput{
		SessionID:     sessionID,
		RootSessionID: rootSessionID,
		Type:          EventSessionStarted,
		Actor:         ActorSystem,
		Payload:       input,
	})
	if err == nil {
		telemetry.Track(telemetry.EventSessionStarted)
	}
	return err
}

// appendSessionFailed marks a session failed via the event log.
func appendSessionFailed(ctx context.Context, q *store.Queries, sessionID, message string) error {
	_, _, err := appendEventAuto(ctx, q, sessionFailedEvent(sessionID, message))
	if err == nil {
		telemetry.Track(telemetry.EventSessionFailed)
	}
	return err
}

// appendSessionCompleted marks a session completed via the event log.
func appendSessionCompleted(ctx context.Context, q *store.Queries, sessionID string, payload json.RawMessage, useCompleteSQL bool) error {
	_, _, err := appendEventAuto(ctx, q, sessionCompletedEvent(sessionID, payload, useCompleteSQL))
	if err == nil {
		telemetry.Track(telemetry.EventSessionCompleted)
	}
	return err
}

// appendSessionCancelled marks a session cancelled via the event log.
func appendSessionCancelled(ctx context.Context, q *store.Queries, sessionID string, useCancelSQL bool) error {
	_, _, err := appendEventAuto(ctx, q, sessionCancelledEvent(sessionID, useCancelSQL))
	return err
}

func isNoRows(err error) bool {
	return errors.Is(err, sql.ErrNoRows)
}

func fmtAppendEvent(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("append event: %w", err)
}

// RebuildProjections clears derived rows for a session and replays the event log
// through applyEventProjection. Used for disaster recovery and equivalence tests.
func RebuildProjections(ctx context.Context, q *store.Queries, sessionID string) error {
	if q == nil || sessionID == "" {
		return errors.New("queries and session_id are required")
	}
	return q.InTx(ctx, func(ctx context.Context, txQ *store.Queries) error {
		session, err := txQ.GetSession(ctx, sessionID)
		if err != nil {
			return err
		}
		if err := txQ.DeleteSessionProjections(ctx, sessionID); err != nil {
			return err
		}
		events, err := txQ.ListEventsBySession(ctx, sessionID)
		if err != nil {
			return err
		}
		for _, ev := range events {
			input := eventProjectionInputFromLog(session, ev)
			if err := applyEventProjection(ctx, txQ, input); err != nil {
				return fmt.Errorf("replay event seq %d type %s: %w", ev.Seq, ev.Type, err)
			}
		}
		return nil
	})
}

func eventProjectionInputFromLog(session store.Session, ev store.Event) EventInput {
	input := EventInput{
		SessionID:      ev.SessionID,
		RootSessionID:  ev.RootSessionID,
		Type:           ev.Type,
		Turn:           ev.Turn,
		CallID:         ev.CallID,
		ChildSessionID: ev.ChildSessionID,
		Actor:          ev.Actor,
		Payload:        ev.Payload,
	}
	switch ev.Type {
	case EventSessionCompleted:
		input.Session = &EventSessionProjection{Status: model.SessionStatusCompleted}
	case EventSessionFailed:
		msg := sessionFailedMessageFromPayload(ev.Payload)
		var errText *string
		if msg != "" {
			errText = &msg
		}
		input.Session = &EventSessionProjection{
			Status: model.SessionStatusFailed,
			Error:  errText,
		}
	case EventSessionCancelled:
		input.Session = &EventSessionProjection{Status: model.SessionStatusCancelled}
	case EventToolRequested, EventToolQueued, EventToolDispatched, EventToolCompleted, EventToolPolicyDenied, EventToolIndeterminate:
		call := toolCallFromStoredEvent(session, ev)
		tool := &EventToolProjection{Call: call}
		switch ev.Type {
		case EventToolRequested:
			tool.Status = toolStatusFromPayload(ev.Payload, model.ToolInvocationPending)
		case EventToolQueued:
			tool.Status = model.ToolInvocationQueued
		case EventToolDispatched:
			tool.Provenance = dispatchProvenanceFromStoredEvent(ev, call)
		case EventToolCompleted, EventToolPolicyDenied:
			tool.Result, tool.DispatchErr = toolResultFromStoredEvent(ev)
		case EventToolIndeterminate:
			var body struct {
				Reason string `json:"reason"`
			}
			_ = json.Unmarshal(ev.Payload, &body)
			tool.IndeterminateReason = body.Reason
		}
		input.Tool = tool
	case EventApprovalRequired:
		input.Approval = approvalProjectionFromStoredEvent(session, ev)
	case EventApprovalVote:
		var vote store.InsertApprovalVoteParams
		if err := json.Unmarshal(ev.Payload, &vote); err == nil {
			input.Approval = &EventApprovalProjection{Vote: &vote}
		}
	case EventApprovalDecided:
		input.Approval = approvalDecidedProjectionFromStoredEvent(ev)
	case EventApprovalEscalated:
		var body struct {
			ID string `json:"id"`
			By string `json:"by"`
		}
		if err := json.Unmarshal(ev.Payload, &body); err == nil {
			input.Approval = &EventApprovalProjection{
				EscalateID: body.ID,
				EscalateBy: body.By,
			}
		}
	}
	return input
}

func toolCallFromStoredEvent(session store.Session, ev store.Event) tooldispatch.ToolCall {
	call := tooldispatch.ToolCall{
		SessionID:      ev.SessionID,
		AgentVersionID: session.AgentVersionID,
	}
	if ev.CallID != nil {
		call.CallID = *ev.CallID
	}
	if ev.Turn != nil {
		call.Turn = *ev.Turn
	}
	if msg, err := serverMsgFromSessionEvent(ev.Payload); err == nil {
		if tc := msg.GetToolCall(); tc != nil {
			call.CallID = tc.GetCallId()
			call.Tool = tc.GetTool()
			call.Version = tc.GetVersion()
			call.Args = tc.GetArgs()
			return call
		}
		if tr := msg.GetToolResult(); tr != nil {
			call.CallID = tr.GetCallId()
			return call
		}
	}
	var body struct {
		Tool    string          `json:"tool"`
		Version string          `json:"version"`
		Args    json.RawMessage `json:"args"`
		Turn    int             `json:"turn"`
		CallID  string          `json:"call_id"`
	}
	if err := json.Unmarshal(ev.Payload, &body); err == nil {
		if body.Tool != "" {
			call.Tool = body.Tool
		}
		if body.Version != "" {
			call.Version = body.Version
		}
		if len(body.Args) > 0 {
			call.Args = body.Args
		}
		if body.Turn > 0 {
			call.Turn = body.Turn
		}
		if body.CallID != "" {
			call.CallID = body.CallID
		}
	}
	if len(call.Args) == 0 {
		call.Args = json.RawMessage("{}")
	}
	return call
}

func toolStatusFromPayload(payload json.RawMessage, fallback string) string {
	var body struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(payload, &body); err != nil || body.Status == "" {
		return fallback
	}
	return body.Status
}

func dispatchProvenanceFromStoredEvent(ev store.Event, call tooldispatch.ToolCall) *tooldispatch.DispatchProvenance {
	var body struct {
		WorkerIdentity      string `json:"worker_identity"`
		ImageDigest         string `json:"image_digest"`
		DescriptorHash      string `json:"descriptor_hash"`
		ManifestContentHash string `json:"manifest_content_hash"`
	}
	if err := json.Unmarshal(ev.Payload, &body); err != nil {
		return &tooldispatch.DispatchProvenance{Call: call}
	}
	return &tooldispatch.DispatchProvenance{
		Call: call,
		Worker: tooldispatch.WorkerInfo{
			WorkloadIdentity: body.WorkerIdentity,
			ImageDigest:      body.ImageDigest,
		},
		DescriptorHash:      body.DescriptorHash,
		ManifestContentHash: body.ManifestContentHash,
	}
}

func toolResultFromStoredEvent(ev store.Event) (tooldispatch.ToolResult, error) {
	callID := ""
	if ev.CallID != nil {
		callID = *ev.CallID
	}
	if msg, err := serverMsgFromSessionEvent(ev.Payload); err == nil {
		if tr := msg.GetToolResult(); tr != nil {
			res := tooldispatch.ToolResult{CallID: tr.GetCallId(), Payload: tr.GetPayload()}
			if tr.GetErrorMessage() != "" {
				res.Err = &tooldispatch.ToolError{Message: tr.GetErrorMessage()}
			}
			return res, nil
		}
	}
	var body struct {
		Result       json.RawMessage `json:"result"`
		ErrorCode    string          `json:"error_code"`
		ErrorMessage string          `json:"error_message"`
		Error        string          `json:"error"`
	}
	if err := json.Unmarshal(ev.Payload, &body); err != nil {
		return tooldispatch.ToolResult{CallID: callID}, err
	}
	res := tooldispatch.ToolResult{CallID: callID}
	if body.ErrorMessage != "" {
		res.Err = &tooldispatch.ToolError{Message: body.ErrorMessage}
		return res, nil
	}
	if body.Error != "" {
		return res, errors.New(body.Error)
	}
	if body.ErrorCode != "" {
		res.Err = &tooldispatch.ToolError{Code: body.ErrorCode, Message: body.ErrorMessage}
		return res, nil
	}
	if len(body.Result) > 0 {
		res.Payload = body.Result
	}
	return res, nil
}

func approvalProjectionFromStoredEvent(session store.Session, ev store.Event) *EventApprovalProjection {
	if msg, err := serverMsgFromSessionEvent(ev.Payload); err == nil {
		if ar := msg.GetApprovalRequired(); ar != nil {
			open := store.InsertApprovalParams{
				ID:        ar.GetApprovalId(),
				SessionID: ev.SessionID,
				CallID:    ar.GetCallId(),
				Status:    model.ApprovalStatusPending,
				Reason:    ar.GetReason(),
				Tool:      ar.GetTool(),
				Version:   ar.GetVersion(),
				Args:      ar.GetArgs(),
			}
			return &EventApprovalProjection{
				Open: &open,
				OpenInvocation: &store.InsertToolInvocationPendingParams{
					CallID:         ar.GetCallId(),
					SessionID:      ev.SessionID,
					AgentVersionID: session.AgentVersionID,
					Tool:           ar.GetTool(),
					Version:        ar.GetVersion(),
					Args:           ar.GetArgs(),
					Status:         model.ToolInvocationAwaitingApproval,
				},
			}
		}
	}
	var open store.InsertApprovalParams
	if err := json.Unmarshal(ev.Payload, &open); err != nil {
		return nil
	}
	args := open.Args
	if len(args) == 0 {
		args = json.RawMessage("{}")
	}
	return &EventApprovalProjection{
		Open: &open,
		OpenInvocation: &store.InsertToolInvocationPendingParams{
			CallID:         open.CallID,
			SessionID:      ev.SessionID,
			AgentVersionID: session.AgentVersionID,
			Turn:           0,
			Tool:           open.Tool,
			Version:        open.Version,
			Args:           args,
			Status:         model.ToolInvocationAwaitingApproval,
		},
	}
}

func approvalDecidedProjectionFromStoredEvent(ev store.Event) *EventApprovalProjection {
	var body approvalDecidedPayload
	if err := json.Unmarshal(ev.Payload, &body); err != nil {
		return nil
	}
	status := model.ApprovalStatusDenied
	if body.Approved {
		status = model.ApprovalStatusApproved
	}
	return &EventApprovalProjection{
		Decide: &store.DecideApprovalParams{
			ID:                body.ApprovalID,
			Status:            status,
			DecidedBy:         body.DecidedBy,
			Comment:           body.Comment,
			ApprovalsReceived: 1,
		},
		DecideApproved: body.Approved,
		DecideCallID:   body.CallID,
		DecideOnReject: body.OnReject,
	}
}

// recordPolicyDeniedToolResult appends a policy-denied tool result to the event log.
func recordPolicyDeniedToolResult(ctx context.Context, q *store.Queries, sessionID, callID, message string) error {
	call := tooldispatch.ToolCall{CallID: callID, SessionID: sessionID}
	res := tooldispatch.ToolResult{
		CallID: callID,
		Err:    &tooldispatch.ToolError{Message: message},
	}
	ev := toolEventInput(sessionID, EventToolPolicyDenied, call, toolCompletedPayload(call, res, nil))
	ev.Tool.Result = res
	_, _, err := appendEventAuto(ctx, q, ev)
	return err
}
