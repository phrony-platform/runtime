package policy

import (
	"encoding/json"
	"testing"

	"github.com/phrony-platform/runtime/internal/manifest"
)

func TestEvaluateConditions_allAndLeaf(t *testing.T) {
	args := json.RawMessage(`{"amount":15000,"currency":"USD"}`)
	conditions := map[string]any{
		"all": []any{
			map[string]any{"field": "amount", "op": "gt", "value": 10000},
			map[string]any{"field": "currency", "op": "eq", "value": "USD"},
		},
	}
	if !evaluateConditions(conditions, args) {
		t.Fatal("expected conditions to match")
	}
	args = json.RawMessage(`{"amount":5000,"currency":"USD"}`)
	if evaluateConditions(conditions, args) {
		t.Fatal("expected conditions not to match")
	}
}

func TestEvaluateConditions_inOperator(t *testing.T) {
	conditions := map[string]any{
		"field": "country",
		"op":    "in",
		"value": []any{"US", "CA"},
	}
	args := json.RawMessage(`{"country":"CA"}`)
	if !evaluateConditions(conditions, args) {
		t.Fatal("expected in match")
	}
}

func TestEvaluateConditions_not(t *testing.T) {
	conditions := map[string]any{
		"not": map[string]any{
			"field": "blocked",
			"op":    "eq",
			"value": true,
		},
	}
	args := json.RawMessage(`{"blocked":false}`)
	if !evaluateConditions(conditions, args) {
		t.Fatal("expected not to pass")
	}
}

func TestEvaluateToolCall_denyBlock(t *testing.T) {
	agent := &manifest.Agent{
		Spec: manifest.AgentSpec{
			Tools: []manifest.ToolBinding{{Ref: "claims.pay"}},
			Policies: []manifest.PolicySpec{{
				Name:   "block-fraud",
				Scope:  "tool:claims.pay",
				Action: "block",
				Reason: "fraud suspected",
				Conditions: map[string]any{
					"field": "risk_score",
					"op":    "gte",
					"value": 90,
				},
			}},
		},
	}
	e := NewEvaluator(agent)
	tc := ToolCallContext{
		ToolRef: "claims.pay",
		Args:    json.RawMessage(`{"risk_score":95}`),
	}
	dec, msg := e.EvaluateToolCall(tc)
	if dec != DecisionDeny || msg != "fraud suspected" {
		t.Fatalf("EvaluateToolCall() = (%v, %q)", dec, msg)
	}
}

func TestEvaluateToolCall_requireApprovalConditionTree(t *testing.T) {
	agent := &manifest.Agent{
		Spec: manifest.AgentSpec{
			Tools: []manifest.ToolBinding{{Ref: "claims.pay"}},
			Policies: []manifest.PolicySpec{{
				Name:   "large-payment",
				Scope:  "tool:claims.pay",
				Action: "require_approval",
				Reason: "over limit",
				Conditions: map[string]any{
					"field": "amount",
					"op":    "gt",
					"value": 10000,
				},
			}},
		},
	}
	e := NewEvaluator(agent)
	tc := ToolCallContext{ToolRef: "claims.pay", Args: json.RawMessage(`{"amount":20000}`)}
	if dec, _ := e.EvaluateToolCall(tc); dec != DecisionRequireApproval {
		t.Fatalf("decision = %v, want require approval", dec)
	}
	tc.Args = json.RawMessage(`{"amount":100}`)
	if dec, _ := e.EvaluateToolCall(tc); dec != DecisionAllow {
		t.Fatalf("decision = %v, want allow", dec)
	}
}
