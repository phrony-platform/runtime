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
				Ref: "routing.assign-queue",
				As:  toolName,
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
				Ref: "routing.assign-queue",
				As:  toolName,
			}},
			Policies: []manifest.PolicySpec{{
				Name:   "severity-approval",
				Scope:  "tool:routing.assign-queue",
				Action: "require_approval",
				Conditions: map[string]any{
					"field": "severity",
					"op":    "gte",
					"value": 3,
				},
				Runtime: map[string]any{"phrony.com/approver_role": "supervisor"},
			}},
		},
	}
	agent.Metadata.Annotations = map[string]string{manifest.AnnotationPoliciesCompiled: "true"}
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
			Tools:        []manifest.ToolBinding{{Ref: "weather.get-forecast", As: toolName}},
			Policies: []manifest.PolicySpec{{
				Name: "no-handler",
				Conditions: map[string]any{
					"field": policy.FieldDispatchTrigger,
					"op":    "eq",
					"value": policy.TriggerDispatchNoHandler,
				},
				Action:  "escalate",
				Runtime: map[string]any{"phrony.com/approver_role": "ops"},
			}},
		},
	}
	agent.Metadata.Annotations = map[string]string{manifest.AnnotationPoliciesCompiled: "true"}
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

func TestStreamCompletion_chargesToolResultUsageAgainstTokenBudget(t *testing.T) {
	max := 1000
	toolName := "ask_billing"
	agent := &manifest.Agent{
		Spec: manifest.AgentSpec{
			Instructions: manifest.InstructionsSpec{Text: "System."},
			Model:        manifest.ModelConfig{Provider: provider.IDAnthropic, Name: "claude"},
			Tools:        []manifest.ToolBinding{{Ref: "support.billing", As: toolName}},
			Limits:       &manifest.Limits{MaxTokensPerRun: &max, OnLimit: "halt"},
		},
	}
	call := provider.ToolCall{ID: "c1", Name: toolName, Args: json.RawMessage(`{"task":"x"}`)}
	disp := &tooldispatch.FakeDispatcher{
		DispatchFn: func(context.Context, tooldispatch.ToolCall) (tooldispatch.ToolResult, error) {
			// A delegated agent run reports its aggregate usage; it must count
			// against the parent run's token budget.
			return tooldispatch.ToolResult{
				Payload: json.RawMessage(`{"output":"done"}`),
				Usage:   &tooldispatch.ToolUsage{InputTokens: 1500, OutputTokens: 600},
			}, nil
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
	if !IsLimitError(err) {
		t.Fatalf("StreamCompletion err = %v, want max_tokens_per_run limit error", err)
	}
}

func TestStreamCompletion_priorDelegatedUsageChargesRecoveredLedgerUsage(t *testing.T) {
	max := 1000
	toolName := "ask_billing"
	agent := &manifest.Agent{
		Spec: manifest.AgentSpec{
			Instructions: manifest.InstructionsSpec{Text: "System."},
			Model:        manifest.ModelConfig{Provider: provider.IDAnthropic, Name: "claude"},
			Tools:        []manifest.ToolBinding{{Ref: "support.billing", As: toolName}},
			Limits:       &manifest.Limits{MaxTokensPerRun: &max, OnLimit: "halt"},
		},
	}
	v := NewVersionWithProvider("v", agent, providertest.Sequence(
		providertest.DeltaCompleted().Events,
	))

	ch := make(chan Event, 16)
	err := v.StreamCompletion(context.Background(), RunParams{
		SessionID:           "sess",
		Turn:                1,
		History:             []provider.Message{{Role: provider.RoleUser, Content: "resume"}},
		ResumeFromHistory:   true,
		PriorDelegatedUsage: 1500,
		Dispatcher:          &tooldispatch.FakeDispatcher{},
		Policies:            policy.NewEvaluator(agent),
	}, ch)
	if !IsLimitError(err) {
		t.Fatalf("StreamCompletion err = %v, want max_tokens_per_run limit error", err)
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
