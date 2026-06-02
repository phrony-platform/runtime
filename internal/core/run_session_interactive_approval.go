package core

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/google/uuid"
	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
	"github.com/phrony-platform/runtime/internal/policy"
	"github.com/phrony-platform/runtime/internal/store"
)

// sessionApprovalGate blocks tool dispatch until an operator approves or denies.
type hitlWaitBudget interface {
	beginHITLWait()
	endHITLWait() error
}

type sessionApprovalGate struct {
	coord          *approvalCoordinator
	sessionID      string
	events         sessionEventSink
	q              *store.Queries
	agentVersionID string
	hitl           hitlWaitBudget

	mu               sync.Mutex
	decisionCh       chan approvalDecision
	pendingReq       *policy.ApprovalRequest
	waitingApproval  bool
}

type approvalDecision struct {
	approved bool
	args     json.RawMessage
	err      error
}

func newSessionApprovalGate(
	coord *approvalCoordinator,
	sessionID string,
	events sessionEventSink,
	q *store.Queries,
	agentVersionID string,
) *sessionApprovalGate {
	return &sessionApprovalGate{
		coord:          coord,
		sessionID:      sessionID,
		events:         events,
		q:              q,
		agentVersionID: agentVersionID,
		decisionCh:     make(chan approvalDecision, 1),
	}
}

func (g *sessionApprovalGate) WaitApproval(ctx context.Context, req policy.ApprovalRequest) (policy.ApprovalResult, error) {
	g.setPending(req)

	if g.hitl != nil {
		g.hitl.beginHITLWait()
	}

	g.mu.Lock()
	g.waitingApproval = true
	g.mu.Unlock()

	var openErr error
	if g.coord != nil {
		openErr = g.coord.OpenApproval(ctx, g, req)
	} else {
		openErr = g.sendApprovalRequired(req)
	}
	if openErr != nil {
		g.mu.Lock()
		g.waitingApproval = false
		g.pendingReq = nil
		g.mu.Unlock()
		if g.hitl != nil {
			_ = g.hitl.endHITLWait()
		}
		return policy.ApprovalResult{}, openErr
	}

	defer func() {
		g.mu.Lock()
		g.waitingApproval = false
		g.mu.Unlock()
	}()

	select {
	case dec := <-g.decisionCh:
		g.mu.Lock()
		g.pendingReq = nil
		g.mu.Unlock()
		if g.hitl != nil {
			if err := g.hitl.endHITLWait(); err != nil {
				return policy.ApprovalResult{}, err
			}
		}
		return policy.ApprovalResult{Approved: dec.approved, Args: dec.args}, dec.err
	case <-ctx.Done():
		if g.hitl != nil {
			_ = g.hitl.endHITLWait()
		}
		return policy.ApprovalResult{}, ctx.Err()
	}
}

func (g *sessionApprovalGate) isWaiting() bool {
	if g == nil {
		return false
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.waitingApproval
}

func (g *sessionApprovalGate) sendApprovalRequired(req policy.ApprovalRequest) error {
	if g.events == nil {
		return nil
	}
	return g.events.Send(&runtimev1.RunSessionInteractiveServerMsg{
		Body: &runtimev1.RunSessionInteractiveServerMsg_ApprovalRequired{
			ApprovalRequired: approvalRequiredToProto(req),
		},
	})
}

func (g *sessionApprovalGate) deliverApproval(msg *runtimev1.RunSessionInteractiveToolApproval) error {
	if g.coord == nil {
		return g.deliverApprovalLegacy(msg)
	}
	return g.coord.DecideFromStream(context.Background(), g, msg)
}

func (g *sessionApprovalGate) deliverApprovalLegacy(msg *runtimev1.RunSessionInteractiveToolApproval) error {
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
	return g.deliverDecision(msg.GetApproved(), msg.GetArgs(), nil)
}

func (g *sessionApprovalGate) deliverDecision(approved bool, args json.RawMessage, err error) error {
	select {
	case g.decisionCh <- approvalDecision{approved: approved, args: args, err: err}:
		g.mu.Lock()
		g.pendingReq = nil
		g.mu.Unlock()
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

func (g *sessionApprovalGate) setPending(req policy.ApprovalRequest) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.pendingReq = &req
}

func (g *sessionApprovalGate) setPendingReplay(req policy.ApprovalRequest) {
	g.setPending(req)
}

func approvalRequiredToProto(req policy.ApprovalRequest) *runtimev1.RunSessionInteractiveApprovalRequired {
	required := req.ApprovalsRequired
	if required <= 0 {
		required = 1
	}
	out := &runtimev1.RunSessionInteractiveApprovalRequired{
		ApprovalId:            req.ApprovalID,
		CallId:                req.CallID,
		Tool:                  req.Tool,
		Version:               req.Version,
		Args:                  req.Args,
		Route:                 req.Route,
		Reason:                req.Reason,
		AuthorityRef:          req.AuthorityRef,
		PolicyName:            req.PolicyName,
		ApprovalsRequired:     int32(required),
		ApprovalsReceived:     int32(req.ApprovalsReceived),
		ComprehensionRequired: req.ComprehensionRequired,
	}
	if len(req.Runtime) > 0 {
		out.PolicyRuntime, _ = json.Marshal(req.Runtime)
	}
	if req.ExpiresAt != nil {
		out.ExpiresAt = req.ExpiresAt.UTC().Format(time.RFC3339)
	} else if req.TimeoutAfterMinutes > 0 {
		expires := time.Now().Add(time.Duration(req.TimeoutAfterMinutes) * time.Minute)
		out.ExpiresAt = expires.UTC().Format(time.RFC3339)
	}
	return out
}

func newApprovalID() string {
	return uuid.NewString()
}

func approvalRequestFromStore(ctx context.Context, q *store.Queries, pending store.Approval, sessionID string) (policy.ApprovalRequest, error) {
	var err error
	if q != nil && ctx != nil {
		pending, err = enrichApprovalFromInvocation(ctx, q, pending)
		if err != nil {
			return policy.ApprovalRequest{}, err
		}
	}
	required := pending.ApprovalsRequired
	if required <= 0 {
		required = 1
	}
	req := policy.ApprovalRequest{
		ApprovalID:            pending.ID,
		CallID:                pending.CallID,
		SessionID:             sessionID,
		Tool:                  pending.Tool,
		Version:               pending.Version,
		Args:                  pending.Args,
		Route:                 pending.Route,
		Reason:                pending.Reason,
		PolicyName:            pending.PolicyName,
		AuthorityRef:          pending.AuthorityRef,
		ApprovalsRequired:     required,
		ComprehensionRequired: pending.ComprehensionRequired,
		OnReject:              pending.OnReject,
		OnModify:              pending.OnModify,
	}
	if len(pending.PolicyRuntime) > 0 {
		var runtime map[string]any
		if jsonErr := json.Unmarshal(pending.PolicyRuntime, &runtime); jsonErr == nil {
			req.Runtime = runtime
		}
	}
	if pending.ExpiresAt != nil {
		req.ExpiresAt = pending.ExpiresAt
	}
	req.ApprovalsReceived = pending.ApprovalsReceived
	return req, nil
}
