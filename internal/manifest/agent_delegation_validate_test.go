package manifest

import (
	"strings"
	"testing"
)

func bundleValidateOpts(t *testing.T) *ValidateOptions {
	t.Helper()
	return &ValidateOptions{
		BundleRoot:      t.TempDir(),
		InBundleClosure: true,
	}
}

// validDelegatingAgent returns a valid authoring agent that delegates to one
// other agent via the spec.agents sugar block.
func validDelegatingAgent() *Agent {
	a := validAgent()
	a.Spec.Agents = []SubagentBinding{{
		Ref:         "support.billing-specialist@1.2.0",
		As:          "ask_billing",
		Description: "Delegate billing questions.",
		Result:      SubagentResultSummary,
	}}
	return a
}

func TestValidate_agentDelegation(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		mutate    func(*Agent)
		opts      func(*testing.T) *ValidateOptions
		wantPaths []string
	}{
		{
			name:      "valid delegation",
			mutate:    func(*Agent) {},
			opts:      bundleValidateOpts,
			wantPaths: nil,
		},
		{
			name: "default wire name and result",
			mutate: func(a *Agent) {
				a.Spec.Agents[0].As = ""
				a.Spec.Agents[0].Result = ""
			},
			opts:      bundleValidateOpts,
			wantPaths: nil,
		},
		{
			name: "standalone agent with spec.agents rejected",
			mutate: func(a *Agent) {
				_ = a
			},
			opts:      func(*testing.T) *ValidateOptions { return nil },
			wantPaths: []string{"spec.agents"},
		},
		{
			name: "ref without version rejected",
			mutate: func(a *Agent) {
				a.Spec.Agents[0].Ref = "support.billing-specialist"
			},
			opts:      bundleValidateOpts,
			wantPaths: []string{"spec.agents[0].ref"},
		},
		{
			name: "late_bound floating external is valid",
			mutate: func(a *Agent) {
				a.Spec.Agents[0].Ref = "support.billing-specialist"
				a.Spec.Agents[0].LateBound = true
			},
			opts:      bundleValidateOpts,
			wantPaths: nil,
		},
		{
			name: "local path is valid in bundle closure",
			mutate: func(a *Agent) {
				a.Spec.Agents[0].Ref = "./specialists/billing.yaml"
			},
			opts:      bundleValidateOpts,
			wantPaths: nil,
		},
		{
			name: "local path escaping bundle root rejected",
			mutate: func(a *Agent) {
				a.Spec.Agents[0].Ref = "../outside/billing.yaml"
			},
			opts:      bundleValidateOpts,
			wantPaths: []string{"spec.agents[0].ref"},
		},
		{
			name: "result full is valid",
			mutate: func(a *Agent) {
				a.Spec.Agents[0].Result = SubagentResultFull
			},
			opts:      bundleValidateOpts,
			wantPaths: nil,
		},
		{
			name: "missing ref",
			mutate: func(a *Agent) {
				a.Spec.Agents[0].Ref = ""
			},
			opts:      bundleValidateOpts,
			wantPaths: []string{"spec.agents[0].ref"},
		},
		{
			name: "ref not namespace.name or local path",
			mutate: func(a *Agent) {
				a.Spec.Agents[0].Ref = "billing-specialist"
			},
			opts:      bundleValidateOpts,
			wantPaths: []string{"spec.agents[0].ref"},
		},
		{
			name: "ref version not semver",
			mutate: func(a *Agent) {
				a.Spec.Agents[0].Ref = "support.billing-specialist@latest"
			},
			opts:      bundleValidateOpts,
			wantPaths: []string{"spec.agents[0].ref"},
		},
		{
			name: "duplicate agent refs",
			mutate: func(a *Agent) {
				a.Spec.Agents = append(a.Spec.Agents, SubagentBinding{
					Ref: "support.billing-specialist@1.2.0",
					As:  "ask_billing_again",
				})
			},
			opts:      bundleValidateOpts,
			wantPaths: []string{"spec.agents[1].ref"},
		},
		{
			name: "self reference rejected",
			mutate: func(a *Agent) {
				a.Spec.Agents[0].Ref = LogicalID(a.Metadata.Namespace, a.Metadata.Name) + "@1.0.0"
			},
			opts:      bundleValidateOpts,
			wantPaths: []string{"spec.agents[0].ref"},
		},
		{
			name: "wire name collides with another agent",
			mutate: func(a *Agent) {
				a.Spec.Agents = append(a.Spec.Agents, SubagentBinding{
					Ref: "support.refund-specialist@1.0.0",
					As:  "ask_billing",
				})
			},
			opts:      bundleValidateOpts,
			wantPaths: []string{"spec.agents[1].as"},
		},
		{
			name: "wire name collides with a tool",
			mutate: func(a *Agent) {
				a.Spec.Tools = []ToolBinding{{Ref: "support.lookup", As: "ask_billing"}}
			},
			opts:      bundleValidateOpts,
			wantPaths: []string{"spec.agents[0].as"},
		},
		{
			name: "invalid wire name",
			mutate: func(a *Agent) {
				a.Spec.Agents[0].As = "ask billing!"
			},
			opts:      bundleValidateOpts,
			wantPaths: []string{"spec.agents[0].as"},
		},
		{
			name: "invalid result",
			mutate: func(a *Agent) {
				a.Spec.Agents[0].Result = "verbose"
			},
			opts:      bundleValidateOpts,
			wantPaths: []string{"spec.agents[0].result"},
		},
		{
			name: "input_schema neither ref nor inline",
			mutate: func(a *Agent) {
				a.Spec.Agents[0].InputSchema = &SchemaSpec{}
			},
			opts:      bundleValidateOpts,
			wantPaths: []string{"spec.agents[0].input_schema"},
		},
		{
			name: "authoring agent binding on a tool is forbidden",
			mutate: func(a *Agent) {
				a.Spec.Agents = nil
				a.Spec.Tools = []ToolBinding{{
					Ref:   "support.billing-specialist",
					As:    "ask_billing",
					Agent: &ToolAgentBinding{Namespace: "support", Name: "billing-specialist"},
				}}
			},
			opts:      bundleValidateOpts,
			wantPaths: []string{"spec.tools[0].agent"},
		},
		{
			name: "max_subagent_depth below one",
			mutate: func(a *Agent) {
				a.Spec.Limits = &Limits{MaxSubagentDepth: intPtr(0)}
			},
			opts:      bundleValidateOpts,
			wantPaths: []string{"spec.limits.max_subagent_depth"},
		},
		{
			name: "max_subagent_depth one is valid",
			mutate: func(a *Agent) {
				a.Spec.Limits = &Limits{MaxSubagentDepth: intPtr(1)}
			},
			opts:      bundleValidateOpts,
			wantPaths: nil,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			agent := validDelegatingAgent()
			tc.mutate(agent)
			opts := tc.opts(t)
			err := ValidateAgent(agent, opts)
			if len(tc.wantPaths) == 0 {
				if err != nil {
					t.Fatalf("ValidateAgent() = %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatal("ValidateAgent() = nil, want error")
			}
			valErrs, ok := err.(ValidationErrors)
			if !ok {
				t.Fatalf("error type %T, want ValidationErrors", err)
			}
			msg := err.Error()
			for _, path := range tc.wantPaths {
				if !pathInErrors(valErrs, path) && !strings.Contains(msg, path) {
					t.Fatalf("error %q missing path %q; got %v", msg, path, valErrs)
				}
			}
		})
	}
}

// TestValidate_compiledAgentBindingShape exercises the compiled-snapshot path
// where spec.tools carries an agent binding (produced by expanding spec.agents
// at publish) and must pass validation.
func TestValidate_compiledAgentBindingShape(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		mutate    func(*Agent)
		wantPaths []string
	}{
		{
			name:      "valid compiled agent binding",
			mutate:    func(*Agent) {},
			wantPaths: nil,
		},
		{
			name: "missing namespace and name",
			mutate: func(a *Agent) {
				a.Spec.Tools[0].Agent.Namespace = ""
				a.Spec.Tools[0].Agent.Name = ""
			},
			wantPaths: []string{"spec.tools[0].agent.namespace", "spec.tools[0].agent.name"},
		},
		{
			name: "unpinned compiled binding rejected",
			mutate: func(a *Agent) {
				a.Spec.Tools[0].Agent.Version = ""
				a.Spec.Tools[0].Agent.AgentVersionID = ""
				a.Spec.Tools[0].Agent.LateBound = false
			},
			wantPaths: []string{"spec.tools[0].agent"},
		},
		{
			name: "late_bound compiled binding without version is valid",
			mutate: func(a *Agent) {
				a.Spec.Tools[0].Agent.Version = ""
				a.Spec.Tools[0].Agent.AgentVersionID = ""
				a.Spec.Tools[0].Agent.LateBound = true
			},
			wantPaths: nil,
		},
		{
			name: "invalid pinned version",
			mutate: func(a *Agent) {
				a.Spec.Tools[0].Agent.Version = "latest"
			},
			wantPaths: []string{"spec.tools[0].agent.version"},
		},
		{
			name: "invalid result",
			mutate: func(a *Agent) {
				a.Spec.Tools[0].Agent.Result = "verbose"
			},
			wantPaths: []string{"spec.tools[0].agent.result"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			agent := validAgent()
			agent.Metadata.Annotations = map[string]string{AnnotationPoliciesCompiled: "true"}
			agent.Spec.Tools = []ToolBinding{{
				Ref:             "support.billing-specialist",
				As:              "ask_billing",
				InputSchema:     &SchemaSpec{Inline: map[string]any{"type": "object"}},
				SideEffectClass: SideEffectNonIdempotentWrite,
				Agent: &ToolAgentBinding{
					Namespace: "support",
					Name:      "billing-specialist",
					Version:   "1.2.0",
					Result:    SubagentResultSummary,
				},
			}}
			tc.mutate(agent)
			err := Validate(agent)
			if len(tc.wantPaths) == 0 {
				if err != nil {
					t.Fatalf("Validate() = %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatal("Validate() = nil, want error")
			}
			valErrs, ok := err.(ValidationErrors)
			if !ok {
				t.Fatalf("error type %T, want ValidationErrors", err)
			}
			msg := err.Error()
			for _, path := range tc.wantPaths {
				if !pathInErrors(valErrs, path) && !strings.Contains(msg, path) {
					t.Fatalf("error %q missing path %q; got %v", msg, path, valErrs)
				}
			}
		})
	}
}

func TestValidateBundle(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	bundle := &BundleManifest{
		APIVersion: APIVersionV1,
		Kind:       KindBundle,
		Metadata: BundleMetadata{
			Name:      "support",
			Namespace: "support",
		},
		Spec: BundleManifestSpec{
			Root: "./orchestrator.yaml",
		},
	}
	if err := ValidateBundle(bundle, root); err != nil {
		t.Fatalf("ValidateBundle() = %v", err)
	}

	bundle.Spec.Root = "../escape.yaml"
	err := ValidateBundle(bundle, root)
	if err == nil {
		t.Fatal("ValidateBundle() = nil, want error for escaping root")
	}
	valErrs, ok := err.(ValidationErrors)
	if !ok || !pathInErrors(valErrs, "spec.root") {
		t.Fatalf("error = %v, want spec.root path", err)
	}
}
