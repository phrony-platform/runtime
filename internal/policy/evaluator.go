package policy

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/phrony-platform/runtime/internal/manifest"
	"github.com/phrony-platform/runtime/internal/tooldispatch"
)

// HITL trigger prefixes for dispatch failures (whitepaper routing).
const (
	TriggerDispatchNoHandler         = "dispatch:no_handler"
	TriggerDispatchCapacityExhausted = "dispatch:capacity_exhausted"
	TriggerDispatchLeaseExpired      = "dispatch:lease_expired"
	TriggerDispatchIndeterminate     = "dispatch:indeterminate"
	TriggerLimitEscalate             = "limit:escalate"
)

const agentScope = "agent"

// ApprovalGate blocks until an operator approves or denies a pending tool call.
type ApprovalGate interface {
	WaitApproval(ctx context.Context, req ApprovalRequest) (ApprovalResult, error)
}

// ApprovalResult is the operator decision for a pending tool call.
type ApprovalResult struct {
	Approved bool
	// Args, when non-empty, replaces the proposed tool arguments (for example after edit).
	Args json.RawMessage
}

// Evaluator applies manifest policies and HITL triggers at dispatch time.
type Evaluator struct {
	agent    *manifest.Agent
	byName   map[string]manifest.PolicySpec
	policies []manifest.PolicySpec
	hitl     []manifest.HITLTrigger
}

// NewEvaluator builds a policy evaluator from a resolved agent manifest.
func NewEvaluator(agent *manifest.Agent) *Evaluator {
	e := &Evaluator{
		byName: make(map[string]manifest.PolicySpec),
	}
	if agent == nil {
		return e
	}
	e.policies = append([]manifest.PolicySpec(nil), agent.Spec.Policies...)
	for _, p := range e.policies {
		name := strings.TrimSpace(p.Name)
		if name == "" {
			continue
		}
		e.byName[name] = p
	}
	e.hitl = append([]manifest.HITLTrigger(nil), agent.Spec.HITL...)
	return e
}

// EvaluateToolCall runs condition trees and policy decisions before dispatch.
func (e *Evaluator) EvaluateToolCall(tc ToolCallContext) (Decision, string) {
	out := e.Evaluate(tc)
	return out.Decision, out.DenyMessage
}

// Evaluate merges applicable policies with deny-wins semantics.
func (e *Evaluator) Evaluate(tc ToolCallContext) EvaluationOutcome {
	if e == nil {
		return EvaluationOutcome{Decision: DecisionAllow}
	}
	var approval *ApprovalMatch
	for _, p := range e.applicablePolicies(tc) {
		if !e.policyMatches(p, tc) {
			continue
		}
		action := strings.ToLower(strings.TrimSpace(p.Action))
		switch {
		case isDenyOrBlockAction(action):
			msg := strings.TrimSpace(p.Reason)
			if msg == "" {
				msg = "tool call blocked by policy " + strings.TrimSpace(p.Name)
			}
			return EvaluationOutcome{Decision: DecisionDeny, DenyMessage: msg}
		case isRequireApprovalAction(action):
			match := approvalFromPolicy(p)
			if approval == nil || preferApproval(approval, match) {
				approval = match
			}
		case action == "allow" || len(p.Allow) > 0:
			if msg := e.checkAllow(p, tc.Args); msg != "" {
				return EvaluationOutcome{Decision: DecisionDeny, DenyMessage: msg}
			}
		}
	}
	if approval != nil {
		return EvaluationOutcome{Decision: DecisionRequireApproval, Approval: approval}
	}
	if e.legacyHITLRequiresApproval(tc) {
		return EvaluationOutcome{Decision: DecisionRequireApproval, Approval: e.legacyHITLApproval(tc)}
	}
	return EvaluationOutcome{Decision: DecisionAllow}
}

func preferApproval(current, next *ApprovalMatch) bool {
	if current == nil {
		return true
	}
	if next == nil {
		return false
	}
	if current.Reason == "" && next.Reason != "" {
		return true
	}
	if current.Route == "" && next.Route != "" {
		return true
	}
	return false
}

func (e *Evaluator) applicablePolicies(tc ToolCallContext) []manifest.PolicySpec {
	if e == nil {
		return nil
	}
	ref := strings.TrimSpace(tc.ToolRef)
	var out []manifest.PolicySpec
	seen := make(map[string]struct{})
	add := func(p manifest.PolicySpec) {
		if _, ok := seen[p.Name]; ok {
			return
		}
		seen[p.Name] = struct{}{}
		out = append(out, p)
	}
	if name := strings.TrimSpace(tc.PolicyName); name != "" {
		if p, ok := e.byName[name]; ok {
			add(p)
		}
	}
	for _, p := range e.policies {
		if policyAppliesToTool(p.Scope, ref) {
			add(p)
		}
	}
	return out
}

func policyAppliesToTool(scope, toolRef string) bool {
	scope = strings.TrimSpace(scope)
	if scope == "" || scope == agentScope {
		return true
	}
	if ref, ok := toolRefFromScoped(scope); ok {
		return ref == toolRef
	}
	return false
}

func (e *Evaluator) policyMatches(p manifest.PolicySpec, tc ToolCallContext) bool {
	if len(p.Conditions) > 0 {
		return evaluateConditions(p.Conditions, tc.Args)
	}
	return true
}

func (e *Evaluator) checkAllow(p manifest.PolicySpec, args []byte) string {
	if len(p.Allow) == 0 {
		return ""
	}
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

func isDenyOrBlockAction(action string) bool {
	return action == "deny" || action == "block"
}

func isRequireApprovalAction(action string) bool {
	return strings.Contains(action, "require") && strings.Contains(action, "approval")
}

func (e *Evaluator) legacyHITLRequiresApproval(tc ToolCallContext) bool {
	if e == nil {
		return false
	}
	policyName := strings.TrimSpace(tc.PolicyName)
	if policyName != "" {
		if p, ok := e.byName[policyName]; ok && isRequireApprovalAction(strings.ToLower(p.Action)) {
			return true
		}
		if strings.Contains(strings.ToLower(policyName), "require-approval") {
			return e.hitlConditionMatchesTool(tc)
		}
	}
	return e.hitlConditionMatchesTool(tc)
}

func (e *Evaluator) legacyHITLApproval(tc ToolCallContext) *ApprovalMatch {
	route, reason := e.hitlRouteForTool(tc.ToolRef, tc.Args)
	return &ApprovalMatch{
		Reason: reason,
		Route:  route,
	}
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
	out := e.Evaluate(tc)
	reason := "tool call requires human approval"
	route := ""
	if out.Approval != nil {
		if r := strings.TrimSpace(out.Approval.Reason); r != "" {
			reason = r
		}
		route = strings.TrimSpace(out.Approval.Route)
	}
	if route == "" {
		route, _ = e.hitlRouteForTool(tc.ToolRef, tc.Args)
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

// OnModifyFor returns the on_modify behavior for the matched approval policy, if any.
func (e *Evaluator) OnModifyFor(tc ToolCallContext) string {
	out := e.Evaluate(tc)
	if out.Approval == nil {
		return ""
	}
	return strings.TrimSpace(out.Approval.OnModify)
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
