package core

import (
	"context"
	"errors"
	"io"
	"time"

	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
	"github.com/phrony-platform/runtime/internal/executor"
	"github.com/phrony-platform/runtime/internal/provider"
	"github.com/phrony-platform/runtime/internal/store"
)

type interactiveClientRecv struct {
	msg *runtimev1.RunSessionInteractiveClientMsg
	err error
}

type interactiveTurnOutcome struct {
	stopReason    string
	assistantText string
	turnUsage     provider.TokenUsage
	err           error
}

func startInteractiveClientRecv(ctx context.Context, stream runtimev1.Runtime_RunSessionInteractiveServer) <-chan interactiveClientRecv {
	ch := make(chan interactiveClientRecv, 1)
	go func() {
		for {
			if ctx.Err() != nil {
				return
			}
			msg, err := stream.Recv()
			if errors.Is(err, io.EOF) {
				select {
				case ch <- interactiveClientRecv{err: err}:
				case <-ctx.Done():
				}
				return
			}
			select {
			case ch <- interactiveClientRecv{msg: msg, err: err}:
			case <-ctx.Done():
				return
			}
		}
	}()
	return ch
}

// wallClockTimerChan returns a channel that fires when max_wall_clock_seconds is reached for this session.
func (st *interactiveSessionState) wallClockTimerChan() <-chan time.Time {
	max := st.maxWallClockSeconds()
	if max <= 0 || st.sessionStartedAt.IsZero() {
		return nil
	}
	rem := time.Until(st.sessionStartedAt.Add(time.Duration(max) * time.Second))
	if rem < 0 {
		return time.After(0)
	}
	return time.After(rem)
}

func (st *interactiveSessionState) blockInput(limitErr error) {
	if limitErr == nil {
		return
	}
	st.inputBlockedReason = limitErrorMessage(limitErr)
}

func (st *interactiveSessionState) publishInputBlocked(
	events sessionEventSink,
	stopReason string,
	turnUsage provider.TokenUsage,
) error {
	if st.inputBlockedReason == "" {
		return nil
	}
	return sendAwaitingInput(events, stopReason, st.turnCount, turnUsage, st.sessionUsage, st.inputBlockedReason)
}

func (st *interactiveSessionState) notifyWallClockLimit(
	events sessionEventSink,
	stopReason string,
	turnUsage provider.TokenUsage,
) error {
	if st.inputBlockedReason != "" {
		return nil
	}
	limitErr := st.sessionWallClockLimitError()
	if limitErr == nil {
		return nil
	}
	st.blockInput(limitErr)
	return sendAwaitingInput(events, stopReason, st.turnCount, turnUsage, st.sessionUsage, st.inputBlockedReason)
}

func (s *runtimeServer) handleInteractiveTurnError(
	ctx context.Context,
	q *store.Queries,
	events sessionEventSink,
	state *interactiveSessionState,
	lastStopReason string,
	lastTurnUsage provider.TokenUsage,
	turnErr error,
) (handled bool, err error) {
	if turnErr == nil {
		return false, nil
	}
	if executor.IsLimitError(turnErr) {
		state.blockInput(turnErr)
		if err := state.publishInputBlocked(events, lastStopReason, lastTurnUsage); err != nil {
			return false, err
		}
		return true, nil
	}
	if executor.IsEscalationError(turnErr) {
		if handled, err := s.tryLimitEscalationHITL(ctx, q, events, state, turnErr, lastStopReason, lastTurnUsage); handled || err != nil {
			return handled, err
		}
		state.blockInput(turnErr)
		if err := state.publishInputBlocked(events, lastStopReason, lastTurnUsage); err != nil {
			return false, err
		}
		return true, nil
	}
	if errors.Is(turnErr, context.Canceled) {
		if wc := state.sessionWallClockLimitError(); wc != nil {
			state.blockInput(wc)
			if err := state.publishInputBlocked(events, lastStopReason, lastTurnUsage); err != nil {
				return false, err
			}
			return true, nil
		}
	}
	return false, nil
}

func runInteractiveTurnAsync(
	loopCtx context.Context,
	q *store.Queries,
	state *interactiveSessionState,
	events sessionEventSink,
	pendingInput []byte,
) (cancel context.CancelFunc, done <-chan interactiveTurnOutcome) {
	turnCtx, cancel := context.WithCancel(loopCtx)
	out := make(chan interactiveTurnOutcome, 1)
	go func() {
		stopReason, assistantText, turnUsage, err := state.runTurn(turnCtx, q, events, pendingInput)
		out <- interactiveTurnOutcome{
			stopReason:    stopReason,
			assistantText: assistantText,
			turnUsage:     turnUsage,
			err:           err,
		}
	}()
	return cancel, out
}
