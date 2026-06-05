package core

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
	"github.com/phrony-platform/runtime/internal/executor"
	"github.com/phrony-platform/runtime/internal/model"
	"github.com/phrony-platform/runtime/internal/policy"
	"github.com/phrony-platform/runtime/internal/provider"
	"github.com/phrony-platform/runtime/internal/store"
)

func (s *runtimeServer) resumeAfterApproval(
	ctx context.Context,
	row store.Approval,
	approved bool,
	args []byte,
	comment string,
) error {
	gate := s.activeSessionGate(row.SessionID)
	if gate != nil && gate.isWaiting() {
		return nil
	}

	q, err := s.queries()
	if err != nil {
		return err
	}

	session, err := q.GetSession(ctx, row.SessionID)
	if err != nil {
		return err
	}
	if sessionStatusTerminal(session.Status) {
		return nil
	}

	if !approved && strings.TrimSpace(row.OnReject) == "fail" {
		msg := strings.TrimSpace(comment)
		if msg == "" {
			msg = "tool call denied"
		}
		return s.failDetachedSession(ctx, q, row.SessionID, errors.New(msg))
	}

	if strings.TrimSpace(row.CallID) == "" {
		return s.resumeAfterLimitApproval(ctx, q, row, approved, comment, gate)
	}

	ver, err := s.loadSessionVersion(ctx, q, session.ID, session.AgentVersionID)
	if err != nil {
		return err
	}

	history, err := decodeHistory(session.History)
	if err != nil {
		return err
	}
	history = enrichHistoryFromSessionOutput(history, session.Output)

	inv, err := q.GetToolInvocation(ctx, row.CallID)
	if errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err != nil {
		return err
	}

	if approved {
		if len(args) > 0 {
			if err := q.UpdateToolInvocationArgs(ctx, row.CallID, args); err != nil {
				return err
			}
			inv.Args = args
		}
		if gate != nil && gate.events != nil {
			if err := s.recoverOutstandingToolInvocations(ctx, q, ver, session, history, []store.ToolInvocation{inv}, false); err != nil {
				return err
			}
			session, err = q.GetSession(ctx, row.SessionID)
			if err != nil {
				return err
			}
			history, err = decodeHistory(session.History)
			if err != nil {
				return err
			}
			history = enrichHistoryFromSessionOutput(history, session.Output)
			return s.completeApprovalTurnOnStream(ctx, q, gate, session, ver, history)
		}
		return s.recoverOutstandingToolInvocations(ctx, q, ver, session, history, []store.ToolInvocation{inv}, true)
	}

	denyMsg := strings.TrimSpace(comment)
	if denyMsg == "" {
		denyMsg = "tool call denied"
	}
	history = appendRecoveredToolResults(history, []provider.ContentBlock{
		provider.ToolResultBlock(row.CallID, denyMsg, true),
	})
	historyJSON, err := encodeHistory(history)
	if err != nil {
		return err
	}
	if _, err := q.UpdateSession(ctx, store.UpdateSessionParams{
		ID:      row.SessionID,
		Status:  model.SessionStatusRunning,
		History: historyJSON,
	}); err != nil {
		return err
	}
	session.History = historyJSON
	if gate != nil && gate.events != nil {
		return s.completeApprovalTurnOnStream(ctx, q, gate, session, ver, history)
	}
	return s.continueRecoveredTurn(ctx, q, session, ver, history, 0)
}

func (s *runtimeServer) completeApprovalTurnOnStream(
	ctx context.Context,
	q *store.Queries,
	gate *sessionApprovalGate,
	session store.Session,
	ver *executor.Version,
	history []provider.Message,
) error {
	events := gate.events
	if events == nil {
		return errors.New("session event sink is required")
	}

	var st *interactiveSessionState
	if h, ok := gate.hitl.(*interactiveSessionState); ok && h != nil {
		st = h
		st.history = history
		st.turnCount = countCompletedTurns(history)
	} else {
		dispatch, err := s.sessionToolDispatch(ctx, q, session.ID, ver, rootSessionDepth)
		if err != nil {
			return err
		}
		defer closeSessionDispatch(dispatch)
		st = &interactiveSessionState{
			sessionID:        session.ID,
			agentVersionID:   session.AgentVersionID,
			version:          ver,
			history:          history,
			turnCount:        countCompletedTurns(history),
			sessionStartedAt: session.CreatedAt,
			toolDispatch:     dispatch,
			policies:         policy.NewEvaluator(ver.Agent),
			approvalGate:     gate,
		}
		gate.hitl = st
	}

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	ch := make(chan executor.Event, 32)
	runErrCh := make(chan error, 1)
	go func() {
		runErrCh <- st.version.StreamCompletion(runCtx, executor.RunParams{
			SessionID:         st.sessionID,
			Turn:              st.turnCount + 1,
			History:           st.history,
			ResumeFromHistory: true,
			Dispatcher:        st.toolDispatch,
			Policies:          st.policies,
			ApprovalGate:      st.approvalGate,
			NewApprovalID:     newApprovalID,
			BeforeToolDispatch: func(ctx context.Context, messages []provider.Message) error {
				return st.persistBeforeToolDispatch(ctx, q, messages)
			},
		}, ch)
	}()

	var assistantText string
	var turnUsage provider.TokenUsage
	for ev := range ch {
		switch ev.Type {
		case executor.EventTextDelta:
			assistantText += ev.TextDelta
			if err := events.Send(&runtimev1.RunSessionInteractiveServerMsg{
				Body: &runtimev1.RunSessionInteractiveServerMsg_TextDelta{
					TextDelta: &runtimev1.RunSessionInteractiveTextDelta{Delta: ev.TextDelta},
				},
			}); err != nil {
				return err
			}
		case executor.EventToolCall:
			if err := sendToolCall(events, ev.ToolCall); err != nil {
				return err
			}
		case executor.EventToolResult:
			if err := sendToolResult(events, ev.ToolResult); err != nil {
				return err
			}
		case executor.EventCompleted:
			if err := <-runErrCh; err != nil {
				return s.failInteractiveSession(ctx, q, events, st.sessionID, err)
			}
			turnUsage = ev.Usage
			stopReason := ev.StopReason
			turnDuration := time.Duration(0)
			st.history = appendTurnHistory(st.history, "", assistantText, stopReason, turnUsage, turnDuration)
			st.turnCount++
			st.sessionUsage.Add(turnUsage)

			outputJSON, err := marshalSessionOutput(assistantText, stopReason, turnUsage, st.sessionUsage, st.history)
			if err != nil {
				return err
			}
			historyJSON, err := encodeHistory(st.history)
			if err != nil {
				return err
			}
			if _, err := q.UpdateSession(ctx, store.UpdateSessionParams{
				ID:      st.sessionID,
				Status:  model.SessionStatusAwaitingInput,
				Output:  outputJSON,
				History: historyJSON,
			}); err != nil {
				return err
			}
			gate.mu.Lock()
			gate.pendingReq = nil
			gate.mu.Unlock()
			return sendAwaitingInput(events, stopReason, st.turnCount, turnUsage, st.sessionUsage, st.inputBlockedReason)
		case executor.EventFailed, executor.EventEscalation:
			if ev.Err != nil {
				return s.failInteractiveSession(ctx, q, events, st.sessionID, ev.Err)
			}
			return s.failInteractiveSession(ctx, q, events, st.sessionID, errors.New("model completion failed"))
		}
	}
	if err := <-runErrCh; err != nil {
		return s.failInteractiveSession(ctx, q, events, st.sessionID, err)
	}
	return nil
}

func (s *runtimeServer) resumeAfterLimitApproval(
	ctx context.Context,
	q *store.Queries,
	row store.Approval,
	approved bool,
	comment string,
	gate *sessionApprovalGate,
) error {
	if !approved {
		denyMsg := strings.TrimSpace(comment)
		if denyMsg == "" {
			denyMsg = "limit escalation denied"
		}
		if gate != nil {
			if h, ok := gate.hitl.(*interactiveSessionState); ok && h != nil {
				h.blockInput(errors.New(denyMsg))
			}
		}
		if _, err := q.UpdateSession(ctx, store.UpdateSessionParams{
			ID:     row.SessionID,
			Status: model.SessionStatusAwaitingInput,
		}); err != nil {
			return err
		}
		if gate != nil && gate.events != nil {
			if h, ok := gate.hitl.(*interactiveSessionState); ok && h != nil {
				return sendAwaitingInput(gate.events, "", h.turnCount, provider.TokenUsage{}, h.sessionUsage, h.inputBlockedReason)
			}
		}
		return nil
	}
	if gate != nil {
		if h, ok := gate.hitl.(*interactiveSessionState); ok && h != nil {
			h.inputBlockedReason = ""
		}
	}
	if _, err := q.UpdateSession(ctx, store.UpdateSessionParams{
		ID:     row.SessionID,
		Status: model.SessionStatusAwaitingInput,
	}); err != nil {
		return err
	}
	if gate != nil && gate.events != nil {
		if h, ok := gate.hitl.(*interactiveSessionState); ok && h != nil {
			return sendAwaitingInput(gate.events, "", h.turnCount, provider.TokenUsage{}, h.sessionUsage, "")
		}
	}
	return nil
}
