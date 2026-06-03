package core

import (
	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
	"github.com/phrony-platform/runtime/internal/executor"
	"github.com/phrony-platform/runtime/internal/policy"
)

func toolCallServerMsg(ev executor.ToolCallEvent) *runtimev1.RunSessionInteractiveServerMsg {
	return &runtimev1.RunSessionInteractiveServerMsg{
		Body: &runtimev1.RunSessionInteractiveServerMsg_ToolCall{
			ToolCall: &runtimev1.RunSessionInteractiveToolCall{
				CallId:  ev.CallID,
				Tool:    ev.Tool,
				Version: ev.Version,
				Args:    ev.Args,
			},
		},
	}
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

func sendToolCall(events sessionEventSink, ev executor.ToolCallEvent) error {
	return events.Send(toolCallServerMsg(ev))
}

func sendToolResult(events sessionEventSink, ev executor.ToolResultEvent) error {
	return events.Send(toolResultServerMsg(ev))
}
