package executor

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/phrony-platform/runtime/internal/manifest"
	"github.com/phrony-platform/runtime/internal/policy"
	"github.com/phrony-platform/runtime/internal/provider"
	"github.com/phrony-platform/runtime/internal/providertest"
	"github.com/phrony-platform/runtime/internal/tooldispatch"
)

type recordingApprovalGate struct {
	req      policy.ApprovalRequest
	approved bool
}

func (g *recordingApprovalGate) WaitApproval(_ context.Context, req policy.ApprovalRequest) (policy.ApprovalResult, error) {
	g.req = req
	return policy.ApprovalResult{Approved: g.approved}, nil
}

func TestStreamCompletion_policyDenyAllowList(t *testing.T) {
	toolName := "assign_queue"
	agent := &manifest.Agent{
		Spec: manifest.AgentSpec{
			Instructions: manifest.InstructionsSpec{Text: "System."},
			Model:        manifest.ModelConfig{Provider: provider.IDAnthropic, Name: "claude"},
			Tools: []manifest.ToolBinding{{
				Ref:  "routing.assign-queue",
				Name: toolName,
			}},
			Policies: []manifest.PolicySpec{{
				Name:  "route-only-known-queues",
				Scope: "tool:routing.assign-queue",
				Allow: []string{"motor-standard"},
			}},
		},
	}
	call := provider.ToolCall{ID: "c1", Name: toolName, Args: json.RawMessage(`{"queue":"unknown"}`)}
	v := NewVersionWithProvider("v", agent, providertest.Sequence(
		providertest.ToolUseCompleted(call).Events,
		providertest.DeltaCompleted().Events,
	))

	ch := make(chan Event, 16)
	err := v.StreamCompletion(context.Background(), RunParams{
		SessionID:  "sess",
		Turn:       1,
		Input:      json.RawMessage(`{"message":"go"}`),
		Dispatcher: &tooldispatch.FakeDispatcher{},
		Policies:   policy.NewEvaluator(agent),
	}, ch)
	if err != nil {
		t.Fatalf("StreamCompletion: %v", err)
	}
}

func TestStreamCompletion_requireApproval(t *testing.T) {
	toolName := "assign_queue"
	agent := &manifest.Agent{
		Spec: manifest.AgentSpec{
			Instructions: manifest.InstructionsSpec{Text: "System."},
			Model:        manifest.ModelConfig{Provider: provider.IDAnthropic, Name: "claude"},
			Tools: []manifest.ToolBinding{{
				Ref:    "routing.assign-queue",
				Name:   toolName,
				Policy: "require-approval-above-severity-3",
			}},
			HITL: []manifest.HITLTrigger{{
				Trigger:   "tool:routing.assign-queue",
				Condition: "severity >= 3",
				Route:     "supervisor",
			}},
		},
	}
	call := provider.ToolCall{ID: "c1", Name: toolName, Args: json.RawMessage(`{"severity":4,"queue":"motor-standard"}`)}
	dispatched := false
	disp := &tooldispatch.FakeDispatcher{
		DispatchFn: func(context.Context, tooldispatch.ToolCall) (tooldispatch.ToolResult, error) {
			dispatched = true
			return tooldispatch.ToolResult{Payload: json.RawMessage(`{"ok":true}`)}, nil
		},
	}
	gate := &recordingApprovalGate{approved: true}
	v := NewVersionWithProvider("v", agent, providertest.Sequence(
		providertest.ToolUseCompleted(call).Events,
		providertest.DeltaCompleted().Events,
	))

	ch := make(chan Event, 16)
	err := v.StreamCompletion(context.Background(), RunParams{
		SessionID:     "sess",
		Turn:          1,
		Input:         json.RawMessage(`{"message":"go"}`),
		Dispatcher:    disp,
		Policies:      policy.NewEvaluator(agent),
		ApprovalGate:  gate,
		NewApprovalID: func() string { return "appr-1" },
	}, ch)
	if err != nil {
		t.Fatalf("StreamCompletion: %v", err)
	}
	if !dispatched {
		t.Fatal("dispatcher was not called after approval")
	}
	if gate.req.Route != "supervisor" {
		t.Fatalf("approval route = %q", gate.req.Route)
	}
	var sawApproval bool
	for ev := range ch {
		if ev.Type == EventApprovalRequired {
			sawApproval = true
		}
	}
	if !sawApproval {
		t.Fatal("missing approval_required event")
	}
}

func TestStreamCompletion_dispatchNoHandlerEscalate(t *testing.T) {
	toolName := "weather_get_forecast"
	agent := &manifest.Agent{
		Spec: manifest.AgentSpec{
			Instructions: manifest.InstructionsSpec{Text: "System."},
			Model:        manifest.ModelConfig{Provider: provider.IDAnthropic, Name: "claude"},
			Tools:        []manifest.ToolBinding{{Ref: "weather.get-forecast", Name: toolName}},
			HITL: []manifest.HITLTrigger{{
				Trigger: policy.TriggerDispatchNoHandler,
				Route:   "ops",
			}},
		},
	}
	call := provider.ToolCall{ID: "c1", Name: toolName, Args: json.RawMessage(`{}`)}
	disp := &tooldispatch.FakeDispatcher{
		DispatchFn: func(context.Context, tooldispatch.ToolCall) (tooldispatch.ToolResult, error) {
			return tooldispatch.ToolResult{}, tooldispatch.ErrNoHandler
		},
	}
	gate := &recordingApprovalGate{approved: false}
	v := NewVersionWithProvider("v", agent, providertest.Sequence(
		providertest.ToolUseCompleted(call).Events,
		providertest.DeltaCompleted().Events,
	))

	ch := make(chan Event, 16)
	err := v.StreamCompletion(context.Background(), RunParams{
		SessionID:     "sess",
		Turn:          1,
		Input:         json.RawMessage(`{"message":"go"}`),
		Dispatcher:    disp,
		Policies:      policy.NewEvaluator(agent),
		ApprovalGate:  gate,
		NewApprovalID: func() string { return "appr-1" },
	}, ch)
	if err != nil {
		t.Fatalf("StreamCompletion: %v", err)
	}
	if gate.req.Route != "ops" {
		t.Fatalf("route = %q", gate.req.Route)
	}
}

func TestStreamCompletion_dispatchIndeterminateReadOnly(t *testing.T) {
	toolName := "weather_get_forecast"
	agent := testAgentWithTool(toolName)
	call := provider.ToolCall{ID: "c1", Name: toolName, Args: json.RawMessage(`{}`)}
	disp := &tooldispatch.FakeDispatcher{
		DispatchFn: func(context.Context, tooldispatch.ToolCall) (tooldispatch.ToolResult, error) {
			return tooldispatch.ToolResult{}, tooldispatch.ErrIndeterminate
		},
	}
	v := NewVersionWithProvider("v", agent, providertest.Sequence(
		providertest.ToolUseCompleted(call).Events,
		providertest.DeltaCompleted().Events,
	))

	ch := make(chan Event, 16)
	err := v.StreamCompletion(context.Background(), RunParams{
		SessionID:  "sess",
		Turn:       1,
		Input:      json.RawMessage(`{"message":"go"}`),
		Dispatcher: disp,
		Policies:   policy.NewEvaluator(agent),
	}, ch)
	if err != nil {
		t.Fatalf("StreamCompletion: %v", err)
	}
}

func TestApprovalDeniedError(t *testing.T) {
	if !IsApprovalDenied(&ApprovalDeniedError{CallID: "x"}) {
		t.Fatal("expected approval denied")
	}
	if IsApprovalDenied(errors.New("other")) {
		t.Fatal("unexpected approval denied")
	}
}
