package core

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
	"github.com/phrony-platform/runtime/internal/manifest"
	"github.com/phrony-platform/runtime/internal/mcp"
	"github.com/phrony-platform/runtime/internal/mcp/mcptest"
	"github.com/phrony-platform/runtime/internal/provider"
	"github.com/phrony-platform/runtime/internal/tooldispatch"
)

// These tests prove that an MCP-backed tool is gated by policy and HITL exactly
// like a worker-backed tool: the executor builds the same tooldispatch.ToolCall
// from the binding, policy and approvals run upstream on the logical ref, and
// only allowed/approved calls reach the MCP backend. The MCP dispatcher is
// injected through the routing dispatcher (mirroring sessionToolDispatch) so the
// dispatch entrypoint stays backend-agnostic.

const (
	e2eMCPToolRef    = "demo.echo"
	e2eMCPToolWire   = "echo_tool"
	e2eMCPRemoteTool = "echo"
	e2eMCPServerName = "search"
)

// e2eMCPAgent builds an agent that declares one MCP server and one MCP-backed
// tool binding (ref demo.echo -> remote "echo"). extra can attach policies.
func e2eMCPAgent(extra func(*manifest.Agent)) *manifest.Agent {
	agent := &manifest.Agent{
		Metadata: manifest.AgentMetadata{Namespace: "e2e", Name: "mcp-agent"},
		Spec: manifest.AgentSpec{
			Instructions: manifest.InstructionsSpec{Text: "System."},
			Model:        manifest.ModelConfig{Provider: provider.IDAnthropic, Name: "claude-sonnet-4-5"},
			MCPServers: []manifest.MCPServerSpec{{
				Name: e2eMCPServerName,
				URL:  "https://mcp.example.com/mcp",
			}},
			Tools: []manifest.ToolBinding{{
				Ref:             e2eMCPToolRef,
				As:              e2eMCPToolWire,
				InputSchema:     &manifest.SchemaSpec{Inline: map[string]any{"type": "object"}},
				SideEffectClass: manifest.SideEffectReadOnly,
				MCP:             &manifest.ToolMCPBinding{Server: e2eMCPServerName, Tool: e2eMCPRemoteTool},
			}},
		},
	}
	if extra != nil {
		extra(agent)
	}
	return agent
}

// useMCPBackend wires the harness session dispatcher to a routing dispatcher
// whose primary is a native MCP client pointed at serverURL and whose fallback
// is the shared worker dispatcher, matching the production sessionToolDispatch.
func (h *toolE2EHarness) useMCPBackend(serverURL string) {
	h.t.Helper()
	client := mcp.NewClient(mcp.ServerConfig{Name: e2eMCPServerName, URL: serverURL})
	h.t.Cleanup(func() { _ = client.Close() })
	disp := mcp.NewDispatcher(
		map[string]*mcp.Client{e2eMCPServerName: client},
		map[string]mcp.Binding{e2eMCPToolRef: {Server: e2eMCPServerName, RemoteTool: e2eMCPRemoteTool}},
	)
	h.dispatchOverride = &tooldispatch.RoutingDispatcher{Primary: disp, Fallback: h.srv.toolDispatch}
}

func e2eMCPToolCall(args string) provider.ToolCall {
	return provider.ToolCall{ID: "call_1", Name: e2eMCPToolWire, Args: json.RawMessage(args)}
}

func lastToolResultPayload(sent []*runtimev1.RunSessionInteractiveServerMsg) string {
	var payload string
	for _, msg := range sent {
		if tr := msg.GetToolResult(); tr != nil {
			payload = string(tr.GetPayload())
		}
	}
	return payload
}

func TestMCPToolDispatchE2E_roundTrip(t *testing.T) {
	h := newToolE2EHarness(t, toolE2EConfig{})
	srv := mcptest.NewServer(t, nil)
	h.useMCPBackend(srv.URL)

	stream := &mockInteractiveStream{ctx: context.Background()}
	stub := e2eToolUseThenEndTurn(e2eMCPToolCall(`{"value":"hi"}`))

	stopReason, _, err := h.runTurn(e2eMCPAgent(nil), stub, stream, json.RawMessage(`{"message":"go"}`), nil)
	if err != nil {
		t.Fatalf("runTurn: %v", err)
	}
	if stopReason != provider.StopReasonEndTurn {
		t.Fatalf("stop_reason = %q, want end_turn", stopReason)
	}
	if countStreamToolCalls(stream.sent) != 1 {
		t.Fatalf("tool_call events = %d, want 1", countStreamToolCalls(stream.sent))
	}
	if countStreamToolResults(stream.sent) != 1 {
		t.Fatalf("tool_result events = %d, want 1", countStreamToolResults(stream.sent))
	}
	if got := lastToolResultPayload(stream.sent); got != `{"echo":"hi"}` {
		t.Fatalf("tool_result payload = %s, want {\"echo\":\"hi\"} from MCP backend", got)
	}
}

func TestMCPToolDispatchE2E_policyDenyAndAllow(t *testing.T) {
	allowPolicyAgent := func() *manifest.Agent {
		return e2eMCPAgent(func(a *manifest.Agent) {
			a.Spec.Policies = []manifest.PolicySpec{{
				Name:  "only-known-topics",
				Scope: "tool:" + e2eMCPToolRef,
				Allow: []string{"weather"},
			}}
			a.Metadata.Annotations = map[string]string{manifest.AnnotationPoliciesCompiled: "true"}
		})
	}

	t.Run("deny", func(t *testing.T) {
		h := newToolE2EHarness(t, toolE2EConfig{})
		srv := mcptest.NewServer(t, nil)
		h.useMCPBackend(srv.URL)

		stream := &mockInteractiveStream{ctx: context.Background()}
		stub := e2eToolUseThenEndTurn(e2eMCPToolCall(`{"value":"news"}`))
		stopReason, _, err := h.runTurn(allowPolicyAgent(), stub, stream, json.RawMessage(`{"message":"go"}`), nil)
		if err != nil {
			t.Fatalf("runTurn: %v", err)
		}
		if stopReason != provider.StopReasonEndTurn {
			t.Fatalf("stop_reason = %q", stopReason)
		}
		// The attempt is surfaced as a tool_call before the deny result, but the
		// MCP tool itself is never dispatched.
		if countStreamToolCalls(stream.sent) != 1 {
			t.Fatalf("tool_call events = %d, want 1", countStreamToolCalls(stream.sent))
		}
		if countStreamToolResults(stream.sent) != 1 {
			t.Fatalf("tool_result events = %d, want 1 deny result", countStreamToolResults(stream.sent))
		}
	})

	t.Run("allow", func(t *testing.T) {
		h := newToolE2EHarness(t, toolE2EConfig{})
		srv := mcptest.NewServer(t, nil)
		h.useMCPBackend(srv.URL)

		stream := &mockInteractiveStream{ctx: context.Background()}
		stub := e2eToolUseThenEndTurn(e2eMCPToolCall(`{"value":"weather"}`))
		stopReason, _, err := h.runTurn(allowPolicyAgent(), stub, stream, json.RawMessage(`{"message":"go"}`), nil)
		if err != nil {
			t.Fatalf("runTurn: %v", err)
		}
		if stopReason != provider.StopReasonEndTurn {
			t.Fatalf("stop_reason = %q", stopReason)
		}
		if countStreamToolCalls(stream.sent) != 1 {
			t.Fatal("allowed value should dispatch the MCP tool")
		}
		if got := lastToolResultPayload(stream.sent); got != `{"echo":"weather"}` {
			t.Fatalf("tool_result payload = %s, want MCP echo result", got)
		}
	})
}

func TestMCPToolDispatchE2E_requireApproval(t *testing.T) {
	approvalAgent := func() *manifest.Agent {
		return e2eMCPAgent(func(a *manifest.Agent) {
			a.Spec.Policies = []manifest.PolicySpec{{
				Name:   "sensitive-topic-approval",
				Scope:  "tool:" + e2eMCPToolRef,
				Action: "require_approval",
				Conditions: map[string]any{
					"field": "value",
					"op":    "eq",
					"value": "secret",
				},
				Runtime: map[string]any{"phrony.com/approver_role": "supervisor"},
			}}
			a.Metadata.Annotations = map[string]string{manifest.AnnotationPoliciesCompiled: "true"}
		})
	}

	t.Run("approved dispatches to mcp", func(t *testing.T) {
		h := newToolE2EHarness(t, toolE2EConfig{})
		srv := mcptest.NewServer(t, nil)
		h.useMCPBackend(srv.URL)

		stream := &mockInteractiveStream{ctx: context.Background()}
		gate := &e2eApprovalGate{stream: stream, approve: true}
		stub := e2eToolUseThenEndTurn(e2eMCPToolCall(`{"value":"secret"}`))
		_, _, err := h.runTurn(approvalAgent(), stub, stream, json.RawMessage(`{"message":"go"}`), gate)
		if err != nil {
			t.Fatalf("runTurn: %v", err)
		}
		if gate.lastReq.Route != "supervisor" {
			t.Fatalf("approval route = %q, want supervisor", gate.lastReq.Route)
		}
		if gate.lastReq.Tool != e2eMCPToolRef {
			t.Fatalf("approval tool = %q, want %q", gate.lastReq.Tool, e2eMCPToolRef)
		}
		if countStreamToolCalls(stream.sent) != 1 {
			t.Fatal("expected MCP tool_call after approval")
		}
		if got := lastToolResultPayload(stream.sent); got != `{"echo":"secret"}` {
			t.Fatalf("tool_result payload = %s, want MCP echo result", got)
		}
	})

	t.Run("rejected does not dispatch", func(t *testing.T) {
		h := newToolE2EHarness(t, toolE2EConfig{})
		srv := mcptest.NewServer(t, nil)
		h.useMCPBackend(srv.URL)

		stream := &mockInteractiveStream{ctx: context.Background()}
		gate := &e2eApprovalGate{stream: stream, approve: false}
		stub := e2eToolUseThenEndTurn(e2eMCPToolCall(`{"value":"secret"}`))
		_, _, err := h.runTurn(approvalAgent(), stub, stream, json.RawMessage(`{"message":"go"}`), gate)
		if err != nil {
			t.Fatalf("runTurn: %v", err)
		}
		if countStreamToolCalls(stream.sent) != 0 {
			t.Fatal("rejected approval must not dispatch the MCP tool")
		}
		if countStreamToolResults(stream.sent) != 1 {
			t.Fatalf("tool_result events = %d, want 1 denied result", countStreamToolResults(stream.sent))
		}
		if got := lastToolResultPayload(stream.sent); strings.Contains(got, "echo") {
			t.Fatalf("tool_result payload = %s, want denial not MCP result", got)
		}
	})
}
