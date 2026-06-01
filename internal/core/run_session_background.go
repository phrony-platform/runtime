package core

import (
	"context"
	"encoding/json"
	"io"
	"time"

	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
	"github.com/phrony-platform/runtime/internal/executor"
	"github.com/phrony-platform/runtime/internal/model"
	"github.com/phrony-platform/runtime/internal/policy"
	"github.com/phrony-platform/runtime/internal/store"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// noopInteractiveStream discards outbound events for detached RunSession execution.
type noopInteractiveStream struct {
	ctx context.Context
}

func (n *noopInteractiveStream) Context() context.Context { return n.ctx }

func (n *noopInteractiveStream) Recv() (*runtimev1.RunSessionInteractiveClientMsg, error) {
	return nil, io.EOF
}

func (n *noopInteractiveStream) Send(*runtimev1.RunSessionInteractiveServerMsg) error { return nil }

func (n *noopInteractiveStream) RecvMsg(msg interface{}) error {
	in, err := n.Recv()
	if err != nil {
		return err
	}
	out, ok := msg.(*runtimev1.RunSessionInteractiveClientMsg)
	if !ok {
		return io.EOF
	}
	*out = *in
	return nil
}

func (n *noopInteractiveStream) SendMsg(interface{}) error { return nil }

func (n *noopInteractiveStream) SetHeader(metadata.MD) error  { return nil }
func (n *noopInteractiveStream) SendHeader(metadata.MD) error { return nil }
func (n *noopInteractiveStream) SetTrailer(metadata.MD)       {}

func (s *runtimeServer) startRunSessionBackground(sessionID, agentVersionID string, inputJSON json.RawMessage) {
	if s.startRunSessionBackgroundFn != nil {
		s.startRunSessionBackgroundFn(sessionID, agentVersionID, inputJSON)
		return
	}
	sessionCtx, sessionCancel := context.WithCancel(context.Background())
	if err := s.registerActiveSession(sessionID, sessionCancel); err != nil {
		sessionCancel()
		return
	}
	go func() {
		defer sessionCancel()
		defer s.unregisterActiveSession(sessionID)
		s.runSessionBackground(sessionCtx, sessionID, agentVersionID, inputJSON)
	}()
}

func (s *runtimeServer) runSessionBackground(
	ctx context.Context,
	sessionID, agentVersionID string,
	inputJSON json.RawMessage,
) {
	q, err := s.queries()
	if err != nil {
		return
	}

	stream := &noopInteractiveStream{ctx: ctx}
	ver, err := s.loadSessionVersion(ctx, q, agentVersionID)
	if err != nil {
		_ = s.failInteractiveSession(ctx, q, stream, sessionID, err)
		return
	}

	// The daemon eagerly enforces the wall-clock budget so a parked session is
	// transitioned to a terminal state when its time runs out, without waiting
	// for an operator to attach.
	if maxSec, onLimit := versionWallClock(ver); maxSec > 0 {
		s.scheduleWallClockExpiry(sessionID, time.Duration(maxSec)*time.Second, onLimit)
	}

	state := &interactiveSessionState{
		sessionID:        sessionID,
		agentVersionID:   agentVersionID,
		version:          ver,
		sessionStartedAt: time.Now(),
		toolDispatch:     s.toolDispatch,
		policies:         policy.NewEvaluator(ver.Agent),
	}
	loopErr := s.runSessionInteractiveLoop(ctx, stream, q, sessionID, state, inputJSON, false)
	if loopErr != nil {
		session, loadErr := q.GetSession(ctx, sessionID)
		if loadErr == nil && !sessionStatusTerminal(session.Status) {
			_ = s.failInteractiveSession(ctx, q, stream, sessionID, loopErr)
		}
	}
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
	msg := (&executor.LimitError{Kind: executor.LimitMaxWallClockSeconds, OnLimit: onLimit}).Error()
	errText := msg
	_, _ = q.UpdateSession(context.Background(), store.UpdateSessionParams{
		ID:     sessionID,
		Status: model.SessionStatusFailed,
		Error:  &errText,
	})
}

// persistDetachedSessionAfterTurn stores a completed detached turn and parks the
// session at awaiting_input so an operator can attach and continue it later.
func (s *runtimeServer) persistDetachedSessionAfterTurn(
	ctx context.Context,
	q *store.Queries,
	sessionID string,
	state *interactiveSessionState,
	outputJSON, historyJSON json.RawMessage,
) error {
	if _, err := q.UpdateSession(ctx, store.UpdateSessionParams{
		ID:      sessionID,
		Status:  model.SessionStatusAwaitingInput,
		Output:  outputJSON,
		History: historyJSON,
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
	params := store.UpdateSessionParams{
		ID:     sessionID,
		Status: model.SessionStatusAwaitingInput,
	}
	if len(lastOutput) > 0 {
		params.Output = lastOutput
	}
	if _, err := q.UpdateSession(ctx, params); err != nil {
		return status.Errorf(codes.Internal, "update session: %v", err)
	}
	return nil
}
