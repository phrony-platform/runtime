package policy

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/phrony-platform/runtime/internal/manifest"
	"github.com/phrony-platform/runtime/internal/tooldispatch"
)

func TestEvaluateToolCall_allowList(t *testing.T) {
	agent := &manifest.Agent{
		Spec: manifest.AgentSpec{
			Policies: []manifest.PolicySpec{{
				Name:  "route-only-known-queues",
				Scope: "tool:routing.assign-queue",
				Allow: []string{"motor-standard", "motor-complex"},
			}},
		},
	}
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

func TestEvaluateToolCall_requireApprovalViaHITL(t *testing.T) {
	agent := &manifest.Agent{
		Spec: manifest.AgentSpec{
			HITL: []manifest.HITLTrigger{{
				Trigger:   "tool:routing.assign-queue",
				Condition: "severity >= 3",
				Route:     "claims-supervisor-queue",
			}},
			Tools: []manifest.ToolBinding{{
				Ref:    "routing.assign-queue",
				Policy: "require-approval-above-severity-3",
			}},
		},
	}
	e := NewEvaluator(agent)
	tc := ToolCallContext{
		ToolRef:    "routing.assign-queue",
		PolicyName: "require-approval-above-severity-3",
		Args:       json.RawMessage(`{"severity":4,"queue":"motor-standard"}`),
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
	agent := &manifest.Agent{}
	e := NewEvaluator(agent)
	tc := ToolCallContext{
		ToolRef:         "pay.charge",
		SideEffectClass: manifest.SideEffectNonIdempotentWrite,
	}
	route := e.RouteDispatchFailure(tooldispatch.ErrIndeterminate, tc)
	if route != RouteEscalateHITL {
		t.Fatalf("route = %v, want escalate", route)
	}
}

func TestRouteDispatchFailure_noHandlerHitL(t *testing.T) {
	agent := &manifest.Agent{
		Spec: manifest.AgentSpec{
			HITL: []manifest.HITLTrigger{{
				Trigger: TriggerDispatchNoHandler,
				Route:   "ops-queue",
			}},
		},
	}
	e := NewEvaluator(agent)
	tc := ToolCallContext{ToolRef: "weather.get-forecast"}
	route := e.RouteDispatchFailure(tooldispatch.ErrNoHandler, tc)
	if route != RouteEscalateHITL {
		t.Fatalf("route = %v, want escalate", route)
	}
}

func TestRouteDispatchFailure_readOnlyIndeterminateToolError(t *testing.T) {
	e := NewEvaluator(&manifest.Agent{})
	tc := ToolCallContext{
		ToolRef:         "weather.get-forecast",
		SideEffectClass: manifest.SideEffectReadOnly,
	}
	route := e.RouteDispatchFailure(tooldispatch.ErrIndeterminate, tc)
	if route != RouteToolError {
		t.Fatalf("route = %v, want tool error", route)
	}
}

func TestConditionMatches(t *testing.T) {
	args := json.RawMessage(`{"severity":3}`)
	if !conditionMatches("severity >= 3", args) {
		t.Fatal("severity >= 3 should match")
	}
	if conditionMatches("severity > 3", args) {
		t.Fatal("severity > 3 should not match")
	}
}

func TestRouteDispatchFailure_wrappedNoHandler(t *testing.T) {
	err := errors.Join(tooldispatch.ErrNoHandler, errors.New("detail"))
	e := NewEvaluator(&manifest.Agent{})
	route := e.RouteDispatchFailure(err, ToolCallContext{ToolRef: "t"})
	if route != RouteFail {
		t.Fatalf("route = %v, want fail without hitl", route)
	}
}
