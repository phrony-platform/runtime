package core

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
	"github.com/phrony-platform/runtime/internal/manifest"
	"github.com/phrony-platform/runtime/internal/model"
	"github.com/phrony-platform/runtime/internal/store"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/structpb"
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

	depthBySession := make(map[string]int32, len(nodes))
	var collectDepth func(*runtimev1.SessionInspect)
	collectDepth = func(n *runtimev1.SessionInspect) {
		if n == nil {
			return
		}
		depthBySession[n.GetId()] = n.GetDepth()
		for _, child := range n.GetChildren() {
			collectDepth(child)
		}
	}
	collectDepth(root)

	rootEvents, err := q.ListEventsByRoot(ctx, sessionID)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "list root session events: %v", err)
	}

	timeline, err := buildTreeInspectTimeline(ctx, q, rootEvents, descendantIDs, depthBySession)
	if err != nil {
		return nil, err
	}

	return &runtimev1.InspectSessionResponse{
		Session:  root,
		Timeline: timeline,
	}, nil
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

	inputValue, err := jsonToProtoValue(session.Input)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "encode session input: %v", err)
	}

	out := &runtimev1.SessionInspect{
		Id:                     session.ID,
		AgentVersionId:         session.AgentVersionID,
		Status:                 session.Status,
		Depth:                  int32(meta.Depth),
		CreatedAt:              formatTime(session.CreatedAt),
		UpdatedAt:              formatTime(session.UpdatedAt),
		Input:                  inputValue,
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
	if output, err := loadSessionOutputJSON(ctx, q, sessionID); err != nil {
		return nil, status.Errorf(codes.Internal, "load session output: %v", err)
	} else if len(output) > 0 {
		out.Output = sessionOutputToProto(output)
	}
	if sessionEndedAtUnixMs := sessionEndedAtUnixMs(session.Status, session.UpdatedAt); sessionEndedAtUnixMs > 0 {
		out.SessionEndedAtUnixMs = sessionEndedAtUnixMs
	}

	agentCtx, err := resolveSessionAgentContext(ctx, q, session.AgentVersionID)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "resolve agent context: %v", err)
	}
	out.Agent = agentCtx

	events, err := q.ListEventsBySession(ctx, sessionID)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "list session events: %v", err)
	}
	for _, ev := range events {
		if ev.Type == EventEvidenceRecorded {
			if meta, metaErr := jsonToProtoValue(ev.Payload); metaErr == nil {
				out.DescriptiveMetadata = meta
			}
			break
		}
	}
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

func jsonToProtoValue(raw []byte) (*structpb.Value, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, err
	}
	expandEmbeddedJSONBytes(v)
	return structpb.NewValue(v)
}

// expandEmbeddedJSONBytes walks decoded JSON and replaces protojson base64 (or
// raw JSON string) values at known bytes-as-JSON keys with nested objects/arrays.
// Wire-backed event payloads store tool args/results as protobuf bytes, which
// otherwise appear as base64 strings in InspectSession --json output.
func expandEmbeddedJSONBytes(v any) {
	switch x := v.(type) {
	case map[string]any:
		for k, val := range x {
			if s, ok := val.(string); ok && isEmbeddedJSONBytesKey(k) {
				if decoded, ok := decodeEmbeddedJSON(s); ok {
					x[k] = decoded
					expandEmbeddedJSONBytes(decoded)
					continue
				}
			}
			expandEmbeddedJSONBytes(val)
		}
	case []any:
		for _, item := range x {
			expandEmbeddedJSONBytes(item)
		}
	}
}

func isEmbeddedJSONBytesKey(k string) bool {
	switch k {
	case "args", "payload", "output", "policyRuntime", "policy_runtime":
		return true
	default:
		return false
	}
}

func decodeEmbeddedJSON(s string) (any, bool) {
	if s == "" {
		return nil, false
	}
	raw := []byte(s)
	if s[0] != '{' && s[0] != '[' {
		decoded, err := base64.StdEncoding.DecodeString(s)
		if err != nil {
			decoded, err = base64.RawStdEncoding.DecodeString(s)
			if err != nil {
				return nil, false
			}
		}
		raw = decoded
	}
	if len(raw) == 0 {
		return nil, false
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, false
	}
	return v, true
}

func sessionEventsToProto(events []store.Event) ([]*runtimev1.SessionEventEntry, error) {
	out := make([]*runtimev1.SessionEventEntry, 0, len(events))
	for _, ev := range events {
		entry, err := eventToProto(ev)
		if err != nil {
			return nil, err
		}
		out = append(out, entry)
	}
	return out, nil
}

func eventToProto(ev store.Event) (*runtimev1.SessionEventEntry, error) {
	payload, err := jsonToProtoValue(ev.Payload)
	if err != nil {
		return nil, fmt.Errorf("event %d payload: %w", ev.ID, err)
	}
	entry := &runtimev1.SessionEventEntry{
		Id:        ev.ID,
		Type:      ev.Type,
		Payload:   payload,
		CreatedAt: formatTime(ev.TS),
		Seq:       int32(ev.Seq),
		TsUnixMs:  ev.TS.UnixMilli(),
		SessionId: ev.SessionID,
		Actor:     ev.Actor,
	}
	if ev.Turn != nil {
		entry.Turn = int32(*ev.Turn)
	}
	if ev.CallID != nil {
		entry.CallId = *ev.CallID
	}
	if ev.ChildSessionID != nil {
		entry.ChildSessionId = *ev.ChildSessionID
	}
	return entry, nil
}

func toolInvocationsToProto(invocations []store.ToolInvocation) ([]*runtimev1.ToolInvocationEntry, error) {
	out := make([]*runtimev1.ToolInvocationEntry, 0, len(invocations))
	for _, inv := range invocations {
		entry, err := toolInvocationToProto(inv)
		if err != nil {
			return nil, err
		}
		out = append(out, entry)
	}
	return out, nil
}

func toolInvocationToProto(inv store.ToolInvocation) (*runtimev1.ToolInvocationEntry, error) {
	args, err := jsonToProtoValue(inv.Args)
	if err != nil {
		return nil, fmt.Errorf("invocation %s args: %w", inv.CallID, err)
	}
	result, err := jsonToProtoValue(inv.Result)
	if err != nil {
		return nil, fmt.Errorf("invocation %s result: %w", inv.CallID, err)
	}
	entry := &runtimev1.ToolInvocationEntry{
		CallId:              inv.CallID,
		AgentVersionId:      inv.AgentVersionID,
		Turn:                int32(inv.Turn),
		Tool:                inv.Tool,
		Version:             inv.Version,
		Args:                args,
		Result:              result,
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
	return entry, nil
}

func approvalToInspectProto(row store.Approval, votes []store.ApprovalVote) (*runtimev1.InspectApproval, error) {
	required := row.ApprovalsRequired
	if required <= 0 {
		required = 1
	}
	args, err := jsonToProtoValue(row.Args)
	if err != nil {
		return nil, fmt.Errorf("approval %s args: %w", row.ID, err)
	}
	policyRuntime, err := jsonToProtoValue(row.PolicyRuntime)
	if err != nil {
		return nil, fmt.Errorf("approval %s policy_runtime: %w", row.ID, err)
	}
	out := &runtimev1.InspectApproval{
		Id:                    row.ID,
		SessionId:             row.SessionID,
		CallId:                row.CallID,
		Status:                row.Status,
		Route:                 row.Route,
		Reason:                row.Reason,
		Tool:                  row.Tool,
		Version:               row.Version,
		Args:                  args,
		AuthorityRef:          row.AuthorityRef,
		PolicyName:            row.PolicyName,
		PolicyRuntime:         policyRuntime,
		ApprovalsRequired:     int32(required),
		ApprovalsReceived:     int32(row.ApprovalsReceived),
		ComprehensionRequired: row.ComprehensionRequired,
		OnReject:              row.OnReject,
		OnModify:              row.OnModify,
		CreatedAt:             formatTime(row.CreatedAt),
		DecidedBy:             row.DecidedBy,
		Comment:               row.Comment,
	}
	if row.ExpiresAt != nil {
		out.ExpiresAt = formatTime(*row.ExpiresAt)
	}
	if row.DecidedAt != nil {
		out.DecidedAt = formatTime(*row.DecidedAt)
	}
	if len(votes) > 0 {
		out.Votes = make([]*runtimev1.ApprovalVote, 0, len(votes))
		for _, v := range votes {
			out.Votes = append(out.Votes, &runtimev1.ApprovalVote{
				DecidedBy:                 v.DecidedBy,
				Decision:                  v.Decision,
				Comment:                   v.Comment,
				ComprehensionAcknowledged: v.ComprehensionAcknowledged,
				CreatedAt:                 formatTime(v.CreatedAt),
			})
		}
	}
	return out, nil
}

type inspectTimelineItem struct {
	timestamp time.Time
	sortKey   int64
	entry     *runtimev1.InspectTimelineEntry
}

func buildTreeInspectTimeline(
	ctx context.Context,
	q *store.Queries,
	events []store.Event,
	sessionIDs []string,
	depthBySession map[string]int32,
) ([]*runtimev1.InspectTimelineEntry, error) {
	protoEvents, err := sessionEventsToProto(events)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "encode session events: %v", err)
	}

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
				Depth:     depthBySession[ev.SessionID],
				Source:    "event",
				Kind:      ev.Type,
				Summary:   sessionEventSummary(ev.Type, ev.Payload),
				Event:     protoEvents[i],
			},
		})
	}

	var seq int64
	for _, sessionID := range sessionIDs {
		invocations, err := q.ListToolInvocationsBySessionID(ctx, sessionID)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "list tool invocations: %v", err)
		}
		invProto, err := toolInvocationsToProto(invocations)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "encode tool invocations: %v", err)
		}
		depth := depthBySession[sessionID]
		for _, inv := range invProto {
			if inv.GetDispatchedAt() != "" {
				ts := parseRFC3339(inv.GetDispatchedAt())
				seq++
				items = append(items, inspectTimelineItem{
					timestamp: ts,
					sortKey:   seq,
					entry: &runtimev1.InspectTimelineEntry{
						Timestamp:  inv.GetDispatchedAt(),
						TsUnixMs:   ts.UnixMilli(),
						SessionId:  sessionID,
						Depth:      depth,
						Source:     "invocation",
						Kind:       "invocation_dispatched",
						Summary:    fmt.Sprintf("dispatched %s@%s call_id=%s queue_delay_ms=%d", inv.GetTool(), inv.GetVersion(), inv.GetCallId(), inv.GetQueueDelayMs()),
						Invocation: inv,
					},
				})
			}
			if inv.GetCompletedAt() != "" {
				ts := parseRFC3339(inv.GetCompletedAt())
				seq++
				items = append(items, inspectTimelineItem{
					timestamp: ts,
					sortKey:   seq,
					entry: &runtimev1.InspectTimelineEntry{
						Timestamp:  inv.GetCompletedAt(),
						TsUnixMs:   ts.UnixMilli(),
						SessionId:  sessionID,
						Depth:      depth,
						Source:     "invocation",
						Kind:       "invocation_completed",
						Summary:    fmt.Sprintf("completed %s@%s call_id=%s status=%s exec_ms=%d", inv.GetTool(), inv.GetVersion(), inv.GetCallId(), inv.GetStatus(), inv.GetExecutionDurationMs()),
						Invocation: inv,
					},
				})
			}
		}

		approvalRows, err := q.ListApprovals(ctx, store.ListApprovalsParams{SessionID: sessionID})
		if err != nil {
			return nil, status.Errorf(codes.Internal, "list approvals: %v", err)
		}
		for _, row := range approvalRows {
			enriched, enrichErr := enrichApprovalFromInvocation(ctx, q, row)
			if enrichErr != nil {
				return nil, status.Errorf(codes.Internal, "load approval context: %v", enrichErr)
			}
			votes, votesErr := q.ListApprovalVotes(ctx, row.ID)
			if votesErr != nil {
				return nil, status.Errorf(codes.Internal, "list approval votes: %v", votesErr)
			}
			appr, apprErr := approvalToInspectProto(enriched, votes)
			if apprErr != nil {
				return nil, status.Errorf(codes.Internal, "encode approval: %v", apprErr)
			}
			if appr.GetCreatedAt() != "" {
				ts := parseRFC3339(appr.GetCreatedAt())
				seq++
				items = append(items, inspectTimelineItem{
					timestamp: ts,
					sortKey:   seq,
					entry: &runtimev1.InspectTimelineEntry{
						Timestamp: appr.GetCreatedAt(),
						TsUnixMs:  ts.UnixMilli(),
						SessionId: sessionID,
						Depth:     depth,
						Source:    "approval",
						Kind:      "approval_created",
						Summary:   fmt.Sprintf("approval %s tool=%s@%s status=%s", appr.GetId(), appr.GetTool(), appr.GetVersion(), appr.GetStatus()),
						Approval:  appr,
					},
				})
			}
			if appr.GetDecidedAt() != "" {
				ts := parseRFC3339(appr.GetDecidedAt())
				seq++
				items = append(items, inspectTimelineItem{
					timestamp: ts,
					sortKey:   seq,
					entry: &runtimev1.InspectTimelineEntry{
						Timestamp: appr.GetDecidedAt(),
						TsUnixMs:  ts.UnixMilli(),
						SessionId: sessionID,
						Depth:     depth,
						Source:    "approval",
						Kind:      "approval_decided",
						Summary:   fmt.Sprintf("approval %s decided status=%s by=%s", appr.GetId(), appr.GetStatus(), appr.GetDecidedBy()),
						Approval:  appr,
					},
				})
			}
		}
	}

	return finalizeInspectTimeline(items), nil
}

// buildInspectTimeline merges events, invocation milestones, and approvals for tests
// and local ordering checks. Production inspect uses buildTreeInspectTimeline.
func buildInspectTimeline(
	events []*runtimev1.SessionEventEntry,
	invocations []*runtimev1.ToolInvocationEntry,
	approvals []*runtimev1.InspectApproval,
) []*runtimev1.InspectTimelineEntry {
	var items []inspectTimelineItem

	var seq int64
	for _, ev := range events {
		ts := eventTimestamp(ev)
		seq++
		items = append(items, inspectTimelineItem{
			timestamp: ts,
			sortKey:   ev.GetId(),
			entry: &runtimev1.InspectTimelineEntry{
				Timestamp: formatInspectTimestamp(ev),
				TsUnixMs:  ev.GetTsUnixMs(),
				Seq:       ev.GetSeq(),
				SessionId: ev.GetSessionId(),
				Source:    "event",
				Kind:      ev.GetType(),
				Summary:   sessionEventSummary(ev.GetType(), protoValueJSON(ev.GetPayload())),
				Event:     ev,
			},
		})
	}

	for _, inv := range invocations {
		if inv.GetDispatchedAt() != "" {
			ts := parseRFC3339(inv.GetDispatchedAt())
			seq++
			items = append(items, inspectTimelineItem{
				timestamp: ts,
				sortKey:   seq,
				entry: &runtimev1.InspectTimelineEntry{
					Timestamp:  inv.GetDispatchedAt(),
					TsUnixMs:   ts.UnixMilli(),
					Source:     "invocation",
					Kind:       "invocation_dispatched",
					Summary:    fmt.Sprintf("dispatched %s@%s call_id=%s queue_delay_ms=%d", inv.GetTool(), inv.GetVersion(), inv.GetCallId(), inv.GetQueueDelayMs()),
					Invocation: inv,
				},
			})
		}
		if inv.GetCompletedAt() != "" {
			ts := parseRFC3339(inv.GetCompletedAt())
			seq++
			items = append(items, inspectTimelineItem{
				timestamp: ts,
				sortKey:   seq,
				entry: &runtimev1.InspectTimelineEntry{
					Timestamp:  inv.GetCompletedAt(),
					TsUnixMs:   ts.UnixMilli(),
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
			seq++
			items = append(items, inspectTimelineItem{
				timestamp: ts,
				sortKey:   seq,
				entry: &runtimev1.InspectTimelineEntry{
					Timestamp: appr.GetCreatedAt(),
					TsUnixMs:  ts.UnixMilli(),
					Source:    "approval",
					Kind:      "approval_created",
					Summary:   fmt.Sprintf("approval %s tool=%s@%s status=%s", appr.GetId(), appr.GetTool(), appr.GetVersion(), appr.GetStatus()),
					Approval:  appr,
				},
			})
		}
		if appr.GetDecidedAt() != "" {
			ts := parseRFC3339(appr.GetDecidedAt())
			seq++
			items = append(items, inspectTimelineItem{
				timestamp: ts,
				sortKey:   seq,
				entry: &runtimev1.InspectTimelineEntry{
					Timestamp: appr.GetDecidedAt(),
					TsUnixMs:  ts.UnixMilli(),
					Source:    "approval",
					Kind:      "approval_decided",
					Summary:   fmt.Sprintf("approval %s decided status=%s by=%s", appr.GetId(), appr.GetStatus(), appr.GetDecidedBy()),
					Approval:  appr,
				},
			})
		}
	}

	return finalizeInspectTimeline(items)
}

func eventTimestamp(ev *runtimev1.SessionEventEntry) time.Time {
	if ev.GetTsUnixMs() > 0 {
		return time.UnixMilli(ev.GetTsUnixMs())
	}
	return parseRFC3339(ev.GetCreatedAt())
}

func formatInspectTimestamp(ev *runtimev1.SessionEventEntry) string {
	if ev.GetTsUnixMs() > 0 {
		return fmt.Sprintf("%d", ev.GetTsUnixMs())
	}
	return ev.GetCreatedAt()
}

func finalizeInspectTimeline(items []inspectTimelineItem) []*runtimev1.InspectTimelineEntry {
	sort.Slice(items, func(i, j int) bool {
		if items[i].timestamp.Equal(items[j].timestamp) {
			return items[i].sortKey < items[j].sortKey
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

func protoValueJSON(v *structpb.Value) json.RawMessage {
	if v == nil {
		return nil
	}
	b, err := v.MarshalJSON()
	if err != nil {
		return nil
	}
	return b
}

func sessionEventSummary(typ string, payload json.RawMessage) string {
	typ = legacyInspectEventType(typ)
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
				if tc.GetAgentDelegation() {
					target := tc.GetDelegationTarget()
					if target == "" {
						target = fmt.Sprintf("%s@%s", tc.GetTool(), tc.GetVersion())
					}
					return fmt.Sprintf(
						"agent_delegation target=%s child_session_id=%s call_id=%s",
						target, tc.GetChildSessionId(), tc.GetCallId(),
					)
				}
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

func legacyInspectEventType(typ string) string {
	switch typ {
	case EventMessageUser:
		return string(model.SessionEventUserMessage)
	case EventMessageAssistant:
		return string(model.SessionEventAssistantMessage)
	case EventToolRequested:
		return string(model.SessionEventToolCall)
	case EventToolCompleted:
		return string(model.SessionEventToolResult)
	case EventToolPolicyDenied:
		return string(model.SessionEventPolicyDenied)
	case EventApprovalRequired:
		return string(model.SessionEventApprovalRequired)
	case EventApprovalDecided:
		return string(model.SessionEventApprovalDecided)
	case EventSessionCompleted:
		return string(model.SessionEventSessionCompleted)
	case EventSessionFailed:
		return string(model.SessionEventSessionFailed)
	case EventSessionCancelled:
		return string(model.SessionEventSessionCancelled)
	default:
		return typ
	}
}

func summarizeText(s string) string {
	s = strings.TrimSpace(s)
	if len(s) <= 120 {
		return s
	}
	return s[:117] + "..."
}
