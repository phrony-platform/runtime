package manifest

import (
	"fmt"
	"sort"
	"strings"
)

// Compile resolves bundle refs and emits a normalized deploy snapshot: catalog and
// file policy attachments are inlined into spec.policies, metadata.governance.authority_boundaries
// compile to require_approval policies, and overlapping rules merge with deny-wins semantics.
func Compile(agentPath string, agent *Agent) (*ResolvedAgent, error) {
	resolved, err := ResolveBundle(agentPath, agent)
	if err != nil {
		return nil, err
	}
	if err := compileResolved(resolved.Agent); err != nil {
		return nil, err
	}
	if err := InlineStubScript(agentPath, resolved.Agent); err != nil {
		return nil, err
	}
	return resolved, nil
}

func compileResolved(agent *Agent) error {
	if agent == nil {
		return fmt.Errorf("agent is nil")
	}
	compiled := compileAuthorityBoundaries(agent)
	agent.Spec.Policies = append(agent.Spec.Policies, compiled...)
	agent.Spec.Policies = mergePoliciesDenyWins(agent.Spec.Policies)
	markPoliciesCompiled(agent)
	normalizeResolvedSnapshot(agent)
	return nil
}

func markPoliciesCompiled(agent *Agent) {
	if agent == nil {
		return
	}
	if agent.Metadata.Annotations == nil {
		agent.Metadata.Annotations = make(map[string]string)
	}
	agent.Metadata.Annotations[AnnotationPoliciesCompiled] = "true"
}

func compileAuthorityBoundaries(agent *Agent) []PolicySpec {
	if agent == nil || agent.Metadata.Governance == nil {
		return nil
	}
	var out []PolicySpec
	for _, boundary := range agent.Metadata.Governance.AuthorityBoundaries {
		boundary = strings.TrimSpace(boundary)
		if boundary == "" {
			continue
		}
		out = append(out, PolicySpec{
			Name:         authorityBoundaryPolicyName(boundary),
			Scope:        agentScope,
			Action:       "require_approval",
			AuthorityRef: boundary,
		})
	}
	return out
}

const agentScope = "agent"

func authorityBoundaryPolicyName(boundary string) string {
	return "authority-boundary-" + sanitizePolicyName(boundary)
}

func sanitizePolicyName(s string) string {
	s = strings.TrimSpace(s)
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	name := strings.Trim(b.String(), "-")
	if name == "" {
		return "unnamed"
	}
	return name
}

// mergePoliciesDenyWins merges policies that share a scope. Allow lists intersect (deny-wins);
// multiple require_approval rules collapse to one; other rules are preserved.
func mergePoliciesDenyWins(policies []PolicySpec) []PolicySpec {
	if len(policies) == 0 {
		return nil
	}
	grouped := make(map[string][]PolicySpec)
	var order []string
	for _, p := range policies {
		key := policyScopeKey(p.Scope)
		if _, seen := grouped[key]; !seen {
			order = append(order, key)
		}
		grouped[key] = append(grouped[key], p)
	}
	var out []PolicySpec
	for _, key := range order {
		out = append(out, mergePoliciesForScope(key, grouped[key])...)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Scope != out[j].Scope {
			return out[i].Scope < out[j].Scope
		}
		return out[i].Name < out[j].Name
	})
	return out
}

func policyScopeKey(scope string) string {
	if s := strings.TrimSpace(scope); s != "" {
		return s
	}
	return agentScope
}

func scopeFromKey(key string) string {
	if key == agentScope {
		return agentScope
	}
	return key
}

func mergePoliciesForScope(scopeKey string, group []PolicySpec) []PolicySpec {
	var allowSources []PolicySpec
	var approvalSources []PolicySpec
	var other []PolicySpec

	for _, p := range group {
		switch {
		case len(p.Allow) > 0:
			allowSources = append(allowSources, p)
		case isRequireApprovalPolicy(p):
			approvalSources = append(approvalSources, p)
		default:
			other = append(other, p)
		}
	}

	var merged []PolicySpec
	if len(allowSources) > 0 {
		lists := make([][]string, len(allowSources))
		for i, p := range allowSources {
			lists[i] = p.Allow
		}
		merged = append(merged, PolicySpec{
			Name:  mergedAllowPolicyName(scopeKey, allowSources),
			Scope: scopeFromKey(scopeKey),
			Allow: intersectAllowLists(lists),
		})
	}
	if len(approvalSources) > 0 {
		var unconditional []PolicySpec
		for _, p := range approvalSources {
			if len(p.Conditions) > 0 {
				merged = append(merged, p)
				continue
			}
			unconditional = append(unconditional, p)
		}
		if len(unconditional) > 0 {
			merged = append(merged, mergeApprovalPolicies(scopeKey, unconditional))
		}
	}
	return append(merged, other...)
}

func isRequireApprovalPolicy(p PolicySpec) bool {
	a := strings.ToLower(strings.TrimSpace(p.Action))
	return strings.Contains(a, "require") && strings.Contains(a, "approval")
}

func mergedAllowPolicyName(scopeKey string, sources []PolicySpec) string {
	if len(sources) == 1 {
		return sources[0].Name
	}
	return "merged-allow-" + sanitizePolicyName(scopeKey)
}

func mergeApprovalPolicies(scopeKey string, sources []PolicySpec) PolicySpec {
	name := sources[0].Name
	if len(sources) > 1 {
		name = "merged-approval-" + sanitizePolicyName(scopeKey)
	}
	merged := PolicySpec{
		Name:              name,
		Scope:             scopeFromKey(scopeKey),
		Action:            "require_approval",
		ApprovalsRequired: 1,
	}
	for _, p := range sources {
		if ar := strings.TrimSpace(p.AuthorityRef); ar != "" && merged.AuthorityRef == "" {
			merged.AuthorityRef = ar
		}
		if r := strings.TrimSpace(p.Reason); r != "" && merged.Reason == "" {
			merged.Reason = r
		}
		if om := strings.TrimSpace(p.OnModify); om == "revalidate" {
			merged.OnModify = om
		} else if merged.OnModify == "" && om != "" {
			merged.OnModify = om
		}
		if or := strings.TrimSpace(p.OnReject); or == "fail" {
			merged.OnReject = or
		} else if merged.OnReject == "" && or != "" {
			merged.OnReject = or
		}
		if p.ComprehensionRequired {
			merged.ComprehensionRequired = true
		}
		required := p.ApprovalsRequired
		if required <= 0 {
			required = 1
		}
		if required > merged.ApprovalsRequired {
			merged.ApprovalsRequired = required
		}
		if p.Timeout != nil {
			merged.Timeout = mergeStrictestTimeout(merged.Timeout, p.Timeout)
		}
		if len(p.Runtime) > 0 && len(merged.Runtime) == 0 {
			merged.Runtime = copyAnyMap(p.Runtime)
		}
	}
	return merged
}

func mergeStrictestTimeout(current, next *PolicyTimeout) *PolicyTimeout {
	if next == nil {
		return current
	}
	if current == nil {
		return &PolicyTimeout{
			AfterMinutes: next.AfterMinutes,
			Default:      strings.TrimSpace(next.Default),
		}
	}
	out := *current
	if next.AfterMinutes > 0 && (out.AfterMinutes == 0 || next.AfterMinutes < out.AfterMinutes) {
		out.AfterMinutes = next.AfterMinutes
	}
	if d := strings.TrimSpace(next.Default); d != "" {
		out.Default = preferTimeoutDefault(out.Default, d)
	}
	return &out
}

func preferTimeoutDefault(current, next string) string {
	current = strings.ToLower(strings.TrimSpace(current))
	next = strings.ToLower(strings.TrimSpace(next))
	if current == "" {
		return next
	}
	if next == "deny" {
		return next
	}
	if current == "deny" {
		return current
	}
	if next == "escalate" {
		return next
	}
	return current
}

func intersectAllowLists(lists [][]string) []string {
	if len(lists) == 0 {
		return nil
	}
	set := make(map[string]string, len(lists[0]))
	for _, v := range lists[0] {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		set[strings.ToLower(v)] = v
	}
	for _, list := range lists[1:] {
		next := make(map[string]string)
		for _, v := range list {
			v = strings.TrimSpace(v)
			if v == "" {
				continue
			}
			key := strings.ToLower(v)
			if canonical, ok := set[key]; ok {
				next[key] = canonical
			}
		}
		set = next
	}
	out := make([]string, 0, len(set))
	for _, v := range set {
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}

func normalizeResolvedSnapshot(agent *Agent) {
	agent.Spec.DefaultPolicies = nil
	for i := range agent.Spec.Tools {
		agent.Spec.Tools[i].Policies = nil
		normalizeToolBindingSchema(&agent.Spec.Tools[i])
	}
}

func normalizeToolBindingSchema(tb *ToolBinding) {
	// input_schema is the only binding schema field; nothing to normalize.
	_ = tb
}
