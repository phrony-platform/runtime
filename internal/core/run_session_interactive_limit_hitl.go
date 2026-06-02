package core

import (
	"context"

	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
	"github.com/phrony-platform/runtime/internal/executor"
	"github.com/phrony-platform/runtime/internal/model"
	"github.com/phrony-platform/runtime/internal/provider"
	"github.com/phrony-platform/runtime/internal/store"
)

func (s *runtimeServer) tryLimitEscalationHITL(
	ctx context.Context,
	q *store.Queries,
	stream runtimev1.Runtime_RunSessionInteractiveServer,
	state *interactiveSessionState,
	turnErr error,
	lastStopReason string,
	lastTurnUsage provider.TokenUsage,
) (handled bool, err error) {
	if state == nil || state.policies == nil || state.approvalGate == nil {
		return false, nil
	}
	if !executor.IsEscalationError(turnErr) {
		return false, nil
	}
	if _, ok := state.policies.HITLForLimitEscalation(); !ok {
		return false, nil
	}
	approvalID := newApprovalID()
	req, ok := state.policies.LimitEscalationApproval(approvalID, state.sessionID, turnErr)
	if !ok {
		return false, nil
	}
	if err := s.approvalCoord().OpenApproval(ctx, state.approvalGate, req); err != nil {
		return false, err
	}
	if _, err := q.UpdateSession(ctx, store.UpdateSessionParams{
		ID:     state.sessionID,
		Status: model.SessionStatusAwaitingApproval,
	}); err != nil {
		return false, err
	}
	state.inputBlockedReason = ""
	return true, sendAwaitingInput(stream, lastStopReason, state.turnCount, lastTurnUsage, state.sessionUsage, "awaiting_approval")
}
