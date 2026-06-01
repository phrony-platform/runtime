package executor

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/phrony-platform/runtime/internal/manifest"
	"github.com/phrony-platform/runtime/internal/policy"
	"github.com/phrony-platform/runtime/internal/provider"
	"github.com/phrony-platform/runtime/internal/providertest"
	"github.com/phrony-platform/runtime/internal/tooldispatch"
)

type revalidateApprovalGate struct{}

func (g *revalidateApprovalGate) WaitApproval(_ context.Context, _ policy.ApprovalRequest) (policy.ApprovalResult, error) {
	return policy.ApprovalResult{
		Approved: true,
		Args:     json.RawMessage(`{"amount":5000}`),
	}, nil
}

func TestStreamCompletion_onModifyRevalidate(t *testing.T) {
	toolName := "approve_payment"
	agent := &manifest.Agent{
		Spec: manifest.AgentSpec{
			Instructions: manifest.InstructionsSpec{Text: "System."},
			Model:        manifest.ModelConfig{Provider: provider.IDAnthropic, Name: "claude"},
			Tools:        []manifest.ToolBinding{{Ref: "claims.pay", As: toolName}},
			Policies: []manifest.PolicySpec{
				{
					Name:     "large",
					Scope:    "tool:claims.pay",
					Action:   "require_approval",
					OnModify: "revalidate",
					Conditions: map[string]any{
						"field": "amount",
						"op":    "gt",
						"value": 10000,
					},
				},
			},
		},
	}
	call := provider.ToolCall{ID: "c1", Name: toolName, Args: json.RawMessage(`{"amount":20000}`)}
	dispatched := false
	disp := &tooldispatch.FakeDispatcher{
		DispatchFn: func(_ context.Context, call tooldispatch.ToolCall) (tooldispatch.ToolResult, error) {
			dispatched = true
			var args map[string]any
			_ = json.Unmarshal(call.Args, &args)
			if args["amount"].(float64) != 5000 {
				t.Fatalf("dispatch args = %v", args)
			}
			return tooldispatch.ToolResult{Payload: json.RawMessage(`{"ok":true}`)}, nil
		},
	}
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
		ApprovalGate:  &revalidateApprovalGate{},
		NewApprovalID: func() string { return "appr-1" },
	}, ch)
	if err != nil {
		t.Fatalf("StreamCompletion: %v", err)
	}
	if !dispatched {
		t.Fatal("expected dispatch after revalidated approval")
	}
}
