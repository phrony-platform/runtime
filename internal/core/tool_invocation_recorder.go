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

// ToolInvocationRecorder writes tool_invocations rows for dispatch audit.
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
	_, err := r.Q.InsertToolInvocationPending(ctx, store.InsertToolInvocationPendingParams{
		CallID:         call.CallID,
		SessionID:      call.SessionID,
		AgentVersionID: call.AgentVersionID,
		Turn:           call.Turn,
		Tool:           call.Tool,
		Version:        call.Version,
		Args:           call.Args,
		Status:         status,
	})
	return err
}

func (r *ToolInvocationRecorder) RecordQueued(ctx context.Context, call tooldispatch.ToolCall) error {
	if r == nil || r.Q == nil || call.CallID == "" {
		return nil
	}
	return r.Q.UpdateToolInvocationStatus(ctx, call.CallID, model.ToolInvocationQueued)
}

func (r *ToolInvocationRecorder) RecordDispatched(ctx context.Context, prov tooldispatch.DispatchProvenance) error {
	if r == nil || r.Q == nil {
		return nil
	}
	manifestHash := prov.ManifestContentHash
	if manifestHash == "" && prov.Call.AgentVersionID != "" {
		hash, err := r.Q.GetAgentVersionContentHash(ctx, prov.Call.AgentVersionID)
		if err != nil && !errors.Is(err, context.Canceled) {
			return err
		}
		manifestHash = hash
	}
	_, err := r.Q.InsertToolInvocationDispatched(ctx, store.InsertToolInvocationDispatchedParams{
		CallID:              prov.Call.CallID,
		SessionID:           prov.Call.SessionID,
		AgentVersionID:      prov.Call.AgentVersionID,
		Turn:                prov.Call.Turn,
		Tool:                prov.Call.Tool,
		Version:             prov.Call.Version,
		Args:                prov.Call.Args,
		Status:              model.ToolInvocationDispatched,
		WorkerIdentity:      prov.Worker.WorkloadIdentity,
		ImageDigest:         prov.Worker.ImageDigest,
		DescriptorHash:      prov.DescriptorHash,
		ManifestContentHash: manifestHash,
	})
	return err
}

func (r *ToolInvocationRecorder) RecordCompleted(ctx context.Context, call tooldispatch.ToolCall, res tooldispatch.ToolResult, dispatchErr error) error {
	if r == nil || r.Q == nil || call.CallID == "" {
		return nil
	}
	status := model.ToolInvocationSucceeded
	var result json.RawMessage
	var errCode, errMsg *string

	switch {
	case dispatchErr != nil:
		status = model.ToolInvocationFailed
		code := "dispatch_error"
		msg := dispatchErr.Error()
		errCode = &code
		errMsg = &msg
		if tooldispatch.IsIntegrityError(dispatchErr) {
			if ie, ok := dispatchErr.(*tooldispatch.IntegrityError); ok {
				errCode = strPtr(string(ie.Violation))
			}
		}
	case res.Err != nil:
		status = model.ToolInvocationFailed
		errCode = strPtr(res.Err.Code)
		errMsg = strPtr(res.Err.Message)
	default:
		if len(res.Payload) > 0 {
			result = res.Payload
		} else {
			result = json.RawMessage("{}")
		}
	}

	usageInput, usageOutput, usageEstimated := usageFieldsFromToolResult(res)
	_, err := r.Q.CompleteToolInvocation(ctx, store.CompleteToolInvocationParams{
		CallID:            call.CallID,
		Status:            status,
		Result:            result,
		ErrorCode:         errCode,
		ErrorMessage:      errMsg,
		UsageInputTokens:  usageInput,
		UsageOutputTokens: usageOutput,
		UsageEstimated:    usageEstimated,
	})
	return err
}

func (r *ToolInvocationRecorder) RecordIndeterminate(ctx context.Context, call tooldispatch.ToolCall, reason string) error {
	if r == nil || r.Q == nil || call.CallID == "" {
		return nil
	}
	if reason == "" {
		reason = tooldispatch.ErrIndeterminate.Error()
	}
	return r.Q.MarkToolInvocationIndeterminate(ctx, call.CallID, reason)
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

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
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
