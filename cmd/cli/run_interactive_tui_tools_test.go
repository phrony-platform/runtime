package main

import (
	"strings"
	"testing"

	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
)

func TestRenderToolCallBlock_includesFullDetails(t *testing.T) {
	block := renderToolCallBlock(60, &runtimev1.RunSessionInteractiveToolCall{
		CallId:  "call-abc",
		Tool:    "weather.get-forecast",
		Version: "1.0.0",
		Args:    []byte(`{"city":"Boston"}`),
	})
	for _, want := range []string{"TOOL CALL", "call-abc", "weather.get-forecast@1.0.0", "Boston", "input"} {
		if !strings.Contains(block, want) {
			t.Fatalf("block = %q, want %q", block, want)
		}
	}
}

func TestRenderToolResultBlock_error(t *testing.T) {
	block := renderToolResultBlock(60, &runtimev1.RunSessionInteractiveToolResult{
		CallId:       "call-abc",
		ErrorMessage: "handler timeout",
	})
	if !strings.Contains(block, "TOOL FAILED") || !strings.Contains(block, "handler timeout") {
		t.Fatalf("block = %q, want failure details", block)
	}
}

func TestRenderToolApprovalBlock_includesIDs(t *testing.T) {
	block := renderToolApprovalBlock(60, &runtimev1.RunSessionInteractiveApprovalRequired{
		ApprovalId: "appr-1",
		CallId:     "call-abc",
		Tool:       "danger.op",
		Version:    "2",
		Reason:     "policy",
		Route:      "hitl",
		Args:       []byte(`{"x":1}`),
	})
	for _, want := range []string{"APPROVAL REQUIRED", "appr-1", "call-abc", "danger.op@2", "policy", "hitl"} {
		if !strings.Contains(block, want) {
			t.Fatalf("block = %q, want %q", block, want)
		}
	}
}
