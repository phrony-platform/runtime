package core

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"

	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
	"github.com/phrony-platform/runtime/internal/executor"
	"github.com/phrony-platform/runtime/internal/model"
	"github.com/phrony-platform/runtime/internal/provider"
	"github.com/phrony-platform/runtime/internal/store"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (s *runtimeServer) RunSessionInteractive(stream runtimev1.Runtime_RunSessionInteractiveServer) error {
	ctx := stream.Context()

	q, err := s.queries()
	if err != nil {
		return err
	}

	first, err := stream.Recv()
	if err != nil {
		if errors.Is(err, io.EOF) {
			return status.Error(codes.InvalidArgument, "first message must be start")
		}
		return err
	}
	start := first.GetStart()
	if start == nil {
		return status.Error(codes.InvalidArgument, "first message must be start")
	}

	sessionID := strings.TrimSpace(start.GetSessionId())
	if sessionID != "" {
		return s.runSessionInteractiveAttach(ctx, stream, q, sessionID)
	}

	ref := start.GetAgentRef()
	if ref == nil || ref.GetNamespace() == "" || ref.GetName() == "" {
		return status.Error(codes.InvalidArgument, "start requires agent_ref or session_id")
	}

	inputJSON, err := normalizeSessionInput(start.GetInput())
	if err != nil {
		return err
	}

	return s.runSessionInteractiveStartWithAgentRef(ctx, stream, q, ref, inputJSON, start.GetResolvedSecrets())
}

// runSessionInteractiveStartWithAgentRef starts a session the same way as RunSession
// (persist + background driver) and attaches this stream as the first subscriber.
func (s *runtimeServer) runSessionInteractiveStartWithAgentRef(
	ctx context.Context,
	stream runtimev1.Runtime_RunSessionInteractiveServer,
	q *store.Queries,
	ref *runtimev1.AgentRef,
	inputJSON json.RawMessage,
	resolvedSecrets map[string][]byte,
) error {
	agentVersionID, err := resolveAgentVersionID(ctx, s.db.DB, ref)
	if err != nil {
		return err
	}

	sessionID, err := s.createRunSession(ctx, agentVersionID, inputJSON, resolvedSecrets)
	if err != nil {
		return err
	}

	if _, err := s.loadSessionVersion(ctx, q, sessionID, agentVersionID); err != nil {
		return s.failInteractiveSession(ctx, q, sessionEventsFromStream(stream), sessionID, err)
	}

	s.startRunSessionBackground(sessionID, agentVersionID, inputJSON)

	return s.runSessionInteractiveAttachDriver(ctx, stream, q, sessionID)
}

func (s *runtimeServer) runSessionInteractiveAttach(
	ctx context.Context,
	stream runtimev1.Runtime_RunSessionInteractiveServer,
	q *store.Queries,
	sessionID string,
) error {
	if s.sessionIsActive(sessionID) {
		return s.runSessionInteractiveAttachDriver(ctx, stream, q, sessionID)
	}

	session, err := q.GetSession(ctx, sessionID)
	if errors.Is(err, sql.ErrNoRows) {
		return status.Errorf(codes.NotFound, "session %s not found", sessionID)
	}
	if err != nil {
		return status.Errorf(codes.Internal, "load session: %v", err)
	}

	if session.Status == model.SessionStatusPending {
		return status.Error(codes.FailedPrecondition, "session is pending execution")
	}

	sessionCtx, sessionCancel := context.WithCancel(ctx)
	defer sessionCancel()
	events := sessionEventsFromStream(stream)

	ver, err := s.loadSessionVersion(sessionCtx, q, sessionID, session.AgentVersionID)
	if err != nil {
		return status.Errorf(codes.Internal, "load agent version: %v", err)
	}

	if session.Status == model.SessionStatusRunning {
		session, err = s.reconcileStaleRunningSession(sessionCtx, q, session, ver)
		if err != nil {
			return err
		}
	}

	history, err := decodeHistory(session.History)
	if err != nil {
		return status.Errorf(codes.Internal, "decode session history: %v", err)
	}
	history = enrichHistoryFromSessionOutput(history, session.Output)
	endedAt := sessionEndedAtForAttach(&session)
	var attachAwaitingState *interactiveSessionState
	var attachAwaitingLastTurn provider.TokenUsage
	if session.Status == model.SessionStatusAwaitingInput {
		var sessionUsage provider.TokenUsage
		attachAwaitingLastTurn, sessionUsage = usageFromSessionOutputJSON(session.Output)
		attachAwaitingState, err = newInteractiveSessionState(sessionCtx, s, sessionID, session.AgentVersionID, ver, session.CreatedAt, events, q)
		if err != nil {
			return status.Errorf(codes.Internal, "build tool dispatch: %v", err)
		}
		attachAwaitingState.history = history
		attachAwaitingState.turnCount = len(history) / 2
		attachAwaitingState.sessionUsage = sessionUsage
		if err := attachAwaitingState.sessionLimitErrorBeforeTurn(); err != nil && isWallClockLimitError(err) {
			endedAt = &session.UpdatedAt
		}
	}
	evidenceSnap, err := s.ensureSessionEvidence(sessionCtx, q, sessionID, ver.Agent)
	if err != nil {
		return status.Errorf(codes.Internal, "record session evidence: %v", err)
	}
	if err := sendSessionStarted(events, sessionID, session.AgentVersionID, ver, history, session.CreatedAt, endedAt, evidenceSnapshotToProto(evidenceSnap)); err != nil {
		return err
	}
	if err := replaySessionEventLog(sessionCtx, q, events, sessionID, pendingApprovalIDForReplay(sessionCtx, q, sessionID)); err != nil {
		return err
	}

	switch session.Status {
	case model.SessionStatusAwaitingTool:
		state, err := newInteractiveSessionState(sessionCtx, s, sessionID, session.AgentVersionID, ver, session.CreatedAt, events, q)
		if err != nil {
			return status.Errorf(codes.Internal, "build tool dispatch: %v", err)
		}
		state.history = history
		state.turnCount = len(history) / 2
		if invocations, err := q.ListUnfinishedInvocationsBySession(sessionCtx, sessionID); err == nil && len(invocations) > 0 {
			if err := s.recoverOutstandingToolInvocations(sessionCtx, q, ver, session, history, invocations, false); err != nil {
				return status.Errorf(codes.Internal, "resume tool dispatch: %v", err)
			}
			session, err = q.GetSession(sessionCtx, sessionID)
			if err != nil {
				return status.Errorf(codes.Internal, "load session: %v", err)
			}
			if session.Status == model.SessionStatusAwaitingApproval {
				if pending, perr := q.GetPendingApprovalBySession(sessionCtx, sessionID); perr == nil {
					req, reqErr := approvalRequestFromStore(sessionCtx, q, pending, sessionID)
					if reqErr == nil {
						_ = events.Send(&runtimev1.RunSessionInteractiveServerMsg{
							Body: &runtimev1.RunSessionInteractiveServerMsg_ApprovalRequired{
								ApprovalRequired: approvalRequiredToProto(req),
							},
						})
						state.approvalGate.setPendingReplay(req)
					}
				}
				if err := sendAwaitingInput(events, stopReasonFromSessionOutput(session.Output), state.turnCount, provider.TokenUsage{}, provider.TokenUsage{}, "awaiting_approval"); err != nil {
					return err
				}
				return s.withActiveSession(sessionID, activeSessionEntry{
					cancel: sessionCancel, approvalGate: state.approvalGate,
				}, func() error {
					return s.runSessionInteractiveLoop(sessionCtx, stream, events, q, sessionID, state, nil, true)
				})
			}
			history, err = decodeHistory(session.History)
			if err != nil {
				return status.Errorf(codes.Internal, "decode session history: %v", err)
			}
			history = enrichHistoryFromSessionOutput(history, session.Output)
			state.history = history
		}
		if err := sendAwaitingInput(events, stopReasonFromSessionOutput(session.Output), state.turnCount, provider.TokenUsage{}, provider.TokenUsage{}, "awaiting_tool"); err != nil {
			return err
		}
		return s.withActiveSession(sessionID, activeSessionEntry{
			cancel: sessionCancel, approvalGate: state.approvalGate,
		}, func() error {
			return s.runSessionInteractiveLoop(sessionCtx, stream, events, q, sessionID, state, nil, true)
		})

	case model.SessionStatusAwaitingApproval:
		state, err := newInteractiveSessionState(sessionCtx, s, sessionID, session.AgentVersionID, ver, session.CreatedAt, events, q)
		if err != nil {
			return status.Errorf(codes.Internal, "build tool dispatch: %v", err)
		}
		state.history = history
		state.turnCount = len(history) / 2
		if pending, err := q.GetPendingApprovalBySession(sessionCtx, sessionID); err == nil {
			req, reqErr := approvalRequestFromStore(sessionCtx, q, pending, sessionID)
			if reqErr == nil {
				_ = events.Send(&runtimev1.RunSessionInteractiveServerMsg{
					Body: &runtimev1.RunSessionInteractiveServerMsg_ApprovalRequired{
						ApprovalRequired: approvalRequiredToProto(req),
					},
				})
				state.approvalGate.setPendingReplay(req)
			}
		}
		if err := sendAwaitingInput(events, stopReasonFromSessionOutput(session.Output), state.turnCount, provider.TokenUsage{}, provider.TokenUsage{}, "awaiting_approval"); err != nil {
			return err
		}
		return s.withActiveSession(sessionID, activeSessionEntry{
			cancel: sessionCancel, approvalGate: state.approvalGate,
		}, func() error {
			return s.runSessionInteractiveLoop(sessionCtx, stream, events, q, sessionID, state, nil, true)
		})

	case model.SessionStatusAwaitingInput:
		blockedReason := ""
		if attachAwaitingState == nil {
			return status.Error(codes.Internal, "attach awaiting_input state missing")
		}
		if err := attachAwaitingState.sessionLimitErrorBeforeTurn(); err != nil {
			if isWallClockLimitError(err) {
				return s.attachWallClockTerminal(sessionCtx, q, stream, sessionID, session, limitErrorMessage(err))
			}
			blockedReason = limitErrorMessage(err)
		}
		return s.withActiveSession(sessionID, activeSessionEntry{
			cancel: sessionCancel, approvalGate: attachAwaitingState.approvalGate,
		}, func() error {
			return s.runSessionInteractiveAttachBlocked(sessionCtx, stream, events, q, sessionID, session, attachAwaitingState, attachAwaitingLastTurn, blockedReason, false)
		})

	case model.SessionStatusCompleted:
		if err := sendInteractiveCompletedFromSession(events, session, history); err != nil {
			return err
		}
		return rejectInteractiveUserMessage(stream)

	case model.SessionStatusFailed:
		errMsg := ""
		if session.Error != nil {
			errMsg = *session.Error
		}
		// Session.Error is stored as text only; match persisted LimitError messages on re-attach.
		// Wall-clock expiry is terminal (the daemon failed it on purpose); other run
		// limits remain resumable so an operator can continue the conversation.
		if executor.IsLimitErrorMessage(errMsg) && !isWallClockLimitMessage(errMsg) {
			lastTurnUsage, sessionUsage := usageFromSessionOutputJSON(session.Output)
			state, err := newInteractiveSessionState(sessionCtx, s, sessionID, session.AgentVersionID, ver, session.CreatedAt, events, q)
			if err != nil {
				return status.Errorf(codes.Internal, "build tool dispatch: %v", err)
			}
			state.history = history
			state.turnCount = len(history) / 2
			state.sessionUsage = sessionUsage
			return s.withActiveSession(sessionID, activeSessionEntry{
				cancel: sessionCancel, approvalGate: state.approvalGate,
			}, func() error {
				return s.runSessionInteractiveAttachBlocked(sessionCtx, stream, events, q, sessionID, session, state, lastTurnUsage, errMsg, true)
			})
		}
		if err := events.Send(&runtimev1.RunSessionInteractiveServerMsg{
			Body: &runtimev1.RunSessionInteractiveServerMsg_Failed{
				Failed: &runtimev1.RunSessionInteractiveFailed{Message: errMsg},
			},
		}); err != nil {
			return err
		}
		return rejectInteractiveUserMessage(stream)

	case model.SessionStatusCancelled:
		if err := sendSessionCancelled(events, session.UpdatedAt); err != nil {
			return err
		}
		return rejectInteractiveUserMessage(stream)

	default:
		return status.Errorf(codes.FailedPrecondition, "session status %q cannot be attached", session.Status)
	}
}

// isWallClockLimitError reports whether err is a max_wall_clock_seconds run limit.
func isWallClockLimitError(err error) bool {
	var lim *executor.LimitError
	if errors.As(err, &lim) {
		return lim.Kind == executor.LimitMaxWallClockSeconds
	}
	return false
}

// isWallClockLimitMessage reports whether a persisted error string is a wall-clock limit.
func isWallClockLimitMessage(msg string) bool {
	return executor.IsLimitErrorMessage(msg) && strings.Contains(msg, string(executor.LimitMaxWallClockSeconds))
}

// attachWallClockTerminal replays a wall-clock-expired session as a read-only
// failure. It also persists the terminal status so ls and attach agree even if
// the background expiry watcher has not run yet (e.g. after a daemon restart).
func (s *runtimeServer) attachWallClockTerminal(
	ctx context.Context,
	q *store.Queries,
	stream runtimev1.Runtime_RunSessionInteractiveServer,
	sessionID string,
	session store.Session,
	message string,
) error {
	if session.Status != model.SessionStatusFailed {
		errText := message
		if _, err := q.UpdateSession(ctx, store.UpdateSessionParams{
			ID:     sessionID,
			Status: model.SessionStatusFailed,
			Error:  &errText,
		}); err != nil {
			return status.Errorf(codes.Internal, "update session: %v", err)
		}
	}
	events := sessionEventsFromStream(stream)
	if err := events.Send(&runtimev1.RunSessionInteractiveServerMsg{
		Body: &runtimev1.RunSessionInteractiveServerMsg_Failed{
			Failed: &runtimev1.RunSessionInteractiveFailed{Message: message},
		},
	}); err != nil {
		return err
	}
	return rejectInteractiveUserMessage(stream)
}

// runSessionInteractiveAttachBlocked keeps the stream open with history visible and input disabled.
// When restoreAwaiting is true (legacy failed sessions that hit a run limit), status is moved back to awaiting_input.
func (s *runtimeServer) runSessionInteractiveAttachBlocked(
	ctx context.Context,
	stream runtimev1.Runtime_RunSessionInteractiveServer,
	events sessionEventSink,
	q *store.Queries,
	sessionID string,
	session store.Session,
	state *interactiveSessionState,
	lastTurnUsage provider.TokenUsage,
	inputBlockedReason string,
	restoreAwaiting bool,
) error {
	state.inputBlockedReason = inputBlockedReason
	if restoreAwaiting {
		cleared := ""
		if _, err := q.UpdateSession(ctx, store.UpdateSessionParams{
			ID:     sessionID,
			Status: model.SessionStatusAwaitingInput,
			Error:  &cleared,
		}); err != nil {
			return status.Errorf(codes.Internal, "update session: %v", err)
		}
	}
	stopReason := stopReasonFromSessionOutput(session.Output)
	if err := sendAwaitingInput(events, stopReason, state.turnCount, lastTurnUsage, state.sessionUsage, state.inputBlockedReason); err != nil {
		return err
	}
	return s.runSessionInteractiveLoop(ctx, stream, events, q, sessionID, state, nil, true)
}

// sessionEndedAtForAttach returns updated_at for terminal attach replays so clients freeze wall-clock display.
func sessionEndedAtForAttach(session *store.Session) *time.Time {
	switch session.Status {
	case model.SessionStatusCompleted, model.SessionStatusCancelled:
		return &session.UpdatedAt
	case model.SessionStatusFailed:
		// Non-wall-clock run limits on failed rows are restored to awaiting_input on attach;
		// keep the wall-clock running until the operator resumes or the session ends.
		if session.Error != nil && executor.IsLimitErrorMessage(*session.Error) && !isWallClockLimitMessage(*session.Error) {
			return nil
		}
		return &session.UpdatedAt
	default:
		return nil
	}
}
