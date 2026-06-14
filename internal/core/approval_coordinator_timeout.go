package core

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/phrony-platform/runtime/internal/model"
	"github.com/phrony-platform/runtime/internal/policy"
	"github.com/phrony-platform/runtime/internal/store"
)

const (
	approvalRuntimeTimeoutDefault     = "phrony.com/timeout_default"
	approvalRuntimeTimeoutAfter       = "phrony.com/timeout_after_minutes"
	approvalRuntimeEscalateAfter      = "phrony.com/escalate_after_minutes"
	approvalRuntimeEscalateToRole     = "decision.runtime.phrony.com/escalate_to_role"
	approvalRuntimeEscalateDepth      = "phrony.com/escalation_depth"
	maxApprovalEscalationDepth        = 5
)

func mergeApprovalRuntime(req policy.ApprovalRequest) json.RawMessage {
	m := make(map[string]any)
	for k, v := range req.Runtime {
		m[k] = v
	}
	if d := strings.TrimSpace(req.TimeoutDefault); d != "" {
		m[approvalRuntimeTimeoutDefault] = d
	}
	if req.TimeoutAfterMinutes > 0 {
		m[approvalRuntimeTimeoutAfter] = req.TimeoutAfterMinutes
	}
	if len(m) == 0 {
		return nil
	}
	b, _ := json.Marshal(m)
	return b
}

func approvalTimeoutDefault(runtime json.RawMessage) string {
	return approvalRuntimeString(runtime, approvalRuntimeTimeoutDefault, "deny")
}

func approvalEscalateAfterMinutes(runtime json.RawMessage, fallback int) int {
	if v := approvalRuntimeInt(runtime, approvalRuntimeEscalateAfter); v > 0 {
		return v
	}
	if fallback > 0 {
		return fallback
	}
	return approvalRuntimeInt(runtime, approvalRuntimeTimeoutAfter)
}

func approvalEscalateRoute(runtime json.RawMessage, currentRoute string) string {
	if r := approvalRuntimeString(runtime, approvalRuntimeEscalateToRole, ""); r != "" {
		return r
	}
	return currentRoute
}

func approvalEscalationDepth(runtime json.RawMessage) int {
	return approvalRuntimeInt(runtime, approvalRuntimeEscalateDepth)
}

func approvalRuntimeString(runtime json.RawMessage, key, fallback string) string {
	if v := approvalRuntimeValue(runtime, key); v != nil {
		if s := strings.TrimSpace(fmt.Sprint(v)); s != "" {
			return s
		}
	}
	return fallback
}

func approvalRuntimeInt(runtime json.RawMessage, key string) int {
	v := approvalRuntimeValue(runtime, key)
	if v == nil {
		return 0
	}
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	default:
		var i int
		if _, err := fmt.Sscanf(fmt.Sprint(v), "%d", &i); err == nil {
			return i
		}
	}
	return 0
}

func approvalRuntimeValue(runtime json.RawMessage, key string) any {
	if len(runtime) == 0 || strings.TrimSpace(key) == "" {
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal(runtime, &m); err != nil {
		return nil
	}
	return m[key]
}

func (c *approvalCoordinator) armApprovalTimeout(approvalID string, expiresAt time.Time) {
	if c == nil || approvalID == "" || expiresAt.IsZero() {
		return
	}
	delay := time.Until(expiresAt)
	if delay < 0 {
		delay = 0
	}
	c.mu.Lock()
	if c.timers == nil {
		c.timers = make(map[string]*time.Timer)
	}
	if old := c.timers[approvalID]; old != nil {
		old.Stop()
	}
	c.timers[approvalID] = time.AfterFunc(delay, func() {
		c.handleApprovalTimeout(approvalID)
	})
	c.mu.Unlock()
}

func (c *approvalCoordinator) cancelApprovalTimeout(approvalID string) {
	if c == nil || approvalID == "" {
		return
	}
	c.mu.Lock()
	if t := c.timers[approvalID]; t != nil {
		t.Stop()
		delete(c.timers, approvalID)
	}
	c.mu.Unlock()
}

func (c *approvalCoordinator) armApprovalTimeoutFromRow(row store.Approval) {
	if row.ExpiresAt != nil {
		c.armApprovalTimeout(row.ID, *row.ExpiresAt)
	}
}

func (c *approvalCoordinator) handleApprovalTimeout(approvalID string) {
	ctx := context.Background()
	unlock := c.lockApproval(approvalID)
	defer unlock()

	q, err := c.server.queries()
	if err != nil {
		return
	}
	row, err := q.GetApproval(ctx, approvalID)
	if err != nil || row.Status != model.ApprovalStatusPending {
		return
	}

	params := approvalDecideParams{
		ApprovalID:                approvalID,
		DecidedBy:                 "system:timeout",
		ComprehensionAcknowledged: true,
	}
	switch strings.ToLower(approvalTimeoutDefault(row.PolicyRuntime)) {
	case "allow":
		params.Approved = true
		if _, err := c.decideLoaded(ctx, q, params, row); err != nil {
			slog.Error("approval timeout auto-allow failed", "approval_id", approvalID, "error", err)
		}
	case "escalate":
		if err := c.escalateTimedOutApproval(ctx, q, row); err != nil {
			slog.Error("approval timeout escalate", "approval_id", approvalID, "error", err)
		}
	default:
		params.Approved = false
		params.Comment = "approval timed out"
		if _, err := c.decideLoaded(ctx, q, params, row); err != nil {
			slog.Error("approval timeout auto-deny failed", "approval_id", approvalID, "error", err)
		}
	}
}

func (c *approvalCoordinator) escalateTimedOutApproval(ctx context.Context, q *store.Queries, row store.Approval) error {
	depth := approvalEscalationDepth(row.PolicyRuntime)
	if depth >= maxApprovalEscalationDepth {
		_, err := c.decideLoaded(ctx, q, approvalDecideParams{
			ApprovalID:                row.ID,
			Approved:                  false,
			DecidedBy:                 "system:timeout",
			Comment:                   "approval escalation depth exceeded",
			ComprehensionAcknowledged: true,
		}, row)
		return err
	}
	if _, _, err := appendEventAuto(ctx, q, EventInput{
		SessionID: row.SessionID,
		Type:      EventApprovalEscalated,
		CallID:    strPtrIf(row.CallID),
		Actor:     ActorSystem,
		Payload:   marshalSessionEventJSON(map[string]string{"approval_id": row.ID}),
		Approval: &EventApprovalProjection{
			EscalateID: row.ID,
			EscalateBy: "system:timeout",
		},
	}); err != nil {
		return err
	}
	c.cancelApprovalTimeout(row.ID)
	c.unlockApprovalCleanup(row.ID)

	childID := uuid.NewString()
	route := approvalEscalateRoute(row.PolicyRuntime, row.Route)
	afterMin := approvalEscalateAfterMinutes(row.PolicyRuntime, 0)
	var expiresAt *time.Time
	if afterMin > 0 {
		t := time.Now().Add(time.Duration(afterMin) * time.Minute)
		expiresAt = &t
	}
	runtime := row.PolicyRuntime
	if len(runtime) > 0 {
		var m map[string]any
		if json.Unmarshal(runtime, &m) == nil {
			m[approvalRuntimeEscalateDepth] = depth + 1
			runtime, _ = json.Marshal(m)
		}
	}

	args := row.Args
	if len(args) == 0 {
		args = json.RawMessage("{}")
	}
	childApproval := store.InsertApprovalParams{
		ID:                    childID,
		SessionID:             row.SessionID,
		CallID:                row.CallID,
		Status:                model.ApprovalStatusPending,
		Route:                 route,
		Reason:                row.Reason,
		Tool:                  row.Tool,
		Version:               row.Version,
		Args:                  args,
		AuthorityRef:          row.AuthorityRef,
		PolicyName:            row.PolicyName,
		ApprovalsRequired:     row.ApprovalsRequired,
		ComprehensionRequired: row.ComprehensionRequired,
		OnReject:              row.OnReject,
		OnModify:              row.OnModify,
		ExpiresAt:             expiresAt,
		PolicyRuntime:         runtime,
	}
	callID := row.CallID
	if _, _, err := appendEventAuto(ctx, q, EventInput{
		SessionID: row.SessionID,
		Type:      EventApprovalRequired,
		CallID:    &callID,
		Actor:     ActorPolicy,
		Payload:   marshalSessionEventJSON(childApproval),
		Approval: &EventApprovalProjection{
			Open: &childApproval,
			OpenInvocation: &store.InsertToolInvocationPendingParams{
				CallID:    row.CallID,
				SessionID: row.SessionID,
				Tool:      row.Tool,
				Version:   row.Version,
				Args:      args,
				Status:    model.ToolInvocationAwaitingApproval,
			},
		},
	}); err != nil {
		return err
	}
	c.registerParked(row.SessionID, childID)
	if expiresAt != nil {
		c.armApprovalTimeout(childID, *expiresAt)
	}
	if gate := c.gateForSession(row.SessionID); gate != nil {
		childRow := row
		childRow.ID = childID
		childRow.Status = model.ApprovalStatusPending
		childRow.Route = route
		childRow.ExpiresAt = expiresAt
		childRow.ApprovalsReceived = 0
		childRow.PolicyRuntime = runtime
		req, err := approvalRequestFromStore(ctx, q, childRow, row.SessionID)
		if err != nil {
			return err
		}
		_ = gate.sendApprovalRequired(req)
		gate.setPending(req)
	}
	return nil
}
