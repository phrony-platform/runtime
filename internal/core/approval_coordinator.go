package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
	"github.com/phrony-platform/runtime/internal/model"
	"github.com/phrony-platform/runtime/internal/policy"
	"github.com/phrony-platform/runtime/internal/store"
)

type approvalCoordinator struct {
	server *runtimeServer

	mu             sync.Mutex
	approvalMu     map[string]*sync.Mutex
	sessionGates   map[string]*sessionApprovalGate
	parkedSessions map[string]string // session_id -> approval_id
	timers         map[string]*time.Timer
}

func newApprovalCoordinator(server *runtimeServer) *approvalCoordinator {
	return &approvalCoordinator{
		server:         server,
		approvalMu:     make(map[string]*sync.Mutex),
		sessionGates:   make(map[string]*sessionApprovalGate),
		parkedSessions: make(map[string]string),
		timers:         make(map[string]*time.Timer),
	}
}

func (c *approvalCoordinator) registerParked(sessionID, approvalID string) {
	if c == nil || sessionID == "" || approvalID == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.parkedSessions[sessionID] = approvalID
}

func (c *approvalCoordinator) unregisterParked(sessionID string) {
	if c == nil || sessionID == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.parkedSessions, sessionID)
}

func (c *approvalCoordinator) registerGate(sessionID string, gate *sessionApprovalGate) {
	if c == nil || sessionID == "" || gate == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sessionGates[sessionID] = gate
}

func (c *approvalCoordinator) unregisterGate(sessionID string) {
	if c == nil || sessionID == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.sessionGates, sessionID)
}

func (c *approvalCoordinator) gateForSession(sessionID string) *sessionApprovalGate {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.sessionGates[sessionID]
}

// sessionApprovalGate returns the in-process gate for a session, consulting the
// active-session registry when the coordinator map has not been populated yet.
func (c *approvalCoordinator) sessionApprovalGate(sessionID string) *sessionApprovalGate {
	gate := c.gateForSession(sessionID)
	if gate != nil || c.server == nil {
		return gate
	}
	gate = c.server.activeSessionGate(sessionID)
	if gate != nil {
		c.registerGate(sessionID, gate)
	}
	return gate
}

// deliverToWaitingGate unblocks a session driver blocked in WaitApproval. Nested
// child sessions driven during agent delegation must receive the decision on the
// in-process gate; spawning resumeAfterApproval concurrently would complete the
// child in the database while the parent stays blocked in Dispatch.
func (c *approvalCoordinator) deliverToWaitingGate(
	row store.Approval,
	approved bool,
	args json.RawMessage,
	failErr error,
) error {
	gate := c.sessionApprovalGate(row.SessionID)
	if gate == nil || !gate.isWaiting() {
		return nil
	}
	if err := gate.deliverDecision(approved, args, failErr); err != nil {
		if strings.Contains(err.Error(), "already received") {
			return nil
		}
		return err
	}
	return nil
}

func (c *approvalCoordinator) lockApproval(approvalID string) func() {
	c.mu.Lock()
	mu, ok := c.approvalMu[approvalID]
	if !ok {
		mu = &sync.Mutex{}
		c.approvalMu[approvalID] = mu
	}
	c.mu.Unlock()
	mu.Lock()
	return mu.Unlock
}

func (c *approvalCoordinator) unlockApprovalCleanup(approvalID string) {
	c.mu.Lock()
	delete(c.approvalMu, approvalID)
	c.mu.Unlock()
}

type approvalDecideParams struct {
	ApprovalID                string
	Approved                  bool
	Args                      json.RawMessage
	Comment                   string
	DecidedBy                 string
	ComprehensionAcknowledged bool
}

type approvalDecideResult struct {
	Status            string
	ApprovalsReceived int
	Terminal          bool
}

func (c *approvalCoordinator) OpenApproval(
	ctx context.Context,
	gate *sessionApprovalGate,
	req policy.ApprovalRequest,
) error {
	if gate == nil {
		return errors.New("approval gate is required")
	}
	gate.setPending(req)
	if gate.q == nil || req.SessionID == "" || req.CallID == "" {
		return gate.sendApprovalRequired(req)
	}
	args := req.Args
	if len(args) == 0 {
		args = json.RawMessage("{}")
	}
	var expiresAt *time.Time
	if req.TimeoutAfterMinutes > 0 {
		t := time.Now().Add(time.Duration(req.TimeoutAfterMinutes) * time.Minute)
		expiresAt = &t
	}
	runtimeJSON := mergeApprovalRuntime(req)
	_, err := gate.q.InsertToolInvocationPending(ctx, store.InsertToolInvocationPendingParams{
		CallID:         req.CallID,
		SessionID:      req.SessionID,
		AgentVersionID: gate.agentVersionID,
		Turn:           0,
		Tool:           req.Tool,
		Version:        req.Version,
		Args:           args,
		Status:         model.ToolInvocationAwaitingApproval,
	})
	if err != nil {
		return err
	}
	_, err = gate.q.InsertApproval(ctx, store.InsertApprovalParams{
		ID:                    req.ApprovalID,
		SessionID:             req.SessionID,
		CallID:                req.CallID,
		Status:                model.ApprovalStatusPending,
		Route:                 req.Route,
		Reason:                req.Reason,
		Tool:                  req.Tool,
		Version:               req.Version,
		Args:                  args,
		AuthorityRef:          req.AuthorityRef,
		PolicyName:            req.PolicyName,
		ApprovalsRequired:     req.ApprovalsRequired,
		ComprehensionRequired: req.ComprehensionRequired,
		OnReject:              req.OnReject,
		OnModify:              req.OnModify,
		ExpiresAt:             expiresAt,
		PolicyRuntime:         runtimeJSON,
	})
	if err != nil {
		return err
	}
	if expiresAt != nil {
		c.armApprovalTimeout(req.ApprovalID, *expiresAt)
	}
	c.registerParked(req.SessionID, req.ApprovalID)
	return gate.sendApprovalRequired(req)
}

func (c *approvalCoordinator) Decide(ctx context.Context, p approvalDecideParams) (approvalDecideResult, error) {
	unlock := c.lockApproval(p.ApprovalID)
	defer unlock()
	return c.decideLocked(ctx, p)
}

func (c *approvalCoordinator) decideLocked(ctx context.Context, p approvalDecideParams) (approvalDecideResult, error) {
	q, err := c.server.queries()
	if err != nil {
		return approvalDecideResult{}, err
	}
	row, err := q.GetApproval(ctx, p.ApprovalID)
	if err != nil {
		return approvalDecideResult{}, err
	}
	return c.decideLoaded(ctx, q, p, row)
}

func (c *approvalCoordinator) decideLoaded(
	ctx context.Context,
	q *store.Queries,
	p approvalDecideParams,
	row store.Approval,
) (approvalDecideResult, error) {
	if row.Status != model.ApprovalStatusPending {
		return approvalDecideResult{Status: row.Status, ApprovalsReceived: row.ApprovalsReceived}, nil
	}
	if row.ComprehensionRequired && !p.ComprehensionAcknowledged {
		return approvalDecideResult{}, errors.New("comprehension acknowledgement is required")
	}
	decidedBy := p.DecidedBy
	if decidedBy == "" {
		decidedBy = "operator"
	}

	decision := model.ApprovalVoteDenied
	if p.Approved {
		decision = model.ApprovalVoteApproved
	}
	if _, err := q.InsertApprovalVote(ctx, store.InsertApprovalVoteParams{
		ApprovalID:                p.ApprovalID,
		DecidedBy:                 decidedBy,
		Decision:                  decision,
		Comment:                   p.Comment,
		ComprehensionAcknowledged: p.ComprehensionAcknowledged,
	}); err != nil {
		if errors.Is(err, store.ErrDuplicateApprovalVote) {
			return approvalDecideResult{}, fmt.Errorf("actor %q already decided on this approval", decidedBy)
		}
		return approvalDecideResult{}, err
	}

	if !p.Approved {
		return c.finalizeDecision(ctx, q, row, false, decidedBy, p.Comment, p.Args, row.ApprovalsReceived)
	}

	received, required, err := q.IncrementApprovalsReceived(ctx, p.ApprovalID)
	if err != nil {
		return approvalDecideResult{}, err
	}
	if received < required {
		return approvalDecideResult{
			Status:            model.ApprovalStatusPending,
			ApprovalsReceived: received,
		}, nil
	}
	return c.finalizeDecision(ctx, q, row, true, decidedBy, p.Comment, p.Args, received)
}

func (c *approvalCoordinator) finalizeDecision(
	ctx context.Context,
	q *store.Queries,
	row store.Approval,
	approved bool,
	decidedBy, comment string,
	args json.RawMessage,
	approvalsReceived int,
) (approvalDecideResult, error) {
	c.cancelApprovalTimeout(row.ID)

	if !approved && strings.TrimSpace(row.OnReject) == "fail" {
		msg := strings.TrimSpace(comment)
		if msg == "" {
			msg = "tool call denied"
		}
		failErr := errors.New(msg)
		received := approvalsReceived
		if _, err := q.DecideApproval(ctx, store.DecideApprovalParams{
			ID:                row.ID,
			Status:            model.ApprovalStatusDenied,
			DecidedBy:         decidedBy,
			Comment:           comment,
			ApprovalsReceived: received,
		}); err != nil {
			return approvalDecideResult{}, err
		}
		recordApprovalDecided(ctx, q, row, false, decidedBy, comment)
		if row.CallID != "" {
			_ = q.UpdateToolInvocationStatus(ctx, row.CallID, model.ToolInvocationFailed)
		}
		c.unlockApprovalCleanup(row.ID)
		c.unregisterParked(row.SessionID)
		result := approvalDecideResult{
			Status:            model.ApprovalStatusDenied,
			ApprovalsReceived: received,
			Terminal:          true,
		}
		if err := c.deliverToWaitingGate(row, false, args, failErr); err != nil {
			return result, err
		}
		if c.server != nil && c.server.sessionIsActive(row.SessionID) {
			return result, nil
		}
		if c.server != nil {
			go func() {
				_ = c.server.failDetachedSession(context.Background(), q, row.SessionID, failErr)
			}()
		}
		return result, nil
	}

	status := model.ApprovalStatusDenied
	if approved {
		status = model.ApprovalStatusApproved
	}
	received := approvalsReceived
	if _, err := q.DecideApproval(ctx, store.DecideApprovalParams{
		ID:                row.ID,
		Status:            status,
		DecidedBy:         decidedBy,
		Comment:           comment,
		ApprovalsReceived: received,
	}); err != nil {
		return approvalDecideResult{}, err
	}
	recordApprovalDecided(ctx, q, row, approved, decidedBy, comment)
	if row.CallID != "" {
		invStatus := model.ToolInvocationFailed
		if approved {
			invStatus = model.ToolInvocationPending
		}
		_ = q.UpdateToolInvocationStatus(ctx, row.CallID, invStatus)
	}
	c.unlockApprovalCleanup(row.ID)
	c.unregisterParked(row.SessionID)

	result := approvalDecideResult{
		Status:            status,
		ApprovalsReceived: received,
		Terminal:          true,
	}
	if err := c.deliverToWaitingGate(row, approved, args, nil); err != nil {
		return result, err
	}
	if c.server != nil && c.server.sessionIsActive(row.SessionID) {
		return result, nil
	}
	// Detached sessions with no in-process driver resume asynchronously.
	if c.server != nil && !c.server.sessionIsActive(row.SessionID) {
		resumeRow := row
		go func() {
			if err := c.server.resumeAfterApproval(context.Background(), resumeRow, approved, args, comment); err != nil {
				slog.Error("resume after approval", "session_id", resumeRow.SessionID, "approval_id", resumeRow.ID, "error", err)
			}
		}()
	}
	return result, nil
}

func (c *approvalCoordinator) DecideFromStream(ctx context.Context, gate *sessionApprovalGate, msg *runtimev1.RunSessionInteractiveToolApproval) error {
	if msg == nil {
		return errors.New("tool_approval is required")
	}
	if gate == nil {
		return errors.New("approval gate is required")
	}
	pending := gate.pendingApproval()
	if pending == nil {
		return errors.New("no pending approval")
	}
	approvalID := pending.ApprovalID
	if id := strings.TrimSpace(msg.GetApprovalId()); id != "" && id != approvalID {
		return errors.New("approval_id does not match pending approval")
	}
	_, err := c.Decide(ctx, approvalDecideParams{
		ApprovalID:                approvalID,
		Approved:                  msg.GetApproved(),
		Args:                      msg.GetArgs(),
		Comment:                   msg.GetComment(),
		DecidedBy:                 "interactive",
		ComprehensionAcknowledged: true,
	})
	return err
}
