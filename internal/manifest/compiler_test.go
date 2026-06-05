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

func TestCompile_subagentBindings(t *testing.T) {
	t.Parallel()
	agent := &manifest.Agent{
		APIVersion: manifest.APIVersionV1,
		Kind:       manifest.KindAgent,
		Metadata: manifest.AgentMetadata{
			Name:      "orchestrator",
			Namespace: "support",
			Version:   "1.0.0",
		},
		Spec: manifest.AgentSpec{
			Purpose:      "test",
			Instructions: manifest.InstructionsSpec{Text: "hi"},
			Model: manifest.ModelConfig{
				Provider: "anthropic",
				Name:     "claude-sonnet-4-5",
			},
			Agents: []manifest.SubagentBinding{
				{
					Ref:         "support.billing-specialist@1.2.0",
					As:          "ask_billing",
					Description: "Delegate billing questions.",
					Result:      manifest.SubagentResultFull,
				},
				{
					Ref: "support.refund-specialist",
				},
			},
		},
	}

	resolved, err := manifest.Compile(t.TempDir()+"/agent.yaml", agent)
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}

	if resolved.Agent.Spec.Agents != nil {
		t.Fatalf("spec.agents = %v, want cleared after compile", resolved.Agent.Spec.Agents)
	}
	if len(resolved.Agent.Spec.Tools) != 2 {
		t.Fatalf("compiled tools len = %d, want 2", len(resolved.Agent.Spec.Tools))
	}

	billing := findToolByName(t, resolved.Agent.Spec.Tools, "ask_billing")
	if !billing.IsAgent() {
		t.Fatal("ask_billing binding should be agent-backed")
	}
	if billing.SideEffectClass != manifest.SideEffectNonIdempotentWrite {
		t.Fatalf("side_effect_class = %q, want non_idempotent_write", billing.SideEffectClass)
	}
	if billing.Agent.Namespace != "support" || billing.Agent.Name != "billing-specialist" {
		t.Fatalf("agent identity = %+v", billing.Agent)
	}
	if billing.Agent.Version != "1.2.0" {
		t.Fatalf("agent version = %q, want pinned 1.2.0", billing.Agent.Version)
	}
	if billing.Agent.ResolvedResult() != manifest.SubagentResultFull {
		t.Fatalf("result = %q, want full", billing.Agent.ResolvedResult())
	}
	if got := billing.DispatchRef(); got != "support.billing-specialist" {
		t.Fatalf("DispatchRef() = %q, want support.billing-specialist", got)
	}
	if billing.Description != "Delegate billing questions." {
		t.Fatalf("description = %q", billing.Description)
	}

	refund := findToolByName(t, resolved.Agent.Spec.Tools, "support_refund-specialist")
	if !refund.IsAgent() {
		t.Fatal("refund binding should be agent-backed")
	}
	if refund.Agent.Version != "" {
		t.Fatalf("refund version = %q, want empty (active deployment)", refund.Agent.Version)
	}
	if refund.Agent.ResolvedResult() != manifest.SubagentResultSummary {
		t.Fatalf("result = %q, want default summary", refund.Agent.ResolvedResult())
	}
	if refund.InputSchema == nil || refund.InputSchema.Inline["type"] != "object" {
		t.Fatalf("default input_schema not applied: %+v", refund.InputSchema)
	}
	props, ok := refund.InputSchema.Inline["properties"].(map[string]any)
	if !ok || props["task"] == nil {
		t.Fatalf("default input_schema missing task property: %+v", refund.InputSchema.Inline)
	}
}

func findToolByName(t *testing.T, tools []manifest.ToolBinding, name string) manifest.ToolBinding {
	t.Helper()
	for _, tb := range tools {
		if tb.ToolName() == name {
			return tb
		}
	}
	t.Fatalf("tool %q not found among %d bindings", name, len(tools))
	return manifest.ToolBinding{}
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
