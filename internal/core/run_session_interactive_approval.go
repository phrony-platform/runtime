package core

import (
	"context"
	"encoding/json"
	"errors"
	"sync"

	"github.com/google/uuid"
	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
	"github.com/phrony-platform/runtime/internal/model"
	"github.com/phrony-platform/runtime/internal/policy"
	"github.com/phrony-platform/runtime/internal/store"
)

// sessionApprovalGate blocks tool dispatch until the interactive client sends tool_approval.
type sessionApprovalGate struct {
	stream         runtimev1.Runtime_RunSessionInteractiveServer
	q              *store.Queries
	agentVersionID string

	mu         sync.Mutex
	decisionCh chan approvalDecision
	pendingReq *policy.ApprovalRequest
}

type approvalDecision struct {
	approved bool
	err      error
}

func newSessionApprovalGate(stream runtimev1.Runtime_RunSessionInteractiveServer, q *store.Queries, agentVersionID string) *sessionApprovalGate {
	return &sessionApprovalGate{
		stream:         stream,
		q:              q,
		agentVersionID: agentVersionID,
		decisionCh:     make(chan approvalDecision, 1),
	}
}

func (g *sessionApprovalGate) WaitApproval(ctx context.Context, req policy.ApprovalRequest) (bool, error) {
	if err := g.stream.Send(&runtimev1.RunSessionInteractiveServerMsg{
		Body: &runtimev1.RunSessionInteractiveServerMsg_ApprovalRequired{
			ApprovalRequired: approvalRequiredToProto(req),
		},
	}); err != nil {
		return false, err
	}

	g.mu.Lock()
	g.pendingReq = &req
	g.mu.Unlock()

	if g.q != nil && req.SessionID != "" && req.CallID != "" {
		args := req.Args
		if len(args) == 0 {
			args = json.RawMessage("{}")
		}
		_, _ = g.q.InsertToolInvocationPending(ctx, store.InsertToolInvocationPendingParams{
			CallID:         req.CallID,
			SessionID:      req.SessionID,
			AgentVersionID: g.agentVersionID,
			Turn:           0,
			Tool:           req.Tool,
			Version:        req.Version,
			Args:           args,
			Status:         model.ToolInvocationAwaitingApproval,
		})
		_, _ = g.q.InsertApproval(ctx, store.InsertApprovalParams{
			ID:        req.ApprovalID,
			SessionID: req.SessionID,
			CallID:    req.CallID,
			Status:    model.ApprovalStatusPending,
			Route:     req.Route,
			Reason:    req.Reason,
		})
	}

	select {
	case dec := <-g.decisionCh:
		g.mu.Lock()
		g.pendingReq = nil
		g.mu.Unlock()
		return dec.approved, dec.err
	case <-ctx.Done():
		return false, ctx.Err()
	}
}

func (g *sessionApprovalGate) deliverApproval(msg *runtimev1.RunSessionInteractiveToolApproval) error {
	if msg == nil {
		return errors.New("tool_approval is required")
	}
	g.mu.Lock()
	pending := g.pendingReq
	g.mu.Unlock()
	if pending == nil {
		return errors.New("no pending approval")
	}
	if id := msg.GetApprovalId(); id != "" && id != pending.ApprovalID {
		return errors.New("approval_id does not match pending approval")
	}

	ctx := context.Background()
	if g.q != nil {
		status := model.ApprovalStatusDenied
		if msg.GetApproved() {
			status = model.ApprovalStatusApproved
		}
		if _, err := g.q.DecideApproval(ctx, store.DecideApprovalParams{
			ID:        pending.ApprovalID,
			Status:    status,
			DecidedBy: "interactive",
			Comment:   msg.GetComment(),
		}); err != nil && !errors.Is(err, context.Canceled) {
			return err
		}
		if msg.GetApproved() {
			_ = g.q.UpdateToolInvocationStatus(ctx, pending.CallID, model.ToolInvocationPending)
		} else {
			_ = g.q.UpdateToolInvocationStatus(ctx, pending.CallID, model.ToolInvocationFailed)
		}
	}

	select {
	case g.decisionCh <- approvalDecision{approved: msg.GetApproved()}:
		return nil
	default:
		return errors.New("approval decision already received")
	}
}

func (g *sessionApprovalGate) pendingApproval() *policy.ApprovalRequest {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.pendingReq
}

func (g *sessionApprovalGate) setPendingReplay(req policy.ApprovalRequest) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.pendingReq = &req
}

func approvalRequiredToProto(req policy.ApprovalRequest) *runtimev1.RunSessionInteractiveApprovalRequired {
	return &runtimev1.RunSessionInteractiveApprovalRequired{
		ApprovalId: req.ApprovalID,
		CallId:     req.CallID,
		Tool:       req.Tool,
		Version:    req.Version,
		Args:       req.Args,
		Route:      req.Route,
		Reason:     req.Reason,
	}
}

func newApprovalID() string {
	return uuid.NewString()
}
