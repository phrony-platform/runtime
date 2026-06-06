package core

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/phrony-platform/runtime/internal/manifest"
	"github.com/phrony-platform/runtime/internal/sessionids"
	"github.com/phrony-platform/runtime/internal/tooldispatch"
)

func TestIsAgentBackedTool(t *testing.T) {
	agent := agentDelegatingAgent()
	if !isAgentBackedTool(agent, "support.billing", "") {
		t.Fatal("support.billing should resolve to an agent (delegation) binding")
	}
	if isAgentBackedTool(agent, "weather.get-forecast", "") {
		t.Fatal("a worker-backed tool must not be reported as agent-backed")
	}
	if isAgentBackedTool(nil, "support.billing", "") {
		t.Fatal("nil agent must not match any tool")
	}
}

func TestChildSessionID_deterministicPerCall(t *testing.T) {
	first := sessionids.ChildFromCallID("call-abc")
	if first != sessionids.ChildFromCallID("call-abc") {
		t.Fatal("ChildFromCallID must be stable for a given call id so recovery can locate the child")
	}
	if first == sessionids.ChildFromCallID("call-xyz") {
		t.Fatal("distinct call ids must map to distinct child sessions")
	}
	if !strings.HasPrefix(first, "run_") {
		t.Fatalf("child session id = %q, want run_ prefix", first)
	}
}

// TestRecoverDispatchedInvocation_agentBackedResumesViaDispatcher proves that a
// dispatched delegation is resumed by re-dispatching through the agent
// dispatcher (which reuses the durable child) rather than polling the worker
// ledger or escalating to HITL as a non_idempotent_write worker tool would.
func TestRecoverDispatchedInvocation_agentBackedResumesViaDispatcher(t *testing.T) {
	disp := &recordingToolDispatcher{}
	srv := &runtimeServer{}
	call := tooldispatch.ToolCall{CallID: "call-1", SessionID: "sess-1", Tool: "support.billing"}

	err := srv.recoverDispatchedInvocation(
		context.Background(), nil, "sess-1", call,
		manifest.SideEffectNonIdempotentWrite, time.Minute, nil, disp, true,
	)
	if err != nil {
		t.Fatalf("recoverDispatchedInvocation: %v", err)
	}
	if disp.callCount() != 1 {
		t.Fatalf("dispatch calls = %d, want 1 (resume via dispatcher)", disp.callCount())
	}
}
