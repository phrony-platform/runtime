package policy

import (
	"context"
	"errors"
	"strings"

	"github.com/phrony-platform/runtime/internal/manifest"
	"github.com/phrony-platform/runtime/internal/tooldispatch"
)

// HITL trigger prefixes for dispatch failures (whitepaper routing).
const (
	TriggerDispatchNoHandler          = "dispatch:no_handler"
	TriggerDispatchCapacityExhausted  = "dispatch:capacity_exhausted"
	TriggerDispatchLeaseExpired         = "dispatch:lease_expired"
	TriggerDispatchIndeterminate        = "dispatch:indeterminate"
	TriggerLimitEscalate                = "limit:escalate"
)

// ApprovalGate blocks until an operator approves or denies a pending tool call.
type ApprovalGate interface {
	WaitApproval(ctx context.Context, req ApprovalRequest) (approved bool, err error)
}

// Evaluator applies manifest policies and HITL triggers at dispatch time.
type Evaluator struct {
	agent    *manifest.Agent
	byName   map[string]manifest.PolicySpec
	hitl     []manifest.HITLTrigger
	allowIdx map[string]manifest.PolicySpec // tool ref -> allow policy
}

// NewEvaluator builds a policy evaluator from a resolved agent manifest.
func NewEvaluator(agent *manifest.Agent) *Evaluator {
	e := &Evaluator{
		byName:   make(map[string]manifest.PolicySpec),
		allowIdx: make(map[string]manifest.PolicySpec),
	}
	if agent == nil {
		return e
	}
	for _, p := range agent.Spec.Policies {
		name := strings.TrimSpace(p.Name)
		if name == "" {
			continue
		}
		e.byName[name] = p
		if len(p.Allow) > 0 {
			if ref, ok := toolRefFromScoped(strings.TrimSpace(p.Scope)); ok {
				e.allowIdx[ref] = p
			}
		}
	}
	e.hitl = append([]manifest.HITLTrigger(nil), agent.Spec.HITL...)
	return e
}

// EvaluateToolCall runs allow-list and approval rules before dispatch.
func (e *Evaluator) EvaluateToolCall(tc ToolCallContext) (Decision, string) {
	if e == nil {
		return DecisionAllow, ""
	}
	if ref := strings.TrimSpace(tc.ToolRef); ref != "" {
		if p, ok := e.allowIdx[ref]; ok {
			if msg := e.checkAllow(p, tc.Args); msg != "" {
				return DecisionDeny, msg
			}
		}
	}
	if e.requiresApproval(tc) {
		return DecisionRequireApproval, ""
	}
	return DecisionAllow, ""
}

func (e *Evaluator) checkAllow(p manifest.PolicySpec, args []byte) string {
	val, ok := allowValueFromArgs(args)
	if !ok {
		return "tool arguments do not include a value to check against the allow list"
	}
	for _, allowed := range p.Allow {
		if strings.EqualFold(strings.TrimSpace(allowed), val) {
			return ""
		}
	}
	return "value " + val + " is not allowed by policy " + strings.TrimSpace(p.Name)
}

func (e *Evaluator) requiresApproval(tc ToolCallContext) bool {
	if e == nil {
		return false
	}
	policyName := strings.TrimSpace(tc.PolicyName)
	if policyName != "" {
		if p, ok := e.byName[policyName]; ok && isRequireApprovalAction(p.Action) {
			return true
		}
		if strings.Contains(strings.ToLower(policyName), "require-approval") {
			return e.hitlConditionMatchesTool(tc)
		}
	}
	return e.hitlConditionMatchesTool(tc)
}

func isRequireApprovalAction(action string) bool {
	a := strings.ToLower(strings.TrimSpace(action))
	return strings.Contains(a, "require") && strings.Contains(a, "approval")
}

func (e *Evaluator) hitlConditionMatchesTool(tc ToolCallContext) bool {
	ref := strings.TrimSpace(tc.ToolRef)
	if ref == "" {
		return false
	}
	scope := "tool:" + ref
	for _, h := range e.hitl {
		if !strings.EqualFold(strings.TrimSpace(h.Trigger), scope) {
			continue
		}
		if conditionMatches(h.Condition, tc.Args) {
			return true
		}
	}
	return false
}

// ApprovalRequestFor builds the interactive approval payload for a tool call.
func (e *Evaluator) ApprovalRequestFor(approvalID, callID, sessionID string, tc ToolCallContext) ApprovalRequest {
	route, reason := e.hitlRouteForTool(tc.ToolRef, tc.Args)
	if reason == "" {
		reason = "tool call requires human approval"
	}
	return ApprovalRequest{
		ApprovalID: approvalID,
		CallID:     callID,
		SessionID:  sessionID,
		Tool:       tc.ToolRef,
		Version:    tc.Version,
		Args:       tc.Args,
		Route:      route,
		Reason:     reason,
	}
}

// HITLForLimitEscalation returns the route when spec.limits.on_limit is escalate.
func (e *Evaluator) HITLForLimitEscalation() (route string, ok bool) {
	if e == nil {
		return "", false
	}
	for _, h := range e.hitl {
		if strings.EqualFold(strings.TrimSpace(h.Trigger), TriggerLimitEscalate) {
			return strings.TrimSpace(h.Route), true
		}
	}
	return "", false
}

func (e *Evaluator) hitlRouteForTool(toolRef string, args []byte) (route, reason string) {
	scope := "tool:" + strings.TrimSpace(toolRef)
	for _, h := range e.hitl {
		if !strings.EqualFold(strings.TrimSpace(h.Trigger), scope) {
			continue
		}
		if conditionMatches(h.Condition, args) {
			return strings.TrimSpace(h.Route), strings.TrimSpace(h.Condition)
		}
	}
	for _, h := range e.hitl {
		if strings.EqualFold(strings.TrimSpace(h.Trigger), scope) {
			return strings.TrimSpace(h.Route), strings.TrimSpace(h.Condition)
		}
	}
	return "", ""
}

// RouteDispatchFailure maps dispatch errors to fail, tool error, or HITL escalation.
func (e *Evaluator) RouteDispatchFailure(err error, tc ToolCallContext) FailureRoute {
	if err == nil {
		return RouteFail
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		if tooldispatch.IsNoHandler(err) {
			return e.routeForTrigger(TriggerDispatchNoHandler, tc, RouteFail)
		}
		if tooldispatch.IsCapacityExhausted(err) || tooldispatch.IsQueueFull(err) {
			return e.routeForTrigger(TriggerDispatchCapacityExhausted, tc, RouteFail)
		}
		return RouteFail
	}

	switch {
	case tooldispatch.IsNoHandler(err):
		return e.routeForTrigger(TriggerDispatchNoHandler, tc, RouteFail)
	case tooldispatch.IsCapacityExhausted(err), tooldispatch.IsQueueFull(err):
		return e.routeForTrigger(TriggerDispatchCapacityExhausted, tc, RouteFail)
	case tooldispatch.IsLeaseExpired(err):
		return e.routeForTrigger(TriggerDispatchLeaseExpired, tc, e.defaultIndeterminateRoute(tc))
	case tooldispatch.IsIndeterminate(err):
		return e.routeForTrigger(TriggerDispatchIndeterminate, tc, e.defaultIndeterminateRoute(tc))
	case tooldispatch.IsIntegrityError(err):
		return RouteToolError
	default:
		return RouteFail
	}
}

func (e *Evaluator) defaultIndeterminateRoute(tc ToolCallContext) FailureRoute {
	switch strings.TrimSpace(tc.SideEffectClass) {
	case manifest.SideEffectReadOnly, manifest.SideEffectIdempotentWrite, "":
		return RouteToolError
	default:
		return RouteEscalateHITL
	}
}

func (e *Evaluator) routeForTrigger(trigger string, tc ToolCallContext, fallback FailureRoute) FailureRoute {
	if e == nil {
		return fallback
	}
	for _, h := range e.hitl {
		if !strings.EqualFold(strings.TrimSpace(h.Trigger), trigger) {
			continue
		}
		if h.Condition == "" || conditionMatches(h.Condition, tc.Args) {
			return RouteEscalateHITL
		}
	}
	return fallback
}

// DispatchFailureApproval builds an approval request when dispatch failure routes to HITL.
func (e *Evaluator) DispatchFailureApproval(approvalID, callID, sessionID string, tc ToolCallContext, dispatchErr error) ApprovalRequest {
	route, _ := e.hitlRouteForDispatchError(dispatchErr)
	reason := dispatchErr.Error()
	return ApprovalRequest{
		ApprovalID: approvalID,
		CallID:     callID,
		SessionID:  sessionID,
		Tool:       tc.ToolRef,
		Version:    tc.Version,
		Args:       tc.Args,
		Route:      route,
		Reason:     reason,
	}
}

func (e *Evaluator) hitlRouteForDispatchError(err error) (route, trigger string) {
	switch {
	case tooldispatch.IsNoHandler(err):
		trigger = TriggerDispatchNoHandler
	case tooldispatch.IsCapacityExhausted(err), tooldispatch.IsQueueFull(err):
		trigger = TriggerDispatchCapacityExhausted
	case tooldispatch.IsLeaseExpired(err):
		trigger = TriggerDispatchLeaseExpired
	case tooldispatch.IsIndeterminate(err):
		trigger = TriggerDispatchIndeterminate
	default:
		return "", ""
	}
	if e == nil {
		return "", trigger
	}
	for _, h := range e.hitl {
		if strings.EqualFold(strings.TrimSpace(h.Trigger), trigger) {
			return strings.TrimSpace(h.Route), trigger
		}
	}
	return "", trigger
}

func toolRefFromScoped(scoped string) (string, bool) {
	const prefix = "tool:"
	if !strings.HasPrefix(scoped, prefix) {
		return "", false
	}
	ref := strings.TrimSpace(scoped[len(prefix):])
	return ref, ref != ""
}
