package manifest

const (
	APIVersionV1 = "phrony.dev/v1"
	KindAgent    = "Agent"
)

// Agent is the v1 Agent document (Kubernetes-style envelope).
type Agent struct {
	APIVersion string        `yaml:"apiVersion"`
	Kind       string        `yaml:"kind"`
	Metadata   AgentMetadata `yaml:"metadata"`
	Spec       AgentSpec     `yaml:"spec"`
	Output     *OutputSpec   `yaml:"output,omitempty"`
}

// AgentMetadata holds identity and versioning for an Agent.
type AgentMetadata struct {
	Name      string            `yaml:"name"`
	Namespace string            `yaml:"namespace"`
	Version   string            `yaml:"version"`
	Owner     string            `yaml:"owner,omitempty"`
	Labels    map[string]string `yaml:"labels,omitempty"`
}

// AgentSpec is the behavior envelope for an Agent.
type AgentSpec struct {
	Purpose      string           `yaml:"purpose"`
	Instructions InstructionsSpec `yaml:"instructions"`
	Model        ModelConfig      `yaml:"model"`
	Limits       *Limits          `yaml:"limits,omitempty"`
}

// InstructionsSpec is the system prompt: exactly one of ref or text.
type InstructionsSpec struct {
	Ref     string `yaml:"ref,omitempty"`
	Version any    `yaml:"version,omitempty"`
	Text    string `yaml:"text,omitempty"`
}

// ModelConfig selects the model and provider pass-through options.
type ModelConfig struct {
	Provider        string           `yaml:"provider"`
	Name            string           `yaml:"name"`
	Parameters      *ModelParameters `yaml:"parameters,omitempty"`
	Reasoning       *ReasoningConfig `yaml:"reasoning,omitempty"`
	ProviderOptions map[string]any   `yaml:"provider_options,omitempty"`
}

// ModelParameters are passed through to the provider per completion.
type ModelParameters struct {
	Temperature     *float64 `yaml:"temperature,omitempty"`
	TopP            *float64 `yaml:"top_p,omitempty"`
	MaxOutputTokens *int     `yaml:"max_output_tokens,omitempty"`
	StopSequences   []string `yaml:"stop_sequences,omitempty"`
}

// ReasoningConfig holds provider-mapped reasoning controls.
type ReasoningConfig struct {
	Effort string `yaml:"effort,omitempty"`
}

// Limits are enforced across the entire run, not per completion.
type Limits struct {
	MaxTokensPerRun     *int   `yaml:"max_tokens_per_run,omitempty"`
	MaxLoopIterations   *int   `yaml:"max_loop_iterations,omitempty"`
	MaxWallClockSeconds *int   `yaml:"max_wall_clock_seconds,omitempty"`
	OnLimit             string `yaml:"on_limit,omitempty"`
}

// OutputSpec is a top-level sibling of spec (not nested under spec).
type OutputSpec struct {
	Format    string      `yaml:"format,omitempty"`
	Schema    *SchemaSpec `yaml:"schema,omitempty"`
	Strict    *bool       `yaml:"strict,omitempty"`
	OnInvalid string      `yaml:"on_invalid,omitempty"`
}

// SchemaSpec is JSON Schema by ref or inline; exactly one of ref or inline.
type SchemaSpec struct {
	Ref     string         `yaml:"ref,omitempty"`
	Version any            `yaml:"version,omitempty"`
	Inline  map[string]any `yaml:"inline,omitempty"`
}
