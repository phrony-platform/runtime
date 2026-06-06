package core

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/phrony-platform/runtime/internal/model"
	"github.com/phrony-platform/runtime/internal/store"
	"github.com/phrony-platform/runtime/internal/tooldispatch"
)

// ToolInvocationRecorder writes tool_invocations rows via the event log.
type ToolInvocationRecorder struct {
	Q *store.Queries
}

func NewToolInvocationRecorder(q *store.Queries) *ToolInvocationRecorder {
	if q == nil {
		return nil
	}
	return &ToolInvocationRecorder{Q: q}
}

func (r *ToolInvocationRecorder) RecordPending(ctx context.Context, call tooldispatch.ToolCall, status string) error {
	if r == nil || r.Q == nil || call.CallID == "" {
		return nil
	}
	if status == "" {
		status = model.ToolInvocationPending
	}
	ev := toolEventInput(call.SessionID, EventToolRequested, call, toolRequestedPayload(call, status))
	ev.Tool.Status = status
	_, _, err := appendEventAuto(ctx, r.Q, ev)
	return err
}

func (r *ToolInvocationRecorder) RecordQueued(ctx context.Context, call tooldispatch.ToolCall) error {
	if r == nil || r.Q == nil || call.CallID == "" {
		return nil
	}
	ev := toolEventInput(call.SessionID, EventToolQueued, call, toolRequestedPayload(call, model.ToolInvocationQueued))
	_, _, err := appendEventAuto(ctx, r.Q, ev)
	return err
}

func (r *ToolInvocationRecorder) RecordDispatched(ctx context.Context, prov tooldispatch.DispatchProvenance) error {
	if r == nil || r.Q == nil {
		return nil
	}
	call := prov.Call
	ev := toolEventInput(call.SessionID, EventToolDispatched, call, toolRequestedPayload(call, model.ToolInvocationDispatched))
	ev.Tool.Provenance = &prov
	ev.Actor = ActorWorker
	_, _, err := appendEventAuto(ctx, r.Q, ev)
	return err
}

func (r *ToolInvocationRecorder) RecordCompleted(ctx context.Context, call tooldispatch.ToolCall, res tooldispatch.ToolResult, dispatchErr error) error {
	if r == nil || r.Q == nil || call.CallID == "" {
		return nil
	}
	ev := toolEventInput(call.SessionID, EventToolCompleted, call, toolCompletedPayload(call, res, dispatchErr))
	ev.Tool.Result = res
	ev.Tool.DispatchErr = dispatchErr
	ev.Actor = ActorWorker
	_, _, err := appendEventAuto(ctx, r.Q, ev)
	return err
}

func (r *ToolInvocationRecorder) RecordIndeterminate(ctx context.Context, call tooldispatch.ToolCall, reason string) error {
	if r == nil || r.Q == nil || call.CallID == "" {
		return nil
	}
	if reason == "" {
		reason = tooldispatch.ErrIndeterminate.Error()
	}
	ev := toolEventInput(call.SessionID, EventToolIndeterminate, call, marshalSessionEventJSON(map[string]string{"reason": reason}))
	ev.Tool.IndeterminateReason = reason
	ev.Actor = ActorWorker
	_, _, err := appendEventAuto(ctx, r.Q, ev)
	return err
}

func (r *ToolInvocationRecorder) LookupCompleted(ctx context.Context, callID string) (tooldispatch.ToolResult, bool, error) {
	if r == nil || r.Q == nil || callID == "" {
		return tooldispatch.ToolResult{}, false, nil
	}
	inv, err := r.Q.GetToolInvocation(ctx, callID)
	if errors.Is(err, sql.ErrNoRows) {
		return tooldispatch.ToolResult{}, false, nil
	}
	if err != nil {
		return tooldispatch.ToolResult{}, false, err
	}
	switch inv.Status {
	case model.ToolInvocationSucceeded:
		payload := inv.Result
		if len(payload) == 0 {
			payload = json.RawMessage("{}")
		}
		res := tooldispatch.ToolResult{CallID: callID, Payload: payload}
		if usage := toolUsageFromInvocation(inv); usage != nil {
			res.Usage = usage
		}
		return res, true, nil
	case model.ToolInvocationFailed:
		res := tooldispatch.ToolResult{CallID: callID}
		if inv.ErrorCode != nil || inv.ErrorMessage != nil {
			res.Err = &tooldispatch.ToolError{}
			if inv.ErrorCode != nil {
				res.Err.Code = *inv.ErrorCode
			}
			if inv.ErrorMessage != nil {
				res.Err.Message = *inv.ErrorMessage
			}
		}
		return res, true, nil
	default:
		return tooldispatch.ToolResult{}, false, nil
	}
}

func usageFieldsFromToolResult(res tooldispatch.ToolResult) (input, output int, estimated bool) {
	if res.Usage == nil {
		return 0, 0, false
	}
	return res.Usage.InputTokens, res.Usage.OutputTokens, res.Usage.Estimated
}

func toolUsageFromInvocation(inv store.ToolInvocation) *tooldispatch.ToolUsage {
	if inv.UsageInputTokens == 0 && inv.UsageOutputTokens == 0 {
		return nil
	}
	return &tooldispatch.ToolUsage{
		InputTokens:  inv.UsageInputTokens,
		OutputTokens: inv.UsageOutputTokens,
		Estimated:    inv.UsageEstimated,
	}
}

// sumRecoveredInvocationUsage returns delegated token usage for invocations
// recovered from the durable ledger, re-reading each row after dispatch.
func sumRecoveredInvocationUsage(ctx context.Context, q *store.Queries, invocations []store.ToolInvocation) (int, error) {
	if q == nil {
		return 0, nil
	}
	total := 0
	for _, inv := range invocations {
		stored, err := q.GetToolInvocation(ctx, inv.CallID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				continue
			}
			return 0, err
		}
		if stored.Status != model.ToolInvocationSucceeded {
			continue
		}
		if usage := toolUsageFromInvocation(stored); usage != nil {
			total += usage.Total()
		}
	}
	return total, nil
}

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
