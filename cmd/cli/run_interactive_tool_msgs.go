package main

import (
	"fmt"
	"strings"

	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
)

func formatInteractiveToolCallLine(tc *runtimev1.RunSessionInteractiveToolCall) string {
	if tc == nil {
		return "tool call"
	}
	tool := strings.TrimSpace(tc.GetTool())
	if v := strings.TrimSpace(tc.GetVersion()); v != "" {
		tool = fmt.Sprintf("%s@%s", tool, v)
	}
	if tool == "" {
		tool = "tool"
	}
	args := strings.TrimSpace(string(tc.GetArgs()))
	if args == "" {
		args = "{}"
	}
	if len(args) > 120 {
		args = args[:117] + "..."
	}
	return fmt.Sprintf("calling %s %s", tool, args)
}

func formatInteractiveToolResultLine(tr *runtimev1.RunSessionInteractiveToolResult) string {
	if tr == nil {
		return "tool result"
	}
	if msg := strings.TrimSpace(tr.GetErrorMessage()); msg != "" {
		return fmt.Sprintf("tool failed: %s", msg)
	}
	payload := strings.TrimSpace(string(tr.GetPayload()))
	if payload == "" {
		payload = "{}"
	}
	if len(payload) > 120 {
		payload = payload[:117] + "..."
	}
	return fmt.Sprintf("tool result %s", payload)
}

func formatInteractiveApprovalLine(ar *runtimev1.RunSessionInteractiveApprovalRequired) string {
	if ar == nil {
		return "approval required"
	}
	tool := strings.TrimSpace(ar.GetTool())
	if v := strings.TrimSpace(ar.GetVersion()); v != "" {
		tool = fmt.Sprintf("%s@%s", tool, v)
	}
	reason := strings.TrimSpace(ar.GetReason())
	if reason == "" {
		reason = "policy"
	}
	return fmt.Sprintf("approval required for %s (%s)", tool, reason)
}
