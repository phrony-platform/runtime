package executor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/phrony-platform/runtime/internal/manifest"
	"github.com/phrony-platform/runtime/internal/policy"
	"github.com/phrony-platform/runtime/internal/provider"
	"github.com/phrony-platform/runtime/internal/tooldispatch"
	"golang.org/x/sync/errgroup"
)

// ToolCallEvent carries tool lifecycle data for interactive streaming.
type ToolCallEvent struct {
	CallID  string
	Tool    string
	Version string
	Args    json.RawMessage
}

// ToolResultEvent is emitted after a tool call resolves.
type ToolResultEvent struct {
	CallID       string
	Payload      json.RawMessage
	ErrorMessage string
}

func (v *Version) dispatchToolCalls(
	ctx context.Context,
	params RunParams,
	turn int,
	calls []provider.ToolCall,
	dispatcher tooldispatch.Dispatcher,
	tracker *limitTracker,
	ch chan<- Event,
) ([]provider.ContentBlock, error) {
	deadline := tracker.deadline(ctx)
	results := make([]provider.ContentBlock, len(calls))
	eval := params.Policies

	g, gctx := errgroup.WithContext(ctx)
	for i := range calls {
		i, call := i, calls[i]
		g.Go(func() error {
			if err := tracker.checkWallClock(); err != nil {
				return err
			}

			tdCall, err := buildToolDispatchCall(v.Agent, params.SessionID, v.AgentVersionID, turn, i, call, deadline)
			if err != nil {
				results[i] = provider.ToolResultBlock(call.ID, err.Error(), true)
				emitToolResult(ch, call.ID, nil, err.Error())
				return nil
			}

			tc := toolCallContext(v.Agent, call, tdCall)
			if eval != nil {
				switch dec, denyMsg := eval.EvaluateToolCall(tc); dec {
				case policy.DecisionDeny:
					results[i] = provider.ToolResultBlock(call.ID, denyMsg, true)
					emitToolResult(ch, tdCall.CallID, nil, denyMsg)
					return nil
				case policy.DecisionRequireApproval:
					if err := v.waitForToolApproval(gctx, params, tdCall, tc, ch); err != nil {
						var denied *ApprovalDeniedError
						if errors.As(err, &denied) {
							msg := err.Error()
							results[i] = provider.ToolResultBlock(call.ID, msg, true)
							emitToolResult(ch, tdCall.CallID, nil, msg)
							return nil
						}
						return err
					}
				}
			}

			emitToolCall(ch, tdCall)
			res, err := dispatcher.Dispatch(gctx, tdCall)
			if err != nil {
				return v.handleDispatchError(gctx, params, call, tdCall, tc, err, dispatcher, ch, &results[i])
			}
			content, isErr := formatToolResult(res)
			results[i] = provider.ToolResultBlock(call.ID, content, isErr)
			emitToolResult(ch, tdCall.CallID, res.Payload, contentIfError(isErr, content))
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}
	return results, nil
}

func toolCallContext(agent *manifest.Agent, call provider.ToolCall, td tooldispatch.ToolCall) policy.ToolCallContext {
	tc := policy.ToolCallContext{
		ToolRef:         td.Tool,
		WireName:        call.Name,
		Version:         td.Version,
		Args:            call.Args,
		SideEffectClass: td.SideEffectClass,
	}
	if agent != nil {
		if tb, err := findToolBinding(agent, call.Name); err == nil && tb != nil {
			tc.PolicyName = tb.Policy
		}
	}
	return tc
}

func (v *Version) waitForToolApproval(
	ctx context.Context,
	params RunParams,
	tdCall tooldispatch.ToolCall,
	tc policy.ToolCallContext,
	ch chan<- Event,
) error {
	gate := params.ApprovalGate
	if gate == nil {
		return fmt.Errorf("tool approval is required but no approval gate is configured")
	}
	approvalID := params.NewApprovalID()
	if approvalID == "" {
		approvalID = uuid.NewString()
	}
	req := params.Policies.ApprovalRequestFor(approvalID, tdCall.CallID, params.SessionID, tc)
	ch <- Event{
		Type:     EventApprovalRequired,
		Approval: req,
	}
	approved, err := gate.WaitApproval(ctx, req)
	if err != nil {
		return err
	}
	if !approved {
		return &ApprovalDeniedError{CallID: tdCall.CallID}
	}
	return nil
}

// IsApprovalDenied reports whether err is an operator denial of a tool call.
func IsApprovalDenied(err error) bool {
	var d *ApprovalDeniedError
	return errors.As(err, &d)
}

// ApprovalDeniedError means the operator denied a pending tool call.
type ApprovalDeniedError struct {
	CallID string
}

func (e *ApprovalDeniedError) Error() string {
	if e == nil || e.CallID == "" {
		return "tool call denied"
	}
	return fmt.Sprintf("tool call %s denied", e.CallID)
}

func (v *Version) handleDispatchError(
	ctx context.Context,
	params RunParams,
	call provider.ToolCall,
	tdCall tooldispatch.ToolCall,
	tc policy.ToolCallContext,
	dispatchErr error,
	dispatcher tooldispatch.Dispatcher,
	ch chan<- Event,
	result *provider.ContentBlock,
) error {
	eval := params.Policies
	if eval == nil {
		return dispatchErr
	}
	switch eval.RouteDispatchFailure(dispatchErr, tc) {
	case policy.RouteToolError:
		msg := dispatchErr.Error()
		*result = provider.ToolResultBlock(call.ID, msg, true)
		emitToolResult(ch, tdCall.CallID, nil, msg)
		return nil
	case policy.RouteEscalateHITL:
		if params.ApprovalGate == nil {
			return dispatchErr
		}
		approvalID := params.NewApprovalID()
		if approvalID == "" {
			approvalID = uuid.NewString()
		}
		req := eval.DispatchFailureApproval(approvalID, tdCall.CallID, params.SessionID, tc, dispatchErr)
		ch <- Event{Type: EventApprovalRequired, Approval: req}
		approved, err := params.ApprovalGate.WaitApproval(ctx, req)
		if err != nil {
			return err
		}
		if !approved {
			msg := "tool dispatch denied after failure: " + dispatchErr.Error()
			*result = provider.ToolResultBlock(call.ID, msg, true)
			emitToolResult(ch, tdCall.CallID, nil, msg)
			return nil
		}
		emitToolCall(ch, tdCall)
		res, redispatchErr := dispatcher.Dispatch(ctx, tdCall)
		if redispatchErr != nil {
			return redispatchErr
		}
		content, isErr := formatToolResult(res)
		*result = provider.ToolResultBlock(call.ID, content, isErr)
		emitToolResult(ch, tdCall.CallID, res.Payload, contentIfError(isErr, content))
		return nil
	default:
		return dispatchErr
	}
}

func emitToolCall(ch chan<- Event, call tooldispatch.ToolCall) {
	if ch == nil {
		return
	}
	ch <- Event{
		Type: EventToolCall,
		ToolCall: ToolCallEvent{
			CallID:  call.CallID,
			Tool:    call.Tool,
			Version: call.Version,
			Args:    call.Args,
		},
	}
}

func emitToolResult(ch chan<- Event, callID string, payload json.RawMessage, errMsg string) {
	if ch == nil {
		return
	}
	ch <- Event{
		Type: EventToolResult,
		ToolResult: ToolResultEvent{
			CallID:       callID,
			Payload:      payload,
			ErrorMessage: errMsg,
		},
	}
}

func contentIfError(isErr bool, content string) string {
	if isErr {
		return content
	}
	return ""
}
