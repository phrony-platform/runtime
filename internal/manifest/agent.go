package manifest

import (
	"regexp"
	"strings"
)

const (
	APIVersionV1 = "phrony.dev/v1"
	KindAgent    = "Agent"
)

// Agent is the v1 Agent document (Kubernetes-style envelope).
type Agent struct {
	APIVersion string                      `yaml:"apiVersion" json:"apiVersion"`
	Kind       string                      `yaml:"kind" json:"kind"`
	Metadata   AgentMetadata               `yaml:"metadata" json:"metadata"`
	Secrets    map[string]SecretDefinition `yaml:"secrets,omitempty" json:"secrets,omitempty"`
	Spec       AgentSpec                   `yaml:"spec" json:"spec"`
	Output     *OutputSpec                 `yaml:"output,omitempty" json:"output,omitempty"`
}

// AgentMetadata holds identity and versioning for an Agent.
type AgentMetadata struct {
	Name      string            `yaml:"name" json:"name"`
	Namespace string            `yaml:"namespace" json:"namespace"`
	Version   string            `yaml:"version" json:"version"`
	Owner     string            `yaml:"owner,omitempty" json:"owner,omitempty"`
	Labels    map[string]string `yaml:"labels,omitempty" json:"labels,omitempty"`
}

// AgentSpec is the behavior envelope for an Agent.
type AgentSpec struct {
	Purpose      string           `yaml:"purpose" json:"purpose"`
	Instructions InstructionsSpec `yaml:"instructions" json:"instructions"`
	Model        ModelConfig      `yaml:"model" json:"model"`
	Tools        []ToolBinding    `yaml:"tools,omitempty" json:"tools,omitempty"`
	Policies     []PolicySpec     `yaml:"policies,omitempty" json:"policies,omitempty"`
	HITL         []HITLTrigger    `yaml:"hitl,omitempty" json:"hitl,omitempty"`
	Limits       *Limits          `yaml:"limits,omitempty" json:"limits,omitempty"`
}

// ToolBinding declares one tool the agent may call. The runtime presents the
// tool contract to the model and dispatches authorized calls to the
// application that owns the implementation; the runtime never executes tool
// code itself. Each binding references a tool by stable identifier (ref).
type ToolBinding struct {
	Ref string `yaml:"ref" json:"ref"`
	// Name is the wire name presented to the model. When empty it is derived
	// from ref. It must be unique within the agent and model-API safe.
	Name        string      `yaml:"name,omitempty" json:"name,omitempty"`
	Description string      `yaml:"description,omitempty" json:"description,omitempty"`
	Parameters  *SchemaSpec `yaml:"parameters,omitempty" json:"parameters,omitempty"`
	// Version is the tool contract version bound for dispatch (tool@version).
	Version string `yaml:"version,omitempty" json:"version,omitempty"`
	// SideEffectClass classifies mutability for dispatch and recovery policy.
	SideEffectClass string `yaml:"side_effect_class,omitempty" json:"side_effect_class,omitempty"`
	// Policy references a named entry in spec.policies that shapes this tool.
	Policy string `yaml:"policy,omitempty" json:"policy,omitempty"`
}

// Side effect classes (whitepaper / runtime dispatch).
const (
	SideEffectReadOnly            = "read_only"
	SideEffectIdempotentWrite     = "idempotent_write"
	SideEffectNonIdempotentWrite  = "non_idempotent_write"
	SideEffectIrreversibleAction  = "irreversible_action"
)

// CanRedispatchAfterIndeterminate reports whether recovery may re-dispatch a call
// with the same call_id when the prior outcome is unknown.
func CanRedispatchAfterIndeterminate(sideEffectClass string) bool {
	switch strings.TrimSpace(sideEffectClass) {
	case SideEffectReadOnly, SideEffectIdempotentWrite, "":
		return true
	default:
		return false
	}
}

// PolicySpec declares one policy rule in the agent manifest (whitepaper shape).
type PolicySpec struct {
	Name   string   `yaml:"name" json:"name"`
	Scope  string   `yaml:"scope,omitempty" json:"scope,omitempty"`
	Action string   `yaml:"action,omitempty" json:"action,omitempty"`
	Allow  []string `yaml:"allow,omitempty" json:"allow,omitempty"`
}

// HITLTrigger declares when the runtime suspends for human review.
type HITLTrigger struct {
	Trigger   string `yaml:"trigger" json:"trigger"`
	Condition string `yaml:"condition,omitempty" json:"condition,omitempty"`
	Route     string `yaml:"route,omitempty" json:"route,omitempty"`
}

// ToolName returns the wire name presented to the model. It prefers an explicit
// Name and otherwise derives a model-API-safe name from the ref.
func (t ToolBinding) ToolName() string {
	if n := strings.TrimSpace(t.Name); n != "" {
		return n
	}
	return sanitizeToolName(t.Ref)
}

// toolNamePattern matches names accepted by the Anthropic and OpenAI tool APIs.
var toolNamePattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

// sanitizeToolName maps a ref (which may contain dots or other separators) to a
// deterministic model-API-safe tool name by replacing unsupported characters
// with underscores and truncating to 64 characters.
func sanitizeToolName(ref string) string {
	ref = strings.TrimSpace(ref)
	var b strings.Builder
	for _, r := range ref {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	name := b.String()
	if len(name) > 64 {
		name = name[:64]
	}
	return name
}

// InstructionsSpec is the system prompt: exactly one of ref or text.
type InstructionsSpec struct {
	Ref     string `yaml:"ref,omitempty" json:"ref,omitempty"`
	Version any    `yaml:"version,omitempty" json:"version,omitempty"`
	Text    string `yaml:"text,omitempty" json:"text,omitempty"`
}

// ModelConfig selects the model and provider pass-through options.
type ModelConfig struct {
	Provider        string           `yaml:"provider" json:"provider"`
	Name            string           `yaml:"name" json:"name"`
	Secret          string           `yaml:"secret,omitempty" json:"secret,omitempty"`
	Parameters      *ModelParameters `yaml:"parameters,omitempty" json:"parameters,omitempty"`
	Reasoning       *ReasoningConfig `yaml:"reasoning,omitempty" json:"reasoning,omitempty"`
	ProviderOptions map[string]any   `yaml:"provider_options,omitempty" json:"provider_options,omitempty"`
}

// ModelParameters are passed through to the provider per completion.
type ModelParameters struct {
	Temperature     *float64 `yaml:"temperature,omitempty" json:"temperature,omitempty"`
	TopP            *float64 `yaml:"top_p,omitempty" json:"top_p,omitempty"`
	MaxOutputTokens *int     `yaml:"max_output_tokens,omitempty" json:"max_output_tokens,omitempty"`
	StopSequences   []string `yaml:"stop_sequences,omitempty" json:"stop_sequences,omitempty"`
}

// ReasoningConfig holds provider-mapped reasoning controls.
type ReasoningConfig struct {
	Effort string `yaml:"effort,omitempty" json:"effort,omitempty"`
}

// Limits are enforced across the entire run, not per completion.
type Limits struct {
	MaxTokensPerRun     *int   `yaml:"max_tokens_per_run,omitempty" json:"max_tokens_per_run,omitempty"`
	MaxLoopIterations   *int   `yaml:"max_loop_iterations,omitempty" json:"max_loop_iterations,omitempty"`
	MaxWallClockSeconds *int   `yaml:"max_wall_clock_seconds,omitempty" json:"max_wall_clock_seconds,omitempty"`
	OnLimit             string `yaml:"on_limit,omitempty" json:"on_limit,omitempty"`
}

// OutputSpec is a top-level sibling of spec (not nested under spec).
type OutputSpec struct {
	Format    string      `yaml:"format,omitempty" json:"format,omitempty"`
	Schema    *SchemaSpec `yaml:"schema,omitempty" json:"schema,omitempty"`
	Strict    *bool       `yaml:"strict,omitempty" json:"strict,omitempty"`
	OnInvalid string      `yaml:"on_invalid,omitempty" json:"on_invalid,omitempty"`
}

// SchemaSpec is JSON Schema by ref or inline; exactly one of ref or inline.
type SchemaSpec struct {
	Ref     string         `yaml:"ref,omitempty" json:"ref,omitempty"`
	Version any            `yaml:"version,omitempty" json:"version,omitempty"`
	Inline  map[string]any `yaml:"inline,omitempty" json:"inline,omitempty"`
}
