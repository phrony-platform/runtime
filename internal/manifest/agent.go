package manifest

const (
	APIVersionV1 = "phrony.dev/v1"
	KindAgent    = "Agent"
)

// Agent is the v1 Agent document (Kubernetes-style envelope).
type Agent struct {
	APIVersion string        `yaml:"apiVersion" json:"apiVersion"`
	Kind       string        `yaml:"kind" json:"kind"`
	Metadata   AgentMetadata `yaml:"metadata" json:"metadata"`
	Spec       AgentSpec     `yaml:"spec" json:"spec"`
	Output     *OutputSpec   `yaml:"output,omitempty" json:"output,omitempty"`
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
	Limits       *Limits          `yaml:"limits,omitempty" json:"limits,omitempty"`
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
