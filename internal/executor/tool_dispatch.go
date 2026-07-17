package executor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/phrony-platform/runtime/internal/manifest"
	"github.com/phrony-platform/runtime/internal/policy"
	"github.com/phrony-platform/runtime/internal/provider"
	"github.com/phrony-platform/runtime/internal/tooldispatch"
	"golang.org/x/sync/errgroup"
)

// ToolCallEvent carries tool lifecycle data for interactive streaming.
type ToolCallEvent struct {
	CallID   string
	Tool     string
	WireName string
	Version  string
	Args     json.RawMessage
}

// ToolResultEvent is emitted after a tool call resolves.
type ToolResultEvent struct {
	CallID       string
	Payload      json.RawMessage
	ErrorMessage string
	// Denied marks a result produced by a policy deny (not a worker dispatch),
	// so the audit log can record it as policy_denied.
	Denied bool
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
	// usages[i] holds tokens a dispatcher attributes to call i (e.g. a nested
	// agent run). They are summed and charged to the tracker after g.Wait so the
	// shared tracker is mutated from a single goroutine.
	usages := make([]int, len(calls))
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
					// Surface the attempt before the denial so the timeline shows both.
					emitToolCall(ch, tdCall, call.Name)
					emitToolDenied(ch, tdCall.CallID, denyMsg)
					return nil
				case policy.DecisionRequireApproval:
					// Surface the tool call before HITL so the event log always has
					// tool.requested ahead of approval/completion (needed for provider history).
					emitToolCall(ch, tdCall, call.Name)
					if err := v.waitForToolApproval(gctx, params, tracker, &tdCall, tc, ch); err != nil {
						var denied *ApprovalDeniedError
						if errors.As(err, &denied) {
							msg := err.Error()
							results[i] = provider.ToolResultBlock(call.ID, msg, true)
							emitToolResult(ch, tdCall.CallID, nil, msg)
							return nil
						}
						return err
					}
					// Already emitted before approval; skip the post-approval emit below.
					dctx, cancelDispatch := dispatchQueueContext(gctx)
					res, err := dispatcher.Dispatch(dctx, tdCall)
					cancelDispatch()
					if err != nil {
						return v.handleDispatchError(gctx, params, tracker, call, tdCall, tc, err, dispatcher, ch, &results[i], &usages[i])
					}
					usages[i] = res.Usage.Total()
					content, isErr := formatToolResult(res)
					results[i] = provider.ToolResultBlock(call.ID, content, isErr)
					emitToolResult(ch, tdCall.CallID, res.Payload, contentIfError(isErr, content))
					return nil
				}
			}

			emitToolCall(ch, tdCall, call.Name)
			dctx, cancelDispatch := dispatchQueueContext(gctx)
			res, err := dispatcher.Dispatch(dctx, tdCall)
			cancelDispatch()
			if err != nil {
				return v.handleDispatchError(gctx, params, tracker, call, tdCall, tc, err, dispatcher, ch, &results[i], &usages[i])
			}
			usages[i] = res.Usage.Total()
			content, isErr := formatToolResult(res)
			results[i] = provider.ToolResultBlock(call.ID, content, isErr)
			emitToolResult(ch, tdCall.CallID, res.Payload, contentIfError(isErr, content))
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}
	total := 0
	for _, u := range usages {
		total += u
	}
	if err := tracker.addUsageTokens(total); err != nil {
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
	return tc
}

func (v *Version) waitForToolApproval(
	ctx context.Context,
	params RunParams,
	tracker *limitTracker,
	tdCall *tooldispatch.ToolCall,
	tc policy.ToolCallContext,
	ch chan<- Event,
) error {
	gate := params.ApprovalGate
	if gate == nil {
		return fmt.Errorf("tool approval is required but no approval gate is configured")
	}
	eval := params.Policies
	initial := eval.Evaluate(tc)
	onModify := ""
	if initial.Approval != nil {
		onModify = strings.TrimSpace(initial.Approval.OnModify)
	}
	for {
		approvalID := params.NewApprovalID()
		if approvalID == "" {
			approvalID = uuid.NewString()
		}
		req := eval.ApprovalRequestFor(approvalID, tdCall.CallID, params.SessionID, tc)
		ch <- Event{
			Type:     EventApprovalRequired,
			Approval: req,
		}
		if tracker != nil {
			tracker.beginHITLWait()
		}
		result, err := gate.WaitApproval(ctx, req)
		if tracker != nil {
			if waitErr := tracker.endHITLWait(); waitErr != nil {
				return waitErr
			}
		}
		if err != nil {
			return err
		}
		if !result.Approved {
			return &ApprovalDeniedError{CallID: tdCall.CallID}
		}
		if len(result.Args) > 0 {
			tc.Args = result.Args
			tdCall.Args = result.Args
		}
		if onModify != "revalidate" {
			return nil
		}
		switch dec, denyMsg := eval.EvaluateToolCall(tc); dec {
		case policy.DecisionDeny:
			return &ApprovalDeniedError{CallID: tdCall.CallID, Message: denyMsg}
		case policy.DecisionRequireApproval:
			continue
		default:
			return nil
		}
	}
}

// IsApprovalDenied reports whether err is an operator denial of a tool call.
func IsApprovalDenied(err error) bool {
	var d *ApprovalDeniedError
	return errors.As(err, &d)
}

// ApprovalDeniedError means the operator denied a pending tool call.
type ApprovalDeniedError struct {
	CallID  string
	Message string
}

func (e *ApprovalDeniedError) Error() string {
	if e == nil {
		return "tool call denied"
	}
	if msg := strings.TrimSpace(e.Message); msg != "" {
		return msg
	}
	if e.CallID == "" {
		return "tool call denied"
	}
	return fmt.Sprintf("tool call %s denied", e.CallID)
}

func (v *Version) handleDispatchError(
	ctx context.Context,
	params RunParams,
	tracker *limitTracker,
	call provider.ToolCall,
	tdCall tooldispatch.ToolCall,
	tc policy.ToolCallContext,
	dispatchErr error,
	dispatcher tooldispatch.Dispatcher,
	ch chan<- Event,
	result *provider.ContentBlock,
	usage *int,
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
		if tracker != nil {
			tracker.beginHITLWait()
		}
		approval, err := params.ApprovalGate.WaitApproval(ctx, req)
		if tracker != nil {
			if waitErr := tracker.endHITLWait(); waitErr != nil {
				return waitErr
			}
		}
		if err != nil {
			return err
		}
		if !approval.Approved {
			msg := "tool dispatch denied after failure: " + dispatchErr.Error()
			*result = provider.ToolResultBlock(call.ID, msg, true)
			emitToolResult(ch, tdCall.CallID, nil, msg)
			return nil
		}
		emitToolCall(ch, tdCall, call.Name)
		dctx, cancelDispatch := dispatchQueueContext(ctx)
		res, redispatchErr := dispatcher.Dispatch(dctx, tdCall)
		cancelDispatch()
		if redispatchErr != nil {
			return redispatchErr
		}
		if usage != nil {
			*usage = res.Usage.Total()
		}
		content, isErr := formatToolResult(res)
		*result = provider.ToolResultBlock(call.ID, content, isErr)
		emitToolResult(ch, tdCall.CallID, res.Payload, contentIfError(isErr, content))
		return nil
	default:
		return dispatchErr
	}
}

func emitToolCall(ch chan<- Event, call tooldispatch.ToolCall, wireName string) {
	if ch == nil {
		return
	}
	ch <- Event{
		Type: EventToolCall,
		ToolCall: ToolCallEvent{
			CallID:   call.CallID,
			Tool:     call.Tool,
			WireName: strings.TrimSpace(wireName),
			Version:  call.Version,
			Args:     call.Args,
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

func emitToolDenied(ch chan<- Event, callID, denyMsg string) {
	if ch == nil {
		return
	}
	ch <- Event{
		Type: EventToolResult,
		ToolResult: ToolResultEvent{
			CallID:       callID,
			ErrorMessage: denyMsg,
			Denied:       true,
		},
	}
}

func contentIfError(isErr bool, content string) string {
	if isErr {
		return content
	}
	return ""
}
