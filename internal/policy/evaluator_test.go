package policy

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/phrony-platform/runtime/internal/manifest"
	"github.com/phrony-platform/runtime/internal/tooldispatch"
)

func compiledAgent(spec manifest.AgentSpec) *manifest.Agent {
	return &manifest.Agent{
		Metadata: manifest.AgentMetadata{
			Annotations: map[string]string{
				manifest.AnnotationPoliciesCompiled: "true",
			},
		},
		Spec: spec,
	}
}

func dispatchEscalatePolicy(trigger, approverRole string) manifest.PolicySpec {
	return manifest.PolicySpec{
		Name: "dispatch-" + trigger,
		Conditions: map[string]any{
			"field": FieldDispatchTrigger,
			"op":    "eq",
			"value": trigger,
		},
		Action: "escalate",
		Runtime: map[string]any{
			"phrony.com/approver_role": approverRole,
		},
	}
}

func TestEvaluateToolCall_allowList(t *testing.T) {
	agent := compiledAgent(manifest.AgentSpec{
		Policies: []manifest.PolicySpec{{
			Name:  "route-only-known-queues",
			Scope: "tool:routing.assign-queue",
			Allow: []string{"motor-standard", "motor-complex"},
		}},
	})
	e := NewEvaluator(agent)
	tc := ToolCallContext{
		ToolRef: "routing.assign-queue",
		Args:    json.RawMessage(`{"queue":"motor-fraud"}`),
	}
	dec, msg := e.EvaluateToolCall(tc)
	if dec != DecisionDeny || msg == "" {
		t.Fatalf("EvaluateToolCall() = (%v, %q), want deny", dec, msg)
	}
	tc.Args = json.RawMessage(`{"queue":"motor-standard"}`)
	dec, msg = e.EvaluateToolCall(tc)
	if dec != DecisionAllow || msg != "" {
		t.Fatalf("EvaluateToolCall() = (%v, %q), want allow", dec, msg)
	}
}

func TestEvaluateToolCall_requireApprovalConditionTree(t *testing.T) {
	agent := compiledAgent(manifest.AgentSpec{
		Tools: []manifest.ToolBinding{{Ref: "routing.assign-queue"}},
		Policies: []manifest.PolicySpec{{
			Name:   "severity-approval",
			Scope:  "tool:routing.assign-queue",
			Action: "require_approval",
			Conditions: map[string]any{
				"field": "severity",
				"op":    "gte",
				"value": 3,
			},
			Runtime: map[string]any{"phrony.com/approver_role": "claims-supervisor-queue"},
		}},
	})
	e := NewEvaluator(agent)
	tc := ToolCallContext{
		ToolRef: "routing.assign-queue",
		Args:    json.RawMessage(`{"severity":4,"queue":"motor-standard"}`),
	}
	dec, _ := e.EvaluateToolCall(tc)
	if dec != DecisionRequireApproval {
		t.Fatalf("decision = %v, want require approval", dec)
	}
	req := e.ApprovalRequestFor("appr-1", "call-1", "sess", tc)
	if req.Route != "claims-supervisor-queue" {
		t.Fatalf("route = %q", req.Route)
	}
}

func TestRouteDispatchFailure_indeterminateNonIdempotent(t *testing.T) {
	e := NewEvaluator(compiledAgent(manifest.AgentSpec{}))
	tc := ToolCallContext{
		ToolRef:         "pay.charge",
		SideEffectClass: manifest.SideEffectNonIdempotentWrite,
	}
	route := e.RouteDispatchFailure(tooldispatch.ErrIndeterminate, tc)
	if route != RouteEscalateHITL {
		t.Fatalf("route = %v, want escalate", route)
	}
}

func TestRouteDispatchFailure_noHandlerPolicy(t *testing.T) {
	agent := compiledAgent(manifest.AgentSpec{
		Policies: []manifest.PolicySpec{
			dispatchEscalatePolicy(TriggerDispatchNoHandler, "ops-queue"),
		},
	})
	e := NewEvaluator(agent)
	tc := ToolCallContext{ToolRef: "weather.get-forecast"}
	route := e.RouteDispatchFailure(tooldispatch.ErrNoHandler, tc)
	if route != RouteEscalateHITL {
		t.Fatalf("route = %v, want escalate", route)
	}
	req := e.DispatchFailureApproval("a1", "c1", "s1", tc, tooldispatch.ErrNoHandler)
	if req.Route != "ops-queue" {
		t.Fatalf("route = %q", req.Route)
	}
}

func TestRouteDispatchFailure_dispatchOutcomeAlias(t *testing.T) {
	agent := compiledAgent(manifest.AgentSpec{
		Policies: []manifest.PolicySpec{{
			Name: "indeterminate-review",
			Conditions: map[string]any{
				"field": FieldDispatchOutcome,
				"op":    "eq",
				"value": "indeterminate",
			},
			Action: "escalate",
			Runtime: map[string]any{
				"phrony.com/approver_role": "on-call",
			},
		}},
	})
	e := NewEvaluator(agent)
	tc := ToolCallContext{ToolRef: "claims.pay"}
	route := e.RouteDispatchFailure(tooldispatch.ErrIndeterminate, tc)
	if route != RouteEscalateHITL {
		t.Fatalf("route = %v, want escalate", route)
	}
}

func TestRouteDispatchFailure_readOnlyIndeterminateToolError(t *testing.T) {
	e := NewEvaluator(compiledAgent(manifest.AgentSpec{}))
	tc := ToolCallContext{
		ToolRef:         "weather.get-forecast",
		SideEffectClass: manifest.SideEffectReadOnly,
	}
	route := e.RouteDispatchFailure(tooldispatch.ErrIndeterminate, tc)
	if route != RouteToolError {
		t.Fatalf("route = %v, want tool error", route)
	}
}

func TestRouteDispatchFailure_wrappedNoHandler(t *testing.T) {
	err := errors.Join(tooldispatch.ErrNoHandler, errors.New("detail"))
	e := NewEvaluator(compiledAgent(manifest.AgentSpec{}))
	route := e.RouteDispatchFailure(err, ToolCallContext{ToolRef: "t"})
	if route != RouteFail {
		t.Fatalf("route = %v, want fail without policy", route)
	}
}

func TestHITLForLimitEscalation_policy(t *testing.T) {
	agent := compiledAgent(manifest.AgentSpec{
		Policies: []manifest.PolicySpec{
			dispatchEscalatePolicy(TriggerLimitEscalate, "limit-ops"),
		},
	})
	e := NewEvaluator(agent)
	route, ok := e.HITLForLimitEscalation()
	if !ok || route != "limit-ops" {
		t.Fatalf("HITLForLimitEscalation() = (%q, %v)", route, ok)
	}
}
