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

func TestDelegatedUserHistoryDuplicatesInput(t *testing.T) {
	args := []byte(`{"task":"Explain quantum entanglement"}`)
	if !delegatedUserHistoryDuplicatesInput(args, "Explain quantum entanglement") {
		t.Fatal("expected plain task to duplicate delegation input")
	}
	if !delegatedUserHistoryDuplicatesInput(args, string(args)) {
		t.Fatal("expected raw JSON to duplicate delegation input")
	}
	if delegatedUserHistoryDuplicatesInput(args, "different task") {
		t.Fatal("expected unrelated history user to be kept")
	}
}

func TestDelegationInputPlainText_extractsTask(t *testing.T) {
	got := delegationInputPlainText([]byte(`{"task":"Explain quantum entanglement"}`))
	if got != "Explain quantum entanglement" {
		t.Fatalf("plain = %q, want task text", got)
	}
	if delegationInputPlainText([]byte(`{"count":3}`)) != "" {
		t.Fatal("expected empty plain text for non-message fields")
	}
}

func TestRenderSubagentSessionInputBlock_showsJSON(t *testing.T) {
	block := renderSubagentSessionInputBlock(60, []byte(`{"count":3}`))
	if !strings.Contains(block, "SESSION INPUT") || !strings.Contains(block, `"count"`) {
		t.Fatalf("block = %q, want session input panel", block)
	}
}

func TestRenderAgentDelegationBlock_includesTargetAndInput(t *testing.T) {
	block := renderAgentDelegationBlock(60, "support.explainer@1.0.0", "call-1", []byte(`{"task":"summarize"}`))
	for _, want := range []string{"AGENT DELEGATION", "support.explainer@1.0.0", "call-1", "summarize", "input"} {
		if !strings.Contains(block, want) {
			t.Fatalf("block = %q, want %q", block, want)
		}
	}
	if strings.Contains(block, "TOOL CALL") {
		t.Fatalf("block = %q, should not use TOOL CALL label", block)
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
