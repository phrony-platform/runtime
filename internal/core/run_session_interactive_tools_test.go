package core

import (
	"encoding/json"
	"testing"

	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
	"github.com/phrony-platform/runtime/internal/executor"
	"github.com/phrony-platform/runtime/internal/manifest"
	"github.com/phrony-platform/runtime/internal/sessionids"
)

func TestToolCallServerMsg_agentDelegationMetadata(t *testing.T) {
	agent := agentDelegatingAgent()
	agent.Spec.Tools[0].Agent.Version = "1.0.0"

	ev := executor.ToolCallEvent{
		CallID:  "call-delegate-1",
		Tool:    "support.billing",
		Version: "1.0.0",
		Args:    json.RawMessage(`{"task":"explain refund"}`),
	}
	msg := toolCallServerMsg(ev, agent)
	tc := msg.GetToolCall()
	if tc == nil {
		t.Fatal("expected tool_call body")
	}
	if !tc.GetAgentDelegation() {
		t.Fatal("expected agent_delegation true")
	}
	wantChild := sessionids.ChildFromCallID("call-delegate-1")
	if tc.GetChildSessionId() != wantChild {
		t.Fatalf("child_session_id = %q, want %q", tc.GetChildSessionId(), wantChild)
	}
	if tc.GetDelegationTarget() != "support.billing@1.0.0" {
		t.Fatalf("delegation_target = %q, want support.billing@1.0.0", tc.GetDelegationTarget())
	}
}

func TestToolCallServerMsg_workerToolNoDelegationMetadata(t *testing.T) {
	agent := e2eWeatherAgent(nil)
	ev := executor.ToolCallEvent{
		CallID:  "call-1",
		Tool:    "weather.get-forecast",
		Version: "1.0.0",
		Args:    json.RawMessage(`{"city":"Boston"}`),
	}
	tc := toolCallServerMsg(ev, agent).GetToolCall()
	if tc.GetAgentDelegation() {
		t.Fatal("worker tool should not set agent_delegation")
	}
	if tc.GetChildSessionId() != "" || tc.GetDelegationTarget() != "" {
		t.Fatalf("unexpected delegation fields: child=%q target=%q", tc.GetChildSessionId(), tc.GetDelegationTarget())
	}
}

func TestApplyDelegationToolCallMetadata_matchesDispatchRefNotWireName(t *testing.T) {
	agent := agentDelegatingAgent()
	tc := &runtimev1.RunSessionInteractiveToolCall{CallId: "call-1", Tool: "ask_billing"}
	applyDelegationToolCallMetadata(tc, agent, executor.ToolCallEvent{
		CallID: "call-1",
		Tool:   "ask_billing",
	})
	if tc.GetAgentDelegation() {
		t.Fatal("wire name must not match when dispatch ref is support.billing")
	}

	tc = &runtimev1.RunSessionInteractiveToolCall{CallId: "call-2", Tool: "support.billing"}
	applyDelegationToolCallMetadata(tc, agent, executor.ToolCallEvent{
		CallID: "call-2",
		Tool:   "support.billing",
	})
	if !tc.GetAgentDelegation() {
		t.Fatal("expected delegation metadata when tool matches dispatch ref")
	}
}

func TestToolCallServerMsg_nilAgent(t *testing.T) {
	msg := toolCallServerMsg(executor.ToolCallEvent{
		CallID: "call-1", Tool: "support.billing", Version: "1.0.0",
	}, nil)
	tc := msg.GetToolCall()
	if tc.GetAgentDelegation() || tc.GetChildSessionId() != "" {
		t.Fatalf("nil agent should not enrich delegation: %+v", tc)
	}
}

func TestDelegationTargetLabel_wireFallback(t *testing.T) {
	tb := manifest.ToolBinding{
		Ref: "ask_billing",
		As:  "ask_billing",
		Agent: &manifest.ToolAgentBinding{
			Namespace: "support",
			Name:      "billing",
		},
	}
	if got := delegationTargetLabel(tb, "support.billing"); got != "support.billing" {
		t.Fatalf("delegationTargetLabel = %q, want support.billing", got)
	}
}
