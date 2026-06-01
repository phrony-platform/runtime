package manifest

import (
	"strings"
	"testing"
)

func TestValidate_validAgent(t *testing.T) {
	t.Parallel()
	if err := Validate(validAgent()); err != nil {
		t.Fatalf("Validate() = %v", err)
	}
}

func TestValidate_fieldErrors(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		mutate    func(*Agent)
		wantPaths []string
	}{
		{
			name: "wrong apiVersion and kind",
			mutate: func(a *Agent) {
				a.APIVersion = "v0"
				a.Kind = "Pod"
			},
			wantPaths: []string{"apiVersion", "kind"},
		},
		{
			name: "missing metadata",
			mutate: func(a *Agent) {
				a.Metadata = AgentMetadata{}
			},
			wantPaths: []string{"metadata.name", "metadata.namespace", "metadata.version"},
		},
		{
			name: "missing purpose",
			mutate: func(a *Agent) {
				a.Spec.Purpose = ""
			},
			wantPaths: []string{"spec.purpose"},
		},
		{
			name: "instructions neither ref nor text",
			mutate: func(a *Agent) {
				a.Spec.Instructions = InstructionsSpec{}
			},
			wantPaths: []string{"spec.instructions"},
		},
		{
			name: "missing model provider and name",
			mutate: func(a *Agent) {
				a.Spec.Model = ModelConfig{}
			},
			wantPaths: []string{"spec.model.provider", "spec.model.name"},
		},
		{
			name: "invalid reasoning effort",
			mutate: func(a *Agent) {
				a.Spec.Model.Reasoning = &ReasoningConfig{Effort: "extreme"}
			},
			wantPaths: []string{"spec.model.reasoning.effort"},
		},
		{
			name: "invalid on_limit",
			mutate: func(a *Agent) {
				a.Spec.Limits = &Limits{OnLimit: "stop"}
			},
			wantPaths: []string{"spec.limits.on_limit"},
		},
		{
			name: "valid on_limit halt",
			mutate: func(a *Agent) {
				a.Spec.Limits = &Limits{OnLimit: "halt"}
			},
			wantPaths: nil,
		},
		{
			name: "valid on_limit escalate",
			mutate: func(a *Agent) {
				a.Spec.Limits = &Limits{OnLimit: "escalate"}
			},
			wantPaths: nil,
		},
		{
			name: "limits without on_limit",
			mutate: func(a *Agent) {
				a.Spec.Limits = &Limits{MaxTokensPerRun: intPtr(100)}
			},
			wantPaths: nil,
		},
		{
			name: "invalid output format",
			mutate: func(a *Agent) {
				a.Output = &OutputSpec{Format: "xml"}
			},
			wantPaths: []string{"output.format"},
		},
		{
			name: "invalid on_invalid",
			mutate: func(a *Agent) {
				a.Output = &OutputSpec{OnInvalid: "ignore"}
			},
			wantPaths: []string{"output.on_invalid"},
		},
		{
			name: "schema ref and inline",
			mutate: func(a *Agent) {
				a.Output = &OutputSpec{
					Schema: &SchemaSpec{
						Ref:    "schemas/foo",
						Inline: map[string]any{"type": "object"},
					},
				}
			},
			wantPaths: []string{"output.schema"},
		},
		{
			name: "schema neither ref nor inline",
			mutate: func(a *Agent) {
				a.Output = &OutputSpec{Schema: &SchemaSpec{}}
			},
			wantPaths: []string{"output.schema"},
		},
		{
			name: "schema inline only",
			mutate: func(a *Agent) {
				a.Output = &OutputSpec{
					Schema: &SchemaSpec{
						Inline: map[string]any{"type": "object"},
					},
				}
			},
			wantPaths: nil,
		},
		{
			name: "instructions text only",
			mutate: func(a *Agent) {
				a.Spec.Instructions = InstructionsSpec{Text: "You are helpful."}
			},
			wantPaths: nil,
		},
		{
			name: "valid tool version and side_effect_class",
			mutate: func(a *Agent) {
				a.Spec.Tools = []ToolBinding{{
					Ref:             "claims-db.read-claim",
					Version:         "1.0.0",
					SideEffectClass: SideEffectReadOnly,
				}}
			},
			wantPaths: nil,
		},
		{
			name: "invalid tool version",
			mutate: func(a *Agent) {
				a.Spec.Tools = []ToolBinding{{
					Ref:     "claims-db.read-claim",
					Version: "not-semver",
				}}
			},
			wantPaths: []string{"spec.tools[0].version"},
		},
		{
			name: "invalid side_effect_class",
			mutate: func(a *Agent) {
				a.Spec.Tools = []ToolBinding{{
					Ref:             "claims-db.read-claim",
					SideEffectClass: "destructive",
				}}
			},
			wantPaths: []string{"spec.tools[0].side_effect_class"},
		},
		{
			name: "valid policies and hitl",
			mutate: func(a *Agent) {
				a.Spec.Tools = []ToolBinding{
					{Ref: "routing.assign-queue"},
				}
				a.Spec.Policies = []PolicySpec{{
					Name:  "route-only-known-queues",
					Scope: "tool:routing.assign-queue",
					Allow: []string{"motor-standard", "motor-complex"},
				}}
				a.Spec.HITL = []HITLTrigger{{
					Trigger:   "tool:routing.assign-queue",
					Condition: "severity >= 3",
					Route:     "claims-supervisor-queue",
				}}
			},
			wantPaths: nil,
		},
		{
			name: "policy missing name",
			mutate: func(a *Agent) {
				a.Spec.Policies = []PolicySpec{{Allow: []string{"a"}}}
			},
			wantPaths: []string{"spec.policies[0].name"},
		},
		{
			name: "policy action and allow",
			mutate: func(a *Agent) {
				a.Spec.Policies = []PolicySpec{{
					Name:   "bad",
					Action: "redact",
					Allow:  []string{"a"},
				}}
			},
			wantPaths: []string{"spec.policies[0]"},
		},
		{
			name: "hitl missing route",
			mutate: func(a *Agent) {
				a.Spec.HITL = []HITLTrigger{{Trigger: "tool:foo"}}
			},
			wantPaths: []string{"spec.hitl[0].route"},
		},
		{
			name: "hitl undeclared tool ref",
			mutate: func(a *Agent) {
				a.Spec.HITL = []HITLTrigger{{
					Trigger: "tool:unknown.tool",
					Route:   "review-queue",
				}}
			},
			wantPaths: []string{"spec.hitl[0].trigger"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			agent := validAgent()
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

func validAgent() *Agent {
	return &Agent{
		APIVersion: APIVersionV1,
		Kind:       KindAgent,
		Metadata: AgentMetadata{
			Name:      "agent",
			Namespace: "default",
			Version:   "1.0.0",
		},
		Spec: AgentSpec{
			Purpose:      "Do work.",
			Instructions: InstructionsSpec{Ref: "prompts/agent"},
			Model: ModelConfig{
				Provider: "anthropic",
				Name:     "claude-sonnet-4-5",
			},
		},
	}
}

func intPtr(n int) *int {
	return &n
}

func pathInErrors(errs ValidationErrors, path string) bool {
	for _, e := range errs {
		if e.Path == path {
			return true
		}
	}
	return false
}
