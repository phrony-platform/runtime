package core

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/phrony-platform/runtime/internal/executor"
	"github.com/phrony-platform/runtime/internal/manifest"
	"github.com/phrony-platform/runtime/internal/model"
	"github.com/phrony-platform/runtime/internal/policy"
	"github.com/phrony-platform/runtime/internal/provider"
	"github.com/phrony-platform/runtime/internal/store"
	"github.com/phrony-platform/runtime/internal/tooldispatch"
)

// reconcileSessionsOnStartup re-drives detached sessions, re-arms wall-clock watchers,
// and resumes outstanding tool invocations from the durable ledger.
func (s *runtimeServer) reconcileSessionsOnStartup(ctx context.Context) {
	q, err := s.queries()
	if err != nil {
		slog.Warn("session recovery skipped: database not configured")
		return
	}
	sessions, err := q.ListSessionsForRecovery(ctx)
	if err != nil {
		slog.Error("list sessions for recovery", "error", err)
		return
	}
	for _, session := range sessions {
		if s.sessionIsActive(session.ID) {
			continue
		}
		s.reconcileRecoveredSession(ctx, q, session)
	}
	s.purgeOrphanedTerminalSessionSecrets(ctx)
}

func (s *runtimeServer) reconcileRecoveredSession(ctx context.Context, q *store.Queries, session store.Session) {
	ver, err := s.loadSessionVersion(ctx, q, session.ID, session.AgentVersionID)
	if err != nil {
		slog.Error("recovery: load agent version", "session_id", session.ID, "error", err)
		return
	}
	if maxSec, onLimit := versionWallClock(ver); maxSec > 0 {
		s.scheduleWallClockExpiry(session.ID, time.Duration(maxSec)*time.Second, onLimit)
	}

	switch session.Status {
	case model.SessionStatusAwaitingApproval:
		pending, err := q.GetPendingApprovalBySession(ctx, session.ID)
		if err != nil {
			slog.Warn("recovery: pending approval", "session_id", session.ID, "error", err)
			return
		}
		coord := s.approvalCoord()
		coord.registerParked(session.ID, pending.ID)
		coord.armApprovalTimeoutFromRow(pending)
		return
	case model.SessionStatusPending:
		input := session.Input
		if len(input) == 0 {
			input = json.RawMessage("{}")
		}
		s.startRunSessionBackground(session.ID, session.AgentVersionID, input)
	case model.SessionStatusRunning, model.SessionStatusAwaitingTool:
		go s.recoverDetachedSession(session.ID)
	}
}

func (s *runtimeServer) recoverDetachedSession(sessionID string) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	q, err := s.queries()
	if err != nil {
		return
	}
	session, err := q.GetSession(ctx, sessionID)
	if err != nil {
		return
	}
	if sessionStatusTerminal(session.Status) || s.sessionIsActive(sessionID) {
		return
	}

	ver, err := s.loadSessionVersion(ctx, q, sessionID, session.AgentVersionID)
	if err != nil {
		_ = s.failDetachedSession(ctx, q, sessionID, err)
		return
	}

	invocations, err := q.ListUnfinishedInvocationsBySession(ctx, sessionID)
	if err != nil {
		slog.Error("recovery: list tool invocations", "session_id", sessionID, "error", err)
		return
	}

	history, err := loadProviderContext(ctx, q, sessionID)
	if err != nil {
		slog.Error("recovery: load provider context", "session_id", sessionID, "error", err)
		return
	}

	if len(invocations) > 0 {
		if err := s.recoverOutstandingToolInvocations(ctx, q, ver, session, history, invocations, true); err != nil {
			if errors.Is(err, context.Canceled) {
				return
			}
			slog.Error("recovery: tool invocations", "session_id", sessionID, "error", err)
			return
		}
		session, err = q.GetSession(ctx, sessionID)
		if err != nil || sessionStatusTerminal(session.Status) {
			return
		}
		history, err = loadProviderContext(ctx, q, sessionID)
		if err != nil {
			return
		}
	}

	if session.Status == model.SessionStatusRunning {
		repaired, err := s.reconcileStaleRunningSession(ctx, q, session, ver)
		if err != nil {
			slog.Error("recovery: reconcile running session", "session_id", sessionID, "error", err)
			return
		}
		if repaired.Status != model.SessionStatusRunning {
			return
		}
		session = repaired
	}

	if session.Status == model.SessionStatusAwaitingTool {
		if _, err := q.UpdateSession(ctx, store.UpdateSessionParams{
			ID:     sessionID,
			Status: model.SessionStatusAwaitingInput,
		}); err != nil {
			slog.Error("recovery: park awaiting_tool session", "session_id", sessionID, "error", err)
		}
		return
	}

	if session.Status == model.SessionStatusRunning {
		input := session.Input
		if len(input) == 0 {
			input = json.RawMessage("{}")
		}
		s.startRunSessionBackground(sessionID, session.AgentVersionID, input)
	}
}

func (s *runtimeServer) recoverOutstandingToolInvocations(
	ctx context.Context,
	q *store.Queries,
	ver *executor.Version,
	session store.Session,
	history []provider.Message,
	invocations []store.ToolInvocation,
	resumeCompletion bool,
) error {
	if s.toolDispatch == nil {
		return errors.New("tool dispatch is not configured")
	}
	dispatch, err := s.sessionToolDispatch(ctx, q, session.ID, ver, rootSessionDepth)
	if err != nil {
		return err
	}
	defer closeSessionDispatch(dispatch)
	resyncWindow := time.Minute
	if s.toolRegistry != nil {
		resyncWindow = s.toolRegistry.LeaseTTL()
	}

	policies := policy.NewEvaluator(ver.Agent)
	agentKey := ""
	if ver.Agent != nil {
		agentKey = ver.Agent.Metadata.Namespace + "/" + ver.Agent.Metadata.Name
	}

	for _, inv := range invocations {
		if inv.Status == model.ToolInvocationAwaitingApproval {
			continue
		}
		call := toolInvocationToCall(inv, ver.Agent, agentKey)
		sideClass := call.SideEffectClass

		switch inv.Status {
		case model.ToolInvocationPending, model.ToolInvocationQueued:
			if _, err := dispatch.Dispatch(ctx, call); err != nil {
				if route := policies.RouteDispatchFailure(err, policy.ToolCallContext{
					ToolRef: inv.Tool, Version: inv.Version, SideEffectClass: sideClass,
				}); route == policy.RouteEscalateHITL {
					return s.escalateRecoveredInvocation(ctx, q, session.ID, inv, err)
				}
			}
		case model.ToolInvocationDispatched:
			agentBacked := isAgentBackedTool(ver.Agent, inv.Tool, inv.Version)
			if err := s.recoverDispatchedInvocation(ctx, q, session.ID, call, sideClass, resyncWindow, policies, dispatch, agentBacked); err != nil {
				return err
			}
		}
	}

	updated, err := q.ListUnfinishedInvocationsBySession(ctx, session.ID)
	if err != nil {
		return err
	}
	if len(updated) > 0 {
		if _, err := q.UpdateSession(ctx, store.UpdateSessionParams{
			ID:     session.ID,
			Status: model.SessionStatusAwaitingTool,
		}); err != nil {
			return err
		}
		return nil
	}

	resultBlocks, err := s.buildToolResultBlocksFromLedger(ctx, q, invocations)
	if err != nil {
		return err
	}
	if len(resultBlocks) == 0 {
		return nil
	}

	history = appendRecoveredToolResults(history, resultBlocks)
	if _, err := q.UpdateSession(ctx, store.UpdateSessionParams{
		ID:     session.ID,
		Status: model.SessionStatusRunning,
	}); err != nil {
		return err
	}
	if resumeCompletion {
		delegatedUsage, err := sumRecoveredInvocationUsage(ctx, q, invocations)
		if err != nil {
			return err
		}
		return s.continueRecoveredTurn(ctx, q, session, ver, history, delegatedUsage)
	}
	return nil
}

func (s *runtimeServer) recoverDispatchedInvocation(
	ctx context.Context,
	q *store.Queries,
	sessionID string,
	call tooldispatch.ToolCall,
	sideClass string,
	resyncWindow time.Duration,
	policies *policy.Evaluator,
	dispatch tooldispatch.Dispatcher,
	agentBacked bool,
) error {
	// A delegation's outcome is backed by a durable child session that only
	// advances when this runtime drives it; no external worker can complete it
	// independently. Re-dispatch through the agent dispatcher, which reuses the
	// existing child session idempotently (its id is derived from the call id):
	// a completed child returns its stored output, otherwise it is re-driven to
	// completion. This is safe despite the non_idempotent_write class because the
	// same child is resumed rather than a fresh side-effecting call replayed.
	if agentBacked {
		_, err := dispatch.Dispatch(ctx, call)
		return err
	}

	deadline := time.Now().Add(resyncWindow)
	for time.Now().Before(deadline) {
		stored, err := q.GetToolInvocation(ctx, call.CallID)
		if err == nil && (stored.Status == model.ToolInvocationSucceeded || stored.Status == model.ToolInvocationFailed) {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}

	if manifest.CanRedispatchAfterIndeterminate(sideClass) {
		_, err := dispatch.Dispatch(ctx, call)
		return err
	}

	reason := tooldispatch.ErrIndeterminate.Error()
	if rec := NewToolInvocationRecorder(q); rec != nil {
		_ = rec.RecordIndeterminate(ctx, call, reason)
	}
	inv, _ := q.GetToolInvocation(ctx, call.CallID)
	return s.escalateRecoveredInvocation(ctx, q, sessionID, inv, tooldispatch.ErrIndeterminate)
}

func (s *runtimeServer) escalateRecoveredInvocation(
	ctx context.Context,
	q *store.Queries,
	sessionID string,
	inv store.ToolInvocation,
	cause error,
) error {
	approvalID := uuid.NewString()
	_, _ = q.InsertApproval(ctx, store.InsertApprovalParams{
		ID:        approvalID,
		SessionID: sessionID,
		CallID:    inv.CallID,
		Status:    model.ApprovalStatusPending,
		Reason:    cause.Error(),
	})
	_, err := q.UpdateSession(ctx, store.UpdateSessionParams{
		ID:     sessionID,
		Status: model.SessionStatusAwaitingApproval,
	})
	return err
}

func (s *runtimeServer) continueRecoveredTurn(
	ctx context.Context,
	q *store.Queries,
	session store.Session,
	ver *executor.Version,
	history []provider.Message,
	priorDelegatedUsage int,
) error {
	if s.sessionIsActive(session.ID) {
		return nil
	}
	sessionCtx, sessionCancel := context.WithCancel(context.Background())
	defer sessionCancel()
	dispatch, err := s.sessionToolDispatch(sessionCtx, q, session.ID, ver, rootSessionDepth)
	if err != nil {
		return err
	}
	defer closeSessionDispatch(dispatch)
	events := newSessionEventHub()
	state := &interactiveSessionState{
		sessionID:        session.ID,
		agentVersionID:   session.AgentVersionID,
		version:          ver,
		history:          history,
		turnCount:        countCompletedTurns(history),
		sessionStartedAt: session.CreatedAt,
		toolDispatch:     dispatch,
		policies:         policy.NewEvaluator(ver.Agent),
	}
	state.approvalGate = newSessionApprovalGate(s.approvalCoord(), session.ID, events, q, session.AgentVersionID)
	state.approvalGate.hitl = state
	if err := s.registerActiveSession(session.ID, activeSessionEntry{
		cancel: sessionCancel, approvalGate: state.approvalGate,
	}); err != nil {
		return err
	}
	defer s.unregisterActiveSession(session.ID)

	ch := make(chan executor.Event, 32)
	runErrCh := make(chan error, 1)
	go func() {
		runErrCh <- ver.StreamCompletion(sessionCtx, executor.RunParams{
			SessionID:           session.ID,
			Turn:                state.turnCount + 1,
			History:             history,
			ResumeFromHistory:   true,
			PriorDelegatedUsage: priorDelegatedUsage,
			Dispatcher:          state.toolDispatch,
			Policies:            state.policies,
			ApprovalGate:        state.approvalGate,
			NewApprovalID:       newApprovalID,
			BeforeToolDispatch: func(ctx context.Context, messages []provider.Message) error {
				return state.persistBeforeToolDispatch(ctx, q, messages)
			},
		}, ch)
	}()

	var assistantText string
	for ev := range ch {
		switch ev.Type {
		case executor.EventTextDelta:
			assistantText += ev.TextDelta
		case executor.EventCompleted:
			if err := <-runErrCh; err != nil {
				return s.failDetachedSession(ctx, q, session.ID, err)
			}
			outputJSON, err := marshalSessionOutput(assistantText, ev.StopReason, ev.Usage, ev.Usage, history)
			if err != nil {
				return err
			}
			historyJSON, err := encodeHistory(appendTurnHistory(history, "", assistantText, ev.StopReason, ev.Usage, 0))
			if err != nil {
				return err
			}
			return s.persistDetachedSessionAfterTurn(ctx, q, session.ID, state, outputJSON, historyJSON)
		case executor.EventFailed, executor.EventEscalation:
			if ev.Err != nil {
				return s.failDetachedSession(ctx, q, session.ID, ev.Err)
			}
			return s.failDetachedSession(ctx, q, session.ID, errors.New("model completion failed"))
		}
	}
	if err := <-runErrCh; err != nil {
		return s.failDetachedSession(ctx, q, session.ID, err)
	}
	return nil
}

func (s *runtimeServer) failDetachedSession(ctx context.Context, q *store.Queries, sessionID string, runErr error) error {
	err := appendSessionFailed(ctx, q, sessionID, runErr.Error())
	if err == nil {
		s.finalizeSessionSecrets(ctx, q, sessionID)
	}
	return err
}

func toolInvocationToCall(inv store.ToolInvocation, agent *manifest.Agent, agentKey string) tooldispatch.ToolCall {
	args := inv.Args
	if len(args) == 0 {
		args = json.RawMessage("{}")
	}
	return tooldispatch.ToolCall{
		CallID:          inv.CallID,
		SessionID:       inv.SessionID,
		AgentVersionID:  inv.AgentVersionID,
		AgentKey:        agentKey,
		Turn:            inv.Turn,
		Tool:            inv.Tool,
		Version:         inv.Version,
		Args:            args,
		SideEffectClass: sideEffectClassForTool(agent, inv.Tool, inv.Version),
	}
}

// isAgentBackedTool reports whether the tool ref resolves to a compiled agent
// (delegation) binding, whose recovery resumes a durable child session rather
// than re-dispatching to a worker.
func isAgentBackedTool(agent *manifest.Agent, toolRef, version string) bool {
	if agent == nil {
		return false
	}
	for i := range agent.Spec.Tools {
		tb := &agent.Spec.Tools[i]
		if !tb.IsAgent() {
			continue
		}
		if tb.DispatchRef() != toolRef {
			continue
		}
		if version != "" && tb.Version != "" && tb.Version != version {
			continue
		}
		return true
	}
	return false
}

func sideEffectClassForTool(agent *manifest.Agent, toolRef, version string) string {
	if agent == nil {
		return ""
	}
	for i := range agent.Spec.Tools {
		tb := &agent.Spec.Tools[i]
		if tb.Ref != toolRef {
			continue
		}
		if version != "" && tb.Version != "" && tb.Version != version {
			continue
		}
		return tb.SideEffectClass
	}
	return ""
}

func (s *runtimeServer) buildToolResultBlocksFromLedger(
	ctx context.Context,
	q *store.Queries,
	invocations []store.ToolInvocation,
) ([]provider.ContentBlock, error) {
	var blocks []provider.ContentBlock
	for _, inv := range invocations {
		stored, err := q.GetToolInvocation(ctx, inv.CallID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				continue
			}
			return nil, err
		}
		if stored.Status != model.ToolInvocationSucceeded && stored.Status != model.ToolInvocationFailed {
			continue
		}
		content, isErr := ledgerInvocationContent(stored)
		blocks = append(blocks, provider.ToolResultBlock(inv.CallID, content, isErr))
	}
	return blocks, nil
}

func ledgerInvocationContent(inv store.ToolInvocation) (string, bool) {
	if inv.Status == model.ToolInvocationFailed {
		msg := "tool call failed"
		if inv.ErrorMessage != nil {
			msg = *inv.ErrorMessage
		}
		return msg, true
	}
	if len(inv.Result) == 0 {
		return "{}", false
	}
	return string(inv.Result), false
}

func appendRecoveredToolResults(history []provider.Message, blocks []provider.ContentBlock) []provider.Message {
	if len(blocks) == 0 {
		return history
	}
	return append(history, provider.Message{
		Role:   provider.RoleUser,
		Blocks: blocks,
	})
}

func countCompletedTurns(history []provider.Message) int {
	n := 0
	for _, m := range history {
		if m.Role == provider.RoleAssistant {
			n++
		}
	}
	return n
}
