package core

import (
	"strings"

	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
	"github.com/phrony-platform/runtime/internal/executor"
	"github.com/phrony-platform/runtime/internal/manifest"
	"github.com/phrony-platform/runtime/internal/policy"
	"github.com/phrony-platform/runtime/internal/sessionids"
)

func toolCallServerMsg(ev executor.ToolCallEvent, agent *manifest.Agent) *runtimev1.RunSessionInteractiveServerMsg {
	tc := &runtimev1.RunSessionInteractiveToolCall{
		CallId:  ev.CallID,
		Tool:    ev.Tool,
		Version: ev.Version,
		Args:    ev.Args,
	}
	applyDelegationToolCallMetadata(tc, agent, ev)
	return &runtimev1.RunSessionInteractiveServerMsg{
		Body: &runtimev1.RunSessionInteractiveServerMsg_ToolCall{
			ToolCall: tc,
		},
	}
}

func applyDelegationToolCallMetadata(tc *runtimev1.RunSessionInteractiveToolCall, agent *manifest.Agent, ev executor.ToolCallEvent) {
	if agent == nil || tc == nil {
		return
	}
	for i := range agent.Spec.Tools {
		tb := &agent.Spec.Tools[i]
		if tb.DispatchRef() != ev.Tool {
			continue
		}
		if !tb.IsAgent() || tb.Agent == nil {
			return
		}
		tc.AgentDelegation = true
		tc.ChildSessionId = sessionids.ChildFromCallID(ev.CallID)
		tc.DelegationTarget = delegationTargetLabel(*tb, ev.Tool)
		return
	}
}

func delegationTargetLabel(tb manifest.ToolBinding, wireTool string) string {
	if tb.Agent != nil {
		id := tb.Agent.LogicalID()
		if v := strings.TrimSpace(tb.Agent.Version); v != "" {
			if id != "" {
				return id + "@" + v
			}
		}
		if id != "" {
			return id
		}
	}
	return strings.TrimSpace(wireTool)
}

func toolResultServerMsg(ev executor.ToolResultEvent) *runtimev1.RunSessionInteractiveServerMsg {
	return &runtimev1.RunSessionInteractiveServerMsg{
		Body: &runtimev1.RunSessionInteractiveServerMsg_ToolResult{
			ToolResult: &runtimev1.RunSessionInteractiveToolResult{
				CallId:       ev.CallID,
				Payload:      ev.Payload,
				ErrorMessage: ev.ErrorMessage,
			},
		},
	}
}

func approvalRequiredServerMsg(req policy.ApprovalRequest) *runtimev1.RunSessionInteractiveServerMsg {
	return &runtimev1.RunSessionInteractiveServerMsg{
		Body: &runtimev1.RunSessionInteractiveServerMsg_ApprovalRequired{
			ApprovalRequired: approvalRequiredToProto(req),
		},
	}
}

func sendToolCall(events sessionEventSink, ev executor.ToolCallEvent, agent *manifest.Agent) error {
	return events.Send(toolCallServerMsg(ev, agent))
}

func sendToolResult(events sessionEventSink, ev executor.ToolResultEvent) error {
	return events.Send(toolResultServerMsg(ev))
}
