package core

import (
	"context"
	"encoding/json"

	"github.com/phrony-platform/runtime/internal/executor"
	"github.com/phrony-platform/runtime/internal/model"
	"github.com/phrony-platform/runtime/internal/provider"
	"github.com/phrony-platform/runtime/internal/store"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// publishWallClockBlockedAndPersist notifies the client and marks the session failed in the store.
func (s *runtimeServer) publishWallClockBlockedAndPersist(
	ctx context.Context,
	q *store.Queries,
	events sessionEventSink,
	sessionID string,
	state *interactiveSessionState,
	lastStopReason string,
	lastTurnUsage provider.TokenUsage,
	lastOutput json.RawMessage,
) error {
	limitErr := state.sessionWallClockLimitError()
	if limitErr == nil {
		return nil
	}
	state.blockInput(limitErr)
	if err := state.publishInputBlocked(events, lastStopReason, lastTurnUsage); err != nil {
		return err
	}
	return s.persistWallClockTerminal(ctx, q, sessionID, state, limitErrorMessage(limitErr), lastOutput)
}

func (s *runtimeServer) persistWallClockTerminal(
	ctx context.Context,
	q *store.Queries,
	sessionID string,
	state *interactiveSessionState,
	errMsg string,
	output json.RawMessage,
) error {
	if errMsg == "" {
		return nil
	}
	errText := errMsg
	params := store.UpdateSessionParams{
		ID:     sessionID,
		Status: model.SessionStatusFailed,
		Error:  &errText,
	}
	if len(output) > 0 {
		params.Output = output
	}
	if state != nil && len(state.history) > 0 {
		historyJSON, err := encodeHistory(state.history)
		if err != nil {
			return status.Errorf(codes.Internal, "encode session history: %v", err)
		}
		params.History = historyJSON
	}
	if _, err := q.UpdateSession(ctx, params); err != nil {
		return status.Errorf(codes.Internal, "update session: %v", err)
	}
	return nil
}

// reconcileStaleRunningSession repairs a running row left behind when an interactive stream ended
// without updating status (for example after a wall-clock limit while status was running).
func (s *runtimeServer) reconcileStaleRunningSession(
	ctx context.Context,
	q *store.Queries,
	session store.Session,
	ver *executor.Version,
) (store.Session, error) {
	history, err := decodeHistory(session.History)
	if err != nil {
		return session, status.Errorf(codes.Internal, "decode session history: %v", err)
	}
	history = enrichHistoryFromSessionOutput(history, session.Output)
	_, sessionUsage := usageFromSessionOutputJSON(session.Output)
	state := &interactiveSessionState{
		sessionID:        session.ID,
		version:          ver,
		history:          history,
		turnCount:        len(history) / 2,
		sessionUsage:     sessionUsage,
		sessionStartedAt: session.CreatedAt,
	}
	if limitErr := state.sessionWallClockLimitError(); limitErr != nil {
		errText := limitErrorMessage(limitErr)
		if _, err := q.UpdateSession(ctx, store.UpdateSessionParams{
			ID:     session.ID,
			Status: model.SessionStatusFailed,
			Error:  &errText,
		}); err != nil {
			return session, status.Errorf(codes.Internal, "update session: %v", err)
		}
		session.Status = model.SessionStatusFailed
		session.Error = &errText
		return session, nil
	}
	if _, err := q.UpdateSession(ctx, store.UpdateSessionParams{
		ID:     session.ID,
		Status: model.SessionStatusAwaitingInput,
	}); err != nil {
		return session, status.Errorf(codes.Internal, "update session: %v", err)
	}
	session.Status = model.SessionStatusAwaitingInput
	session.Error = nil
	return session, nil
}
