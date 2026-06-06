package core

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
	"github.com/phrony-platform/runtime/internal/evidence"
	"github.com/phrony-platform/runtime/internal/manifest"
	"github.com/phrony-platform/runtime/internal/model"
	"github.com/phrony-platform/runtime/internal/store"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (s *runtimeServer) InspectSession(ctx context.Context, req *runtimev1.InspectSessionRequest) (*runtimev1.InspectSessionResponse, error) {
	sessionID := strings.TrimSpace(req.GetSessionId())
	if sessionID == "" {
		return nil, status.Error(codes.InvalidArgument, "session_id is required")
	}
	q, err := s.queries()
	if err != nil {
		return nil, err
	}

	if _, err := q.GetSession(ctx, sessionID); errors.Is(err, sql.ErrNoRows) {
		return nil, status.Errorf(codes.NotFound, "session %s not found", sessionID)
	} else if err != nil {
		return nil, status.Errorf(codes.Internal, "get session: %v", err)
	}

	descendantIDs, err := q.ListDescendantSessionIDs(ctx, sessionID)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "list descendant sessions: %v", err)
	}

	nodes := make(map[string]*runtimev1.SessionInspect, len(descendantIDs))
	for _, id := range descendantIDs {
		inspect, err := s.buildSessionInspect(ctx, q, id)
		if err != nil {
			return nil, err
		}
		nodes[id] = inspect
	}

	root := assembleInspectTree(sessionID, nodes)
	return &runtimev1.InspectSessionResponse{Session: root}, nil
}

func (s *runtimeServer) buildSessionInspect(ctx context.Context, q *store.Queries, sessionID string) (*runtimev1.SessionInspect, error) {
	session, err := q.GetSession(ctx, sessionID)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "get session %s: %v", sessionID, err)
	}
	meta, err := q.GetSessionDelegationMeta(ctx, sessionID)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "get session delegation meta: %v", err)
	}

	out := &runtimev1.SessionInspect{
		Id:             session.ID,
		AgentVersionId: session.AgentVersionID,
		Status:         session.Status,
		Depth:          int32(meta.Depth),
		CreatedAt:      formatTime(session.CreatedAt),
		UpdatedAt:      formatTime(session.UpdatedAt),
		Input:          session.Input,
		OutputRaw:      session.Output,
		SessionStartedAtUnixMs: session.CreatedAt.UnixMilli(),
	}
	if meta.ParentSessionID != nil {
		out.ParentSessionId = *meta.ParentSessionID
	}
	if meta.BundleVersionID != nil {
		out.BundleVersionId = *meta.BundleVersionID
	}
	if session.Error != nil {
		out.Error = *session.Error
	}
	if len(session.Output) > 0 {
		out.Output = sessionOutputToProto(session.Output)
	}
	if sessionEndedAtUnixMs := sessionEndedAtUnixMs(session.Status, session.UpdatedAt); sessionEndedAtUnixMs > 0 {
		out.SessionEndedAtUnixMs = sessionEndedAtUnixMs
	}

	agentCtx, err := resolveSessionAgentContext(ctx, q, session.AgentVersionID)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "resolve agent context: %v", err)
	}
	out.Agent = agentCtx

	if raw, err := q.GetSessionEvidence(ctx, sessionID); err == nil {
		if snap, parseErr := evidence.ParseSnapshot(raw); parseErr == nil {
			out.DescriptiveMetadata = evidenceSnapshotToProto(snap)
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return nil, status.Errorf(codes.Internal, "get session evidence: %v", err)
	}

	history, err := decodeHistory(session.History)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "decode history: %v", err)
	}
	history = enrichHistoryFromSessionOutput(history, session.Output)
	out.History = historyToProto(history)

	events, err := q.ListSessionEventsBySessionID(ctx, sessionID)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "list session events: %v", err)
	}
	out.Events = sessionEventsToProto(events)

	invocations, err := q.ListToolInvocationsBySessionID(ctx, sessionID)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "list tool invocations: %v", err)
	}
	invProto := toolInvocationsToProto(invocations)
	out.Invocations = invProto

	approvalRows, err := q.ListApprovals(ctx, store.ListApprovalsParams{SessionID: sessionID})
	if err != nil {
		return nil, status.Errorf(codes.Internal, "list approvals: %v", err)
	}
	approvals := make([]*runtimev1.Approval, 0, len(approvalRows))
	for _, row := range approvalRows {
		enriched, enrichErr := enrichApprovalFromInvocation(ctx, q, row)
		if enrichErr != nil {
			return nil, status.Errorf(codes.Internal, "load approval context: %v", enrichErr)
		}
		votes, votesErr := q.ListApprovalVotes(ctx, row.ID)
		if votesErr != nil {
			return nil, status.Errorf(codes.Internal, "list approval votes: %v", votesErr)
		}
		approvals = append(approvals, approvalToProto(enriched, votes))
	}
	out.Approvals = approvals

	out.Timeline = buildInspectTimeline(out.Events, invProto, approvals)
	return out, nil
}

func assembleInspectTree(rootID string, nodes map[string]*runtimev1.SessionInspect) *runtimev1.SessionInspect {
	root := nodes[rootID]
	if root == nil {
		return nil
	}
	var attachChildren func(parentID string) []*runtimev1.SessionInspect
	attachChildren = func(parentID string) []*runtimev1.SessionInspect {
		var children []*runtimev1.SessionInspect
		for id, node := range nodes {
			if id == rootID || node.GetParentSessionId() != parentID {
				continue
			}
			node.Children = attachChildren(id)
			children = append(children, node)
		}
		sort.Slice(children, func(i, j int) bool {
			if children[i].GetDepth() != children[j].GetDepth() {
				return children[i].GetDepth() < children[j].GetDepth()
			}
			return children[i].GetCreatedAt() < children[j].GetCreatedAt()
		})
		return children
	}
	root.Children = attachChildren(rootID)
	return root
}

func sessionEndedAtUnixMs(status string, updatedAt time.Time) int64 {
	switch status {
	case model.SessionStatusCompleted, model.SessionStatusFailed, model.SessionStatusCancelled:
		if updatedAt.IsZero() {
			return 0
		}
		return updatedAt.UnixMilli()
	default:
		return 0
	}
}

func resolveSessionAgentContext(ctx context.Context, q *store.Queries, agentVersionID string) (*runtimev1.SessionAgentContext, error) {
	identity, err := q.GetAgentVersionIdentity(ctx, agentVersionID)
	if errors.Is(err, sql.ErrNoRows) {
		return &runtimev1.SessionAgentContext{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := &runtimev1.SessionAgentContext{
		Namespace: identity.Namespace,
		Name:      identity.Name,
		Version:   identity.Version,
	}
	agent, err := manifest.ParseJSON(identity.Manifest)
	if err != nil {
		return out, nil
	}
	out.ModelProvider = agent.Spec.Model.Provider
	out.ModelName = agent.Spec.Model.Name
	if lim := agent.Spec.Limits; lim != nil {
		if lim.MaxTokensPerRun != nil && *lim.MaxTokensPerRun > 0 {
			out.MaxTokensPerRun = int32(*lim.MaxTokensPerRun)
		}
		if lim.MaxWallClockSeconds != nil && *lim.MaxWallClockSeconds > 0 {
			out.MaxWallClockSeconds = int32(*lim.MaxWallClockSeconds)
		}
	}
	return out, nil
}

func sessionOutputToProto(output json.RawMessage) *runtimev1.SessionOutputInspect {
	var obj sessionOutput
	if err := json.Unmarshal(output, &obj); err != nil {
		return nil
	}
	out := &runtimev1.SessionOutputInspect{
		Message:    obj.Message,
		StopReason: obj.StopReason,
	}
	if obj.TurnUsage != nil {
		out.TurnUsage = tokenUsageToProto(usageFromSessionOutput(obj.TurnUsage))
	}
	if obj.SessionUsage != nil {
		out.SessionUsage = tokenUsageToProto(usageFromSessionOutput(obj.SessionUsage))
	}
	for _, turn := range obj.Turns {
		entry := &runtimev1.SessionTurnInspect{
			StopReason:     turn.StopReason,
			TurnDurationMs: turn.TurnDurationMs,
		}
		if turn.TurnUsage != nil {
			entry.TurnUsage = tokenUsageToProto(usageFromSessionOutput(turn.TurnUsage))
		}
		out.Turns = append(out.Turns, entry)
	}
	return out
}

func sessionEventsToProto(events []store.SessionEvent) []*runtimev1.SessionEventEntry {
	out := make([]*runtimev1.SessionEventEntry, 0, len(events))
	for _, ev := range events {
		out = append(out, &runtimev1.SessionEventEntry{
			Id:        ev.ID,
			Type:      ev.Type,
			Payload:   ev.Payload,
			CreatedAt: formatTime(ev.CreatedAt),
		})
	}
	return out
}

func toolInvocationsToProto(invocations []store.ToolInvocation) []*runtimev1.ToolInvocationEntry {
	out := make([]*runtimev1.ToolInvocationEntry, 0, len(invocations))
	for _, inv := range invocations {
		out = append(out, toolInvocationToProto(inv))
	}
	return out
}

func toolInvocationToProto(inv store.ToolInvocation) *runtimev1.ToolInvocationEntry {
	entry := &runtimev1.ToolInvocationEntry{
		CallId:              inv.CallID,
		AgentVersionId:      inv.AgentVersionID,
		Turn:                int32(inv.Turn),
		Tool:                inv.Tool,
		Version:             inv.Version,
		Args:                inv.Args,
		Result:              inv.Result,
		Status:              inv.Status,
		WorkerIdentity:      inv.WorkerIdentity,
		ImageDigest:         inv.ImageDigest,
		DescriptorHash:      inv.DescriptorHash,
		ManifestContentHash: inv.ManifestContentHash,
		Attempt:             int32(inv.Attempt),
		CreatedAt:           formatTime(inv.CreatedAt),
		UpdatedAt:           formatTime(inv.UpdatedAt),
	}
	if inv.ErrorCode != nil {
		entry.ErrorCode = *inv.ErrorCode
	}
	if inv.ErrorMessage != nil {
		entry.ErrorMessage = *inv.ErrorMessage
	}
	if inv.UsageInputTokens > 0 || inv.UsageOutputTokens > 0 || inv.UsageEstimated {
		total := inv.UsageInputTokens + inv.UsageOutputTokens
		entry.Usage = &runtimev1.TokenUsage{
			InputTokens:  int32(inv.UsageInputTokens),
			OutputTokens: int32(inv.UsageOutputTokens),
			TotalTokens:  int32(total),
			Estimated:    inv.UsageEstimated,
		}
	}
	if inv.DispatchedAt != nil {
		entry.DispatchedAt = formatTime(*inv.DispatchedAt)
		entry.QueueDelayMs = inv.DispatchedAt.Sub(inv.CreatedAt).Milliseconds()
	}
	if inv.CompletedAt != nil {
		entry.CompletedAt = formatTime(*inv.CompletedAt)
		entry.TotalDurationMs = inv.CompletedAt.Sub(inv.CreatedAt).Milliseconds()
		if inv.DispatchedAt != nil {
			entry.ExecutionDurationMs = inv.CompletedAt.Sub(*inv.DispatchedAt).Milliseconds()
		}
	}
	return entry
}

type inspectTimelineItem struct {
	timestamp time.Time
	entry     *runtimev1.InspectTimelineEntry
}

func buildInspectTimeline(
	events []*runtimev1.SessionEventEntry,
	invocations []*runtimev1.ToolInvocationEntry,
	approvals []*runtimev1.Approval,
) []*runtimev1.InspectTimelineEntry {
	var items []inspectTimelineItem

	for _, ev := range events {
		ts, err := time.Parse(time.RFC3339Nano, ev.GetCreatedAt())
		if err != nil {
			ts, _ = time.Parse(time.RFC3339, ev.GetCreatedAt())
		}
		items = append(items, inspectTimelineItem{
			timestamp: ts,
			entry: &runtimev1.InspectTimelineEntry{
				Timestamp: ev.GetCreatedAt(),
				Source:    "event",
				Kind:      ev.GetType(),
				Summary:   sessionEventSummary(ev.GetType(), ev.GetPayload()),
				Event:     ev,
			},
		})
	}

	for _, inv := range invocations {
		if inv.GetDispatchedAt() != "" {
			ts := parseRFC3339(inv.GetDispatchedAt())
			items = append(items, inspectTimelineItem{
				timestamp: ts,
				entry: &runtimev1.InspectTimelineEntry{
					Timestamp:  inv.GetDispatchedAt(),
					Source:     "invocation",
					Kind:       "invocation_dispatched",
					Summary:    fmt.Sprintf("dispatched %s@%s call_id=%s queue_delay_ms=%d", inv.GetTool(), inv.GetVersion(), inv.GetCallId(), inv.GetQueueDelayMs()),
					Invocation: inv,
				},
			})
		}
		if inv.GetCompletedAt() != "" {
			ts := parseRFC3339(inv.GetCompletedAt())
			items = append(items, inspectTimelineItem{
				timestamp: ts,
				entry: &runtimev1.InspectTimelineEntry{
					Timestamp:  inv.GetCompletedAt(),
					Source:     "invocation",
					Kind:       "invocation_completed",
					Summary:    fmt.Sprintf("completed %s@%s call_id=%s status=%s exec_ms=%d", inv.GetTool(), inv.GetVersion(), inv.GetCallId(), inv.GetStatus(), inv.GetExecutionDurationMs()),
					Invocation: inv,
				},
			})
		}
	}

	for _, appr := range approvals {
		if appr.GetCreatedAt() != "" {
			ts := parseRFC3339(appr.GetCreatedAt())
			items = append(items, inspectTimelineItem{
				timestamp: ts,
				entry: &runtimev1.InspectTimelineEntry{
					Timestamp: appr.GetCreatedAt(),
					Source:    "approval",
					Kind:      "approval_created",
					Summary:   fmt.Sprintf("approval %s tool=%s@%s status=%s", appr.GetId(), appr.GetTool(), appr.GetVersion(), appr.GetStatus()),
					Approval:  appr,
				},
			})
		}
		if appr.GetDecidedAt() != "" {
			ts := parseRFC3339(appr.GetDecidedAt())
			items = append(items, inspectTimelineItem{
				timestamp: ts,
				entry: &runtimev1.InspectTimelineEntry{
					Timestamp: appr.GetDecidedAt(),
					Source:    "approval",
					Kind:      "approval_decided",
					Summary:   fmt.Sprintf("approval %s decided status=%s by=%s", appr.GetId(), appr.GetStatus(), appr.GetDecidedBy()),
					Approval:  appr,
				},
			})
		}
	}

	sort.Slice(items, func(i, j int) bool {
		if items[i].timestamp.Equal(items[j].timestamp) {
			return items[i].entry.GetKind() < items[j].entry.GetKind()
		}
		return items[i].timestamp.Before(items[j].timestamp)
	})

	out := make([]*runtimev1.InspectTimelineEntry, 0, len(items))
	var prev time.Time
	for _, item := range items {
		entry := item.entry
		if !prev.IsZero() && !item.timestamp.IsZero() {
			entry.GapMs = item.timestamp.Sub(prev).Milliseconds()
		}
		out = append(out, entry)
		if !item.timestamp.IsZero() {
			prev = item.timestamp
		}
	}
	return out
}

func parseRFC3339(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t
	}
	t, _ := time.Parse(time.RFC3339, s)
	return t
}

func sessionEventSummary(typ string, payload json.RawMessage) string {
	switch model.SessionEventType(typ) {
	case model.SessionEventUserMessage:
		if msg, err := conversationMessageFromSessionEvent(payload); err == nil {
			return fmt.Sprintf("user: %s", summarizeText(msg.GetContent()))
		}
	case model.SessionEventAssistantMessage:
		if msg, err := conversationMessageFromSessionEvent(payload); err == nil {
			usage := ""
			if u := msg.GetTurnUsage(); u != nil {
				usage = fmt.Sprintf(" tokens=%d", u.GetTotalTokens())
			}
			dur := ""
			if msg.GetTurnDurationMs() > 0 {
				dur = fmt.Sprintf(" duration_ms=%d", msg.GetTurnDurationMs())
			}
			return fmt.Sprintf("assistant (%s): %s%s%s", msg.GetStopReason(), summarizeText(msg.GetContent()), usage, dur)
		}
	case model.SessionEventToolCall:
		if msg, err := serverMsgFromSessionEvent(payload); err == nil {
			if tc := msg.GetToolCall(); tc != nil {
				return fmt.Sprintf("tool_call %s@%s call_id=%s", tc.GetTool(), tc.GetVersion(), tc.GetCallId())
			}
		}
	case model.SessionEventToolResult:
		if msg, err := serverMsgFromSessionEvent(payload); err == nil {
			if tr := msg.GetToolResult(); tr != nil {
				if tr.GetErrorMessage() != "" {
					return fmt.Sprintf("tool_result call_id=%s error=%s", tr.GetCallId(), tr.GetErrorMessage())
				}
				return fmt.Sprintf("tool_result call_id=%s", tr.GetCallId())
			}
		}
	case model.SessionEventApprovalRequired:
		if msg, err := serverMsgFromSessionEvent(payload); err == nil {
			if ar := msg.GetApprovalRequired(); ar != nil {
				return fmt.Sprintf("approval_required %s tool=%s@%s", ar.GetApprovalId(), ar.GetTool(), ar.GetVersion())
			}
		}
	case model.SessionEventApprovalDecided:
		var decided approvalDecidedPayload
		if err := json.Unmarshal(payload, &decided); err == nil {
			decision := "denied"
			if decided.Approved {
				decision = "approved"
			}
			return fmt.Sprintf("approval_decided %s %s by=%s", decided.ApprovalID, decision, decided.DecidedBy)
		}
	case model.SessionEventPolicyDenied:
		return "policy_denied"
	case model.SessionEventSessionCompleted:
		return "session_completed"
	case model.SessionEventSessionFailed:
		return "session_failed"
	case model.SessionEventSessionCancelled:
		return "session_cancelled"
	}
	return typ
}

func summarizeText(s string) string {
	s = strings.TrimSpace(s)
	if len(s) <= 120 {
		return s
	}
	return s[:117] + "..."
}
