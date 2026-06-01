package manifest_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/phrony-platform/runtime/internal/manifest"
)

func TestCompile_multidocCatalog(t *testing.T) {
	t.Parallel()
	bundle := filepath.Join("testdata", "bundle-multidoc")
	agentPath := filepath.Join(bundle, "agent.yaml")
	data := readTestdataFile(t, filepath.Join("bundle-multidoc", "agent.yaml"))
	agent, err := manifest.Parse(data)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if err := manifest.Validate(agent); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	resolved, err := manifest.Compile(agentPath, agent)
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}

	if len(resolved.Agent.Spec.DefaultPolicies) != 0 {
		t.Fatalf("default_policies = %v, want cleared after compile", resolved.Agent.Spec.DefaultPolicies)
	}
	tb := resolved.Agent.Spec.Tools[0]
	if len(tb.Policies) != 0 {
		t.Fatalf("tool policies attachments = %v, want cleared", tb.Policies)
	}
	if tb.InputSchema == nil || len(tb.InputSchema.Inline) == 0 {
		t.Fatal("input_schema not inlined")
	}

	foundAllow := false
	foundApproval := false
	for _, p := range resolved.Agent.Spec.Policies {
		if p.Scope != "tool:claims.approve-payment" {
			continue
		}
		if len(p.Allow) > 0 {
			foundAllow = true
			if len(p.Allow) != 2 {
				t.Fatalf("merged allow = %v, want USD and CAD", p.Allow)
			}
		}
		if p.Action == "require_approval" {
			foundApproval = true
		}
	}
	if !foundAllow {
		t.Fatal("merged allow policy missing for approve-payment")
	}
	if !foundApproval {
		t.Fatal("merged require_approval policy missing for approve-payment")
	}

	raw, err := resolved.JSON()
	if err != nil {
		t.Fatalf("JSON() error = %v", err)
	}
	if strings.Contains(string(raw), "schemas/payment-input") {
		t.Fatalf("JSON() still contains unresolved schema ref: %s", string(raw))
	}
}

func TestCompile_authorityBoundaries(t *testing.T) {
	t.Parallel()
	agent := &manifest.Agent{
		APIVersion: manifest.APIVersionV1,
		Kind:       manifest.KindAgent,
		Metadata: manifest.AgentMetadata{
			Name:      "triage",
			Namespace: "claims",
			Version:   "1.0.0",
			Governance: &manifest.GovernanceMetadata{
				AuthorityBoundaries: []string{"claims.payment-authority"},
			},
		},
		Spec: manifest.AgentSpec{
			Purpose: "test",
			Instructions: manifest.InstructionsSpec{
				Text: "hi",
			},
			Model: manifest.ModelConfig{
				Provider: "anthropic",
				Name:     "claude-sonnet-4-5",
			},
		},
	}

	resolved, err := manifest.Compile(t.TempDir()+"/agent.yaml", agent)
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}

	var boundary *manifest.PolicySpec
	for i := range resolved.Agent.Spec.Policies {
		p := &resolved.Agent.Spec.Policies[i]
		if p.AuthorityRef == "claims.payment-authority" {
			boundary = p
			break
		}
	}
	if boundary == nil {
		t.Fatal("compiled authority boundary policy missing")
	}
	if boundary.Scope != "agent" {
		t.Fatalf("boundary scope = %q, want agent", boundary.Scope)
	}
	if boundary.Action != "require_approval" {
		t.Fatalf("boundary action = %q", boundary.Action)
	}
}

func TestCompile_denyWinsAllowIntersect(t *testing.T) {
	t.Parallel()
	agent := &manifest.Agent{
		APIVersion: manifest.APIVersionV1,
		Kind:       manifest.KindAgent,
		Metadata: manifest.AgentMetadata{
			Name:      "triage",
			Namespace: "claims",
			Version:   "1.0.0",
		},
		Spec: manifest.AgentSpec{
			Purpose: "test",
			Instructions: manifest.InstructionsSpec{
				Text: "hi",
			},
			Model: manifest.ModelConfig{
				Provider: "anthropic",
				Name:     "claude-sonnet-4-5",
			},
			Tools: []manifest.ToolBinding{
				{Ref: "claims.pay"},
			},
			Policies: []manifest.PolicySpec{
				{
					Name:  "region-a",
					Scope: "tool:claims.pay",
					Allow: []string{"USD", "CAD", "EUR"},
				},
				{
					Name:  "region-b",
					Scope: "tool:claims.pay",
					Allow: []string{"USD", "CAD"},
				},
			},
		},
	}

	resolved, err := manifest.Compile(t.TempDir()+"/agent.yaml", agent)
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}

	var allowPolicy *manifest.PolicySpec
	for i := range resolved.Agent.Spec.Policies {
		p := &resolved.Agent.Spec.Policies[i]
		if p.Scope == "tool:claims.pay" && len(p.Allow) > 0 {
			allowPolicy = p
			break
		}
	}
	if allowPolicy == nil {
		t.Fatal("merged allow policy missing")
	}
	if len(allowPolicy.Allow) != 2 {
		t.Fatalf("intersected allow = %v, want USD and CAD only", allowPolicy.Allow)
	}
}
