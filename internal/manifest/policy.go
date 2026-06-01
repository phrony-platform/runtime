package manifest

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

const KindPolicy = "Policy"

// Policy is a v1 Policy document (kind Policy).
type Policy struct {
	APIVersion string         `yaml:"apiVersion" json:"apiVersion"`
	Kind       string         `yaml:"kind" json:"kind"`
	Metadata   PolicyMetadata `yaml:"metadata" json:"metadata"`
	Spec       PolicyDocSpec  `yaml:"spec" json:"spec"`
}

func (p *Policy) DocumentKind() string {
	if p == nil {
		return ""
	}
	return p.Kind
}

// PolicyMetadata holds identity for a Policy document.
type PolicyMetadata struct {
	Name      string            `yaml:"name" json:"name"`
	Namespace string            `yaml:"namespace" json:"namespace"`
	Version   string            `yaml:"version" json:"version"`
	Labels    map[string]string `yaml:"labels,omitempty" json:"labels,omitempty"`
}

// PolicyDocSpec declares when a policy applies and what it does.
type PolicyDocSpec struct {
	Description string           `yaml:"description,omitempty" json:"description,omitempty"`
	Scope       string           `yaml:"scope,omitempty" json:"scope,omitempty"`
	// Allow supports the legacy allow-list shape on standalone Policy documents.
	Allow      []string         `yaml:"allow,omitempty" json:"allow,omitempty"`
	Action     string           `yaml:"action,omitempty" json:"action,omitempty"`
	Conditions map[string]any   `yaml:"conditions,omitempty" json:"conditions,omitempty"`
	Decision   *PolicyDecision  `yaml:"decision,omitempty" json:"decision,omitempty"`
}

// PolicyDecision is the portable policy effect.
type PolicyDecision struct {
	Type                  string         `yaml:"type" json:"type"`
	AuthorityRef          string         `yaml:"authority_ref,omitempty" json:"authority_ref,omitempty"`
	ApprovalsRequired     int            `yaml:"approvals_required,omitempty" json:"approvals_required,omitempty"`
	Reason                string         `yaml:"reason,omitempty" json:"reason,omitempty"`
	Runtime               map[string]any `yaml:"runtime,omitempty" json:"runtime,omitempty"`
}

// LogicalID returns the catalog id namespace.name for this policy.
func (p *Policy) LogicalID() string {
	if p == nil {
		return ""
	}
	return LogicalID(p.Metadata.Namespace, p.Metadata.Name)
}

// legacyPolicySpec returns an embedded PolicySpec when the document uses the allow-list shape.
func (p *Policy) legacyPolicySpec() (PolicySpec, bool) {
	if p == nil {
		return PolicySpec{}, false
	}
	name := strings.TrimSpace(p.Metadata.Name)
	if name == "" {
		return PolicySpec{}, false
	}
	hasAllow := len(p.Spec.Allow) > 0
	hasLegacyAction := strings.TrimSpace(p.Spec.Action) != ""
	if hasAllow {
		return PolicySpec{
			Name:   name,
			Scope:  strings.TrimSpace(p.Spec.Scope),
			Allow:  append([]string(nil), p.Spec.Allow...),
		}, true
	}
	if hasLegacyAction && strings.TrimSpace(p.Spec.Scope) != "" {
		return PolicySpec{
			Name:   name,
			Scope:  strings.TrimSpace(p.Spec.Scope),
			Action: strings.TrimSpace(p.Spec.Action),
		}, true
	}
	decision := p.Spec.Decision
	if decision == nil {
		return PolicySpec{}, false
	}
	switch strings.TrimSpace(decision.Type) {
	case "allow":
		if len(p.Spec.Allow) > 0 {
			return PolicySpec{
				Name:  name,
				Scope: strings.TrimSpace(p.Spec.Scope),
				Allow: append([]string(nil), p.Spec.Allow...),
			}, true
		}
	case "":
		return PolicySpec{}, false
	default:
		if strings.TrimSpace(p.Spec.Scope) != "" && isRequireApprovalDecision(decision.Type) {
			return PolicySpec{
				Name:   name,
				Scope:  strings.TrimSpace(p.Spec.Scope),
				Action: strings.TrimSpace(decision.Type),
			}, true
		}
	}
	return PolicySpec{}, false
}

// ParsePolicy decodes YAML bytes into a Policy document.
func ParsePolicy(data []byte) (*Policy, error) {
	var policy Policy
	if err := yaml.Unmarshal(data, &policy); err != nil {
		return nil, fmt.Errorf("parse policy: %w", err)
	}
	return &policy, nil
}

func isRequireApprovalDecision(decisionType string) bool {
	t := strings.ToLower(strings.TrimSpace(decisionType))
	return t == "require_approval" || (strings.Contains(t, "require") && strings.Contains(t, "approval"))
}
