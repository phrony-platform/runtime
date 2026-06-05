package manifest

import (
	"strings"
	"testing"
)

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
		wantPaths []string
	}{
		{
			name:      "valid delegation",
			mutate:    func(*Agent) {},
			wantPaths: nil,
		},
		{
			name: "default wire name and result",
			mutate: func(a *Agent) {
				a.Spec.Agents[0].As = ""
				a.Spec.Agents[0].Result = ""
			},
			wantPaths: nil,
		},
		{
			name: "ref without version is valid",
			mutate: func(a *Agent) {
				a.Spec.Agents[0].Ref = "support.billing-specialist"
			},
			wantPaths: nil,
		},
		{
			name: "result full is valid",
			mutate: func(a *Agent) {
				a.Spec.Agents[0].Result = SubagentResultFull
			},
			wantPaths: nil,
		},
		{
			name: "missing ref",
			mutate: func(a *Agent) {
				a.Spec.Agents[0].Ref = ""
			},
			wantPaths: []string{"spec.agents[0].ref"},
		},
		{
			name: "ref not namespace.name",
			mutate: func(a *Agent) {
				a.Spec.Agents[0].Ref = "billing-specialist"
			},
			wantPaths: []string{"spec.agents[0].ref"},
		},
		{
			name: "ref version not semver",
			mutate: func(a *Agent) {
				a.Spec.Agents[0].Ref = "support.billing-specialist@latest"
			},
			wantPaths: []string{"spec.agents[0].ref"},
		},
		{
			name: "duplicate agent refs",
			mutate: func(a *Agent) {
				a.Spec.Agents = append(a.Spec.Agents, SubagentBinding{
					Ref: "support.billing-specialist",
					As:  "ask_billing_again",
				})
			},
			wantPaths: []string{"spec.agents[1].ref"},
		},
		{
			name: "self reference rejected",
			mutate: func(a *Agent) {
				a.Spec.Agents[0].Ref = LogicalID(a.Metadata.Namespace, a.Metadata.Name)
			},
			wantPaths: []string{"spec.agents[0].ref"},
		},
		{
			name: "wire name collides with another agent",
			mutate: func(a *Agent) {
				a.Spec.Agents = append(a.Spec.Agents, SubagentBinding{
					Ref: "support.refund-specialist",
					As:  "ask_billing",
				})
			},
			wantPaths: []string{"spec.agents[1].as"},
		},
		{
			name: "wire name collides with a tool",
			mutate: func(a *Agent) {
				a.Spec.Tools = []ToolBinding{{Ref: "support.lookup", As: "ask_billing"}}
			},
			wantPaths: []string{"spec.agents[0].as"},
		},
		{
			name: "invalid wire name",
			mutate: func(a *Agent) {
				a.Spec.Agents[0].As = "ask billing!"
			},
			wantPaths: []string{"spec.agents[0].as"},
		},
		{
			name: "invalid result",
			mutate: func(a *Agent) {
				a.Spec.Agents[0].Result = "verbose"
			},
			wantPaths: []string{"spec.agents[0].result"},
		},
		{
			name: "input_schema neither ref nor inline",
			mutate: func(a *Agent) {
				a.Spec.Agents[0].InputSchema = &SchemaSpec{}
			},
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
			wantPaths: []string{"spec.tools[0].agent"},
		},
		{
			name: "max_subagent_depth below one",
			mutate: func(a *Agent) {
				a.Spec.Limits = &Limits{MaxSubagentDepth: intPtr(0)}
			},
			wantPaths: []string{"spec.limits.max_subagent_depth"},
		},
		{
			name: "max_subagent_depth one is valid",
			mutate: func(a *Agent) {
				a.Spec.Limits = &Limits{MaxSubagentDepth: intPtr(1)}
			},
			wantPaths: nil,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			agent := validDelegatingAgent()
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
