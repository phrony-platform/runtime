package core

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"

	"github.com/google/uuid"
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

	return s.runSessionInteractiveNew(ctx, stream, q, ref, inputJSON)
}

func (s *runtimeServer) runSessionInteractiveNew(
	ctx context.Context,
	stream runtimev1.Runtime_RunSessionInteractiveServer,
	q *store.Queries,
	ref *runtimev1.AgentRef,
	inputJSON json.RawMessage,
) error {
	agentVersionID, err := resolveAgentVersionID(ctx, s.db.DB, ref)
	if err != nil {
		return err
	}

	sessionID := uuid.NewString()
	if _, err := q.InsertSession(ctx, store.InsertSessionParams{
		ID:             sessionID,
		AgentVersionID: agentVersionID,
		Input:          inputJSON,
		Status:         model.SessionStatusRunning,
	}); err != nil {
		return status.Errorf(codes.Internal, "persist session: %v", err)
	}

	if err := s.registerActiveSession(sessionID); err != nil {
		return err
	}
	defer s.unregisterActiveSession(sessionID)

	ver, err := s.loadSessionVersion(ctx, q, agentVersionID)
	if err != nil {
		return s.failInteractiveSession(ctx, q, stream, sessionID, err)
	}

	sessionStartedAt := time.Now()
	if err := sendSessionStarted(stream, sessionID, agentVersionID, ver, nil, sessionStartedAt); err != nil {
		return err
	}

	state := &interactiveSessionState{
		sessionID:        sessionID,
		version:          ver,
		sessionStartedAt: sessionStartedAt,
	}
	return s.runSessionInteractiveLoop(ctx, stream, q, sessionID, state, inputJSON)
}

func (s *runtimeServer) runSessionInteractiveAttach(
	ctx context.Context,
	stream runtimev1.Runtime_RunSessionInteractiveServer,
	q *store.Queries,
	sessionID string,
) error {
	session, err := q.GetSession(ctx, sessionID)
	if errors.Is(err, sql.ErrNoRows) {
		return status.Errorf(codes.NotFound, "session %s not found", sessionID)
	}
	if err != nil {
		return status.Errorf(codes.Internal, "load session: %v", err)
	}

	switch session.Status {
	case model.SessionStatusRunning:
		return status.Error(codes.FailedPrecondition, "session is running on another stream")
	case model.SessionStatusPending:
		return status.Error(codes.FailedPrecondition, "session is pending execution")
	}

	if err := s.registerActiveSession(sessionID); err != nil {
		return err
	}
	defer s.unregisterActiveSession(sessionID)

	ver, err := s.loadSessionVersion(ctx, q, session.AgentVersionID)
	if err != nil {
		return s.failInteractiveSession(ctx, q, stream, sessionID, err)
	}

	history, err := decodeHistory(session.History)
	if err != nil {
		return status.Errorf(codes.Internal, "decode session history: %v", err)
	}
	history = enrichHistoryFromSessionOutput(history, session.Output)
	if err := sendSessionStarted(stream, sessionID, session.AgentVersionID, ver, history, session.CreatedAt); err != nil {
		return err
	}

	switch session.Status {
	case model.SessionStatusAwaitingInput:
		blockedReason := ""
		lastTurnUsage, sessionUsage := usageFromSessionOutputJSON(session.Output)
		state := &interactiveSessionState{
			sessionID:        sessionID,
			version:          ver,
			history:          history,
			turnCount:        len(history) / 2,
			sessionUsage:     sessionUsage,
			sessionStartedAt: session.CreatedAt,
		}
		if err := state.sessionLimitErrorBeforeTurn(); err != nil {
			blockedReason = limitErrorMessage(err)
		}
		return s.runSessionInteractiveAttachBlocked(ctx, stream, q, sessionID, session, state, lastTurnUsage, blockedReason, false)

	case model.SessionStatusCompleted:
		output := session.Output
		if len(output) == 0 {
			output = json.RawMessage("null")
		}
		if err := stream.Send(&runtimev1.RunSessionInteractiveServerMsg{
			Body: &runtimev1.RunSessionInteractiveServerMsg_Completed{
				Completed: &runtimev1.RunSessionInteractiveCompleted{
					StopReason: stopReasonFromSessionOutput(output),
					Output:     output,
				},
			},
		}); err != nil {
			return err
		}
		return rejectInteractiveUserMessage(stream)

	case model.SessionStatusFailed:
		errMsg := ""
		if session.Error != nil {
			errMsg = *session.Error
		}
		// Session.Error is stored as text only; match persisted LimitError messages on re-attach.
		if executor.IsLimitErrorMessage(errMsg) {
			lastTurnUsage, sessionUsage := usageFromSessionOutputJSON(session.Output)
			state := &interactiveSessionState{
				sessionID:        sessionID,
				version:          ver,
				history:          history,
				turnCount:        len(history) / 2,
				sessionUsage:     sessionUsage,
				sessionStartedAt: session.CreatedAt,
			}
			return s.runSessionInteractiveAttachBlocked(ctx, stream, q, sessionID, session, state, lastTurnUsage, errMsg, true)
		}
		if err := stream.Send(&runtimev1.RunSessionInteractiveServerMsg{
			Body: &runtimev1.RunSessionInteractiveServerMsg_Failed{
				Failed: &runtimev1.RunSessionInteractiveFailed{Message: errMsg},
			},
		}); err != nil {
			return err
		}
		return rejectInteractiveUserMessage(stream)

	default:
		return status.Errorf(codes.FailedPrecondition, "session status %q cannot be attached", session.Status)
	}
}

// runSessionInteractiveAttachBlocked keeps the stream open with history visible and input disabled.
// When restoreAwaiting is true (legacy failed sessions that hit a run limit), status is moved back to awaiting_input.
func (s *runtimeServer) runSessionInteractiveAttachBlocked(
	ctx context.Context,
	stream runtimev1.Runtime_RunSessionInteractiveServer,
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
	if err := sendAwaitingInput(stream, stopReason, state.turnCount, lastTurnUsage, state.sessionUsage, state.inputBlockedReason); err != nil {
		return err
	}
	return s.runSessionInteractiveLoop(ctx, stream, q, sessionID, state, nil)
}
