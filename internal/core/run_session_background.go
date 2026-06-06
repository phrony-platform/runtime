package core

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
	"github.com/phrony-platform/runtime/internal/executor"
	"github.com/phrony-platform/runtime/internal/model"
	"github.com/phrony-platform/runtime/internal/store"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (s *runtimeServer) startRunSessionBackground(sessionID, agentVersionID string, inputJSON json.RawMessage) {
	if s.startRunSessionBackgroundFn != nil {
		s.startRunSessionBackgroundFn(sessionID, agentVersionID, inputJSON)
		return
	}
	driverCtx, sessionCancel := context.WithCancel(context.Background())
	eventHub := newSessionEventHub()
	inputMux := newSessionInputMux(driverCtx)
	if err := s.registerActiveSession(sessionID, activeSessionEntry{
		cancel:   sessionCancel,
		eventHub: eventHub,
		inputMux: inputMux,
	}); err != nil {
		sessionCancel()
		return
	}
	go func() {
		defer sessionCancel()
		defer inputMux.close()
		defer s.unregisterActiveSession(sessionID)
		s.runSessionBackground(driverCtx, sessionID, agentVersionID, inputJSON, eventHub, inputMux)
	}()
}

func (s *runtimeServer) runSessionBackground(
	ctx context.Context,
	sessionID, agentVersionID string,
	inputJSON json.RawMessage,
	events *sessionEventHub,
	inputMux *sessionInputMux,
) {
	q, err := s.queries()
	if err != nil {
		return
	}

	ver, err := s.loadSessionVersion(ctx, q, sessionID, agentVersionID)
	if err != nil {
		_ = s.failInteractiveSession(ctx, q, events, sessionID, err)
		return
	}
	if _, err := s.ensureSessionEvidence(ctx, q, sessionID, ver.Agent); err != nil {
		slog.Error("session evidence", "session_id", sessionID, "error", err)
	}

	// The daemon eagerly enforces the wall-clock budget so a parked session is
	// transitioned to a terminal state when its time runs out, without waiting
	// for an operator to attach.
	if maxSec, onLimit := versionWallClock(ver); maxSec > 0 {
		s.scheduleWallClockExpiry(sessionID, time.Duration(maxSec)*time.Second, onLimit)
	}

	_ = s.driveSessionToCompletion(ctx, q, sessionID, agentVersionID, ver, events, inputMux, inputJSON, true, rootSessionDepth)
}

// driveSessionToCompletion builds the interactive session state for an
// already-loaded version and drives it through the interactive loop. It is the
// shared core used by the root background runner and by nested child sessions
// dispatched as agent tool calls, so both paths build dispatch, the approval
// gate, and live-text wiring identically and recurse naturally.
//
// waitForUser controls whether the loop parks the session at awaiting_input
// between turns (root sessions await an operator) or runs autonomously to a
// terminal state (child delegation returns its final output to the parent).
// stream supplies client messages (user input, tool approvals); callers driving
// a non-interactive child pass an input source that yields no further messages.
//
// The returned error is the driver loop error (nil on a clean park/completion).
// A non-benign loop error transitions the session to failed before returning,
// matching the behaviour callers previously inlined.
func (s *runtimeServer) driveSessionToCompletion(
	ctx context.Context,
	q *store.Queries,
	sessionID, agentVersionID string,
	ver *executor.Version,
	events sessionEventSink,
	stream runtimev1.Runtime_RunSessionInteractiveServer,
	inputJSON json.RawMessage,
	waitForUser bool,
	depth int,
) error {
	state, err := newInteractiveSessionState(ctx, s, sessionID, agentVersionID, ver, time.Now(), events, q, depth)
	if err != nil {
		return s.failInteractiveSession(ctx, q, events, sessionID, err)
	}
	s.attachActiveSessionGate(sessionID, state.approvalGate)
	state.liveTextSink = func(cumulative string) {
		s.setActiveSessionLiveAssistant(sessionID, cumulative)
	}

	loopErr := s.runSessionInteractiveLoop(ctx, stream, events, q, sessionID, state, inputJSON, waitForUser)
	if loopErr != nil && !isBenignDriverLoopExit(ctx, q, sessionID, loopErr) {
		session, loadErr := q.GetSession(ctx, sessionID)
		if loadErr == nil && !sessionStatusTerminal(session.Status) {
			_ = s.failInteractiveSession(ctx, q, events, sessionID, loopErr)
		}
	}
	return loopErr
}

// isBenignDriverLoopExit reports whether the driver loop ended without needing to
// mark the session failed (explicit cancel or parked awaiting operator input).
func isBenignDriverLoopExit(ctx context.Context, q *store.Queries, sessionID string, loopErr error) bool {
	if loopErr == nil {
		return true
	}
	if errors.Is(loopErr, context.Canceled) {
		wasCancelled, err := sessionWasCancelled(ctx, q, sessionID)
		if err == nil && wasCancelled {
			return true
		}
		session, err := q.GetSession(sessionLookupCtx(ctx), sessionID)
		if err != nil {
			return false
		}
		switch session.Status {
		case model.SessionStatusAwaitingInput,
			model.SessionStatusCompleted,
			model.SessionStatusAwaitingApproval,
			model.SessionStatusAwaitingTool:
			return true
		}
	}
	return false
}

// sessionStatusTerminal reports whether a session status can no longer change.
func sessionStatusTerminal(s string) bool {
	switch s {
	case model.SessionStatusCompleted, model.SessionStatusFailed, model.SessionStatusCancelled:
		return true
	default:
		return false
	}
}

// versionWallClock returns the configured max_wall_clock_seconds and on_limit
// policy for an agent version, or (0, "") when no wall-clock limit applies.
func versionWallClock(ver *executor.Version) (int, string) {
	if ver == nil || ver.Agent == nil {
		return 0, ""
	}
	lim := ver.Agent.Spec.Limits
	if lim == nil || lim.MaxWallClockSeconds == nil {
		return 0, ""
	}
	return *lim.MaxWallClockSeconds, lim.OnLimit
}

// scheduleWallClockExpiry runs a background watcher that fails a session once
// its wall-clock budget (measured from creation) is exhausted, unless the
// session has already reached a terminal state or is being driven by a live
// stream. It uses context.Background so it outlives the detached turn goroutine.
func (s *runtimeServer) scheduleWallClockExpiry(sessionID string, maxWallClock time.Duration, onLimit string) {
	if maxWallClock <= 0 {
		return
	}
	go func() {
		q, err := s.queries()
		if err != nil {
			return
		}
		session, err := q.GetSession(context.Background(), sessionID)
		if err != nil {
			return
		}
		if wait := time.Until(session.CreatedAt.Add(maxWallClock)); wait > 0 {
			timer := time.NewTimer(wait)
			defer timer.Stop()
			<-timer.C
		}
		s.expireWallClockSession(sessionID, onLimit)
	}()
}

// expireWallClockSession marks a parked session terminal due to the wall-clock
// limit. It is a no-op when the session is already terminal or currently being
// driven by an attached stream (that stream enforces the limit itself).
func (s *runtimeServer) expireWallClockSession(sessionID, onLimit string) {
	if s.activeSessions != nil {
		if _, active := s.activeSessions.Load(sessionID); active {
			return
		}
	}
	q, err := s.queries()
	if err != nil {
		return
	}
	session, err := q.GetSession(context.Background(), sessionID)
	if err != nil || sessionStatusTerminal(session.Status) {
		return
	}
	// Wall-clock budget excludes time parked for human approval.
	if session.Status == model.SessionStatusAwaitingApproval {
		return
	}
	msg := (&executor.LimitError{Kind: executor.LimitMaxWallClockSeconds, OnLimit: onLimit}).Error()
	ctx := context.Background()
	_ = appendSessionFailed(ctx, q, sessionID, msg)
	s.finalizeSessionSecrets(ctx, q, sessionID)
}

// persistDetachedSessionAfterTurn parks the session at awaiting_input so an
// operator can attach and continue it later. Conversation state lives in the
// event log and is folded on read.
func (s *runtimeServer) persistDetachedSessionAfterTurn(
	ctx context.Context,
	q *store.Queries,
	sessionID string,
	state *interactiveSessionState,
) error {
	if _, err := q.UpdateSession(ctx, store.UpdateSessionParams{
		ID:     sessionID,
		Status: model.SessionStatusAwaitingInput,
	}); err != nil {
		return status.Errorf(codes.Internal, "update session: %v", err)
	}
	return nil
}

// persistDetachedSessionOutcome parks a detached session at awaiting_input when a
// run limit blocked further input. The blocked reason is recomputed on attach.
func (s *runtimeServer) persistDetachedSessionOutcome(
	ctx context.Context,
	q *store.Queries,
	sessionID string,
	state *interactiveSessionState,
	lastOutput json.RawMessage,
) error {
	if _, err := q.UpdateSession(ctx, store.UpdateSessionParams{
		ID:     sessionID,
		Status: model.SessionStatusAwaitingInput,
	}); err != nil {
		return status.Errorf(codes.Internal, "update session: %v", err)
	}
	return nil
}
