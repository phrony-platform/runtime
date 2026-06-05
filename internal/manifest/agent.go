package manifest

import (
	"regexp"
	"strings"
)

const KindAgent = "Agent"

func isCompiledPolicySnapshot(agent *Agent) bool {
	if agent == nil || agent.Metadata.Annotations == nil {
		return false
	}
	return agent.Metadata.Annotations[AnnotationPoliciesCompiled] == "true"
}

// AnnotationPoliciesCompiled marks a resolved deploy snapshot whose spec.policies
// were inlined from kind: Policy documents at publish. Authoring manifests must not set it.
const AnnotationPoliciesCompiled = "phrony.com/policies-compiled"

// Agent is the v1 Agent document (Kubernetes-style envelope).
type Agent struct {
	APIVersion string                      `yaml:"apiVersion" json:"apiVersion"`
	Kind       string                      `yaml:"kind" json:"kind"`
	Metadata   AgentMetadata               `yaml:"metadata" json:"metadata"`
	Secrets    map[string]SecretDefinition `yaml:"secrets,omitempty" json:"secrets,omitempty"`
	Spec       AgentSpec                   `yaml:"spec" json:"spec"`
	Output     *OutputSpec                 `yaml:"output,omitempty" json:"output,omitempty"`
}

// DocumentKind returns the manifest kind.
func (a *Agent) DocumentKind() string {
	if a == nil {
		return ""
	}
	return a.Kind
}

// AgentMetadata holds identity and versioning for an Agent.
type AgentMetadata struct {
	Name        string              `yaml:"name" json:"name"`
	Namespace   string              `yaml:"namespace" json:"namespace"`
	Version     string              `yaml:"version" json:"version"`
	Owner       string              `yaml:"owner,omitempty" json:"owner,omitempty"`
	Governance  *GovernanceMetadata `yaml:"governance,omitempty" json:"governance,omitempty"`
	Labels      map[string]string   `yaml:"labels,omitempty" json:"labels,omitempty"`
	Annotations map[string]string   `yaml:"annotations,omitempty" json:"annotations,omitempty"`
}

// AgentSpec is the behavior envelope for an Agent.
type AgentSpec struct {
	Purpose         string             `yaml:"purpose" json:"purpose"`
	Instructions    InstructionsSpec   `yaml:"instructions" json:"instructions"`
	Model           ModelConfig        `yaml:"model" json:"model"`
	Tools           []ToolBinding      `yaml:"tools,omitempty" json:"tools,omitempty"`
	MCPServers      []MCPServerSpec    `yaml:"mcp_servers,omitempty" json:"mcp_servers,omitempty"`
	// Agents is the authoring-only sugar block for agent-to-agent delegation. Each
	// entry compiles down to an ordinary spec.tools binding with Agent set at publish
	// time and is cleared from the resolved snapshot.
	Agents          []SubagentBinding  `yaml:"agents,omitempty" json:"agents,omitempty"`
	DefaultPolicies []PolicyAttachment `yaml:"default_policies,omitempty" json:"default_policies,omitempty"`
	// Policies holds compiled rules on resolved snapshots only (see AnnotationPoliciesCompiled).
	Policies []PolicySpec `yaml:"policies,omitempty" json:"policies,omitempty"`
	Limits   *Limits      `yaml:"limits,omitempty" json:"limits,omitempty"`
}

// MCPServerSpec declares a remote MCP server the runtime connects to natively to
// back MCP tool bindings. Only the remote Streamable HTTP transport is supported.
type MCPServerSpec struct {
	Name      string         `yaml:"name" json:"name"`
	URL       string         `yaml:"url" json:"url"`
	Transport string         `yaml:"transport,omitempty" json:"transport,omitempty"`
	Auth      *MCPServerAuth `yaml:"auth,omitempty" json:"auth,omitempty"`
}

// MCPServerAuth describes how the runtime authenticates to an MCP server using a
// Phrony secret. Scheme "bearer" sends Authorization: Bearer <secret>; scheme
// "header" sends the secret value in the named custom header.
type MCPServerAuth struct {
	Scheme string `yaml:"scheme" json:"scheme"`
	Secret string `yaml:"secret" json:"secret"`
	Header string `yaml:"header,omitempty" json:"header,omitempty"`
}

// MCP transports (remote only in v1).
const MCPTransportStreamableHTTP = "streamable_http"

// MCP auth schemes.
const (
	MCPAuthSchemeBearer = "bearer"
	MCPAuthSchemeHeader = "header"
)

// ResolvedTransport returns the configured transport, defaulting to streamable_http.
func (s MCPServerSpec) ResolvedTransport() string {
	if t := strings.TrimSpace(s.Transport); t != "" {
		return t
	}
	return MCPTransportStreamableHTTP
}

// ToolBinding declares one tool the agent may call. The runtime presents the
// tool contract to the model and dispatches authorized calls to the
// application that owns the implementation; the runtime never executes tool
// code itself. Each binding references a tool by stable identifier (ref).
type ToolBinding struct {
	Ref string `yaml:"ref" json:"ref"`
	// As is the wire name presented to the model when set; otherwise derived from ref.
	As          string      `yaml:"as,omitempty" json:"as,omitempty"`
	Description string      `yaml:"description,omitempty" json:"description,omitempty"`
	InputSchema *SchemaSpec `yaml:"input_schema,omitempty" json:"input_schema,omitempty"`
	// Version is the pinned tool contract version (set at publish from the Tool document).
	Version string `yaml:"version,omitempty" json:"version,omitempty"`
	// SideEffectClass classifies mutability for dispatch and recovery policy.
	SideEffectClass string `yaml:"side_effect_class,omitempty" json:"side_effect_class,omitempty"`
	// MCP, when set, routes this binding to a declared spec.mcp_servers entry
	// instead of the worker registry.
	MCP *ToolMCPBinding `yaml:"mcp,omitempty" json:"mcp,omitempty"`
	// Agent, when set, routes this binding to a nested child agent session
	// instead of the worker registry. Compiled-only: produced by expanding a
	// spec.agents entry at publish and never set on authoring manifests.
	Agent *ToolAgentBinding `yaml:"agent,omitempty" json:"agent,omitempty"`
	// Policies attaches Policy documents by logical id or bundle file ref.
	Policies []PolicyAttachment `yaml:"policies,omitempty" json:"policies,omitempty"`
}

// ToolMCPBinding marks a ToolBinding as backed by a remote MCP server tool.
// Server names a declared spec.mcp_servers entry; Tool is the remote MCP tool
// name and defaults to the binding wire name when empty.
type ToolMCPBinding struct {
	Server string `yaml:"server" json:"server"`
	Tool   string `yaml:"tool,omitempty" json:"tool,omitempty"`
}

// SubagentBinding is one authoring-only spec.agents entry declaring another
// agent this agent may call as a tool. It compiles to a ToolBinding with Agent
// set at bundle publish time.
type SubagentBinding struct {
	// Ref is a bundle-local path (./specialists/billing.yaml), a pinned external
	// namespace.name@version, or a floating namespace.name only when late_bound
	// is true.
	Ref string `yaml:"ref" json:"ref"`
	// As is the wire name presented to the parent model; defaults from ref.
	As          string      `yaml:"as,omitempty" json:"as,omitempty"`
	Description string      `yaml:"description,omitempty" json:"description,omitempty"`
	InputSchema *SchemaSpec `yaml:"input_schema,omitempty" json:"input_schema,omitempty"`
	// Result selects how the child output is returned to the parent model:
	// summary (default) returns the final output; full includes the step trace.
	Result string `yaml:"result,omitempty" json:"result,omitempty"`
	// Policies attaches Policy documents gating the delegation call.
	Policies []PolicyAttachment `yaml:"policies,omitempty" json:"policies,omitempty"`
	// LateBound opts into live active-deployment resolution at call time and
	// excludes the edge from bundle closure walks.
	LateBound bool `yaml:"late_bound,omitempty" json:"late_bound,omitempty"`
}

// ToolAgentBinding marks a ToolBinding as backed by a nested child agent
// session. It is compiled from a spec.agents entry and pins the resolved
// target agent identity plus the requested result shape.
type ToolAgentBinding struct {
	// Identity fields are used for tracing and late_bound fallback resolution.
	Namespace string `yaml:"namespace" json:"namespace"`
	Name      string `yaml:"name" json:"name"`
	Version   string `yaml:"version,omitempty" json:"version,omitempty"`
	// ChildName is the vendored member name within a bundle closure.
	ChildName string `yaml:"child_name,omitempty" json:"child_name,omitempty"`
	// LateBound resolves to the active deployment at call time when true.
	LateBound bool `yaml:"late_bound,omitempty" json:"late_bound,omitempty"`
	// AgentVersionID is the compiled-only frozen target snapshot UUID set at
	// bundle publish; runtime dispatch uses this directly when LateBound is false.
	AgentVersionID string `yaml:"agent_version_id,omitempty" json:"agent_version_id,omitempty"`
	// Result selects how the child output is returned (summary | full).
	Result string `yaml:"result,omitempty" json:"result,omitempty"`
}

// Subagent result shapes presented back to the parent model.
const (
	SubagentResultSummary = "summary"
	SubagentResultFull    = "full"
)

// ResolvedResult returns the configured result shape, defaulting to summary.
func (b ToolAgentBinding) ResolvedResult() string {
	if r := strings.TrimSpace(b.Result); r != "" {
		return r
	}
	return SubagentResultSummary
}

// LogicalID returns the target agent catalog identifier namespace.name.
func (b ToolAgentBinding) LogicalID() string {
	return LogicalID(b.Namespace, b.Name)
}

// Side effect classes (whitepaper / runtime dispatch).
const (
	SideEffectReadOnly           = "read_only"
	SideEffectIdempotentWrite    = "idempotent_write"
	SideEffectNonIdempotentWrite = "non_idempotent_write"
	SideEffectIrreversibleAction = "irreversible_action"
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
	Name                  string         `yaml:"name" json:"name"`
	Scope                 string         `yaml:"scope,omitempty" json:"scope,omitempty"`
	Action                string         `yaml:"action,omitempty" json:"action,omitempty"`
	Allow                 []string       `yaml:"allow,omitempty" json:"allow,omitempty"`
	Conditions            map[string]any `yaml:"conditions,omitempty" json:"conditions,omitempty"`
	AuthorityRef          string         `yaml:"authority_ref,omitempty" json:"authority_ref,omitempty"`
	ApprovalsRequired     int            `yaml:"approvals_required,omitempty" json:"approvals_required,omitempty"`
	Reason                string         `yaml:"reason,omitempty" json:"reason,omitempty"`
	OnReject              string         `yaml:"on_reject,omitempty" json:"on_reject,omitempty"`
	OnModify              string         `yaml:"on_modify,omitempty" json:"on_modify,omitempty"`
	ComprehensionRequired bool           `yaml:"comprehension_required,omitempty" json:"comprehension_required,omitempty"`
	Timeout               *PolicyTimeout `yaml:"timeout,omitempty" json:"timeout,omitempty"`
	Runtime               map[string]any `yaml:"runtime,omitempty" json:"runtime,omitempty"`
}

// BindingSchema returns the tool argument schema when set on the binding.
func (t ToolBinding) BindingSchema() *SchemaSpec {
	return t.InputSchema
}

// ToolName returns the wire name presented to the model.
func (t ToolBinding) ToolName() string {
	if n := strings.TrimSpace(t.As); n != "" {
		return n
	}
	return sanitizeToolName(t.Ref)
}

// IsMCP reports whether the binding is backed by a remote MCP server.
func (t ToolBinding) IsMCP() bool {
	return t.MCP != nil
}

// IsAgent reports whether the binding is backed by a nested child agent.
func (t ToolBinding) IsAgent() bool {
	return t.Agent != nil
}

// DispatchRef returns the logical ref used as the tool dispatch routing key
// (ToolCall.Tool). It mirrors how the executor derives the ref from the binding
// so MCP/agent routing and the executor agree on the same key.
func (t ToolBinding) DispatchRef() string {
	if t.Agent != nil {
		if id := t.Agent.LogicalID(); id != "" {
			return id
		}
	}
	ref := strings.TrimSpace(t.Ref)
	if parsed, err := ParseLogicalRef(ref); err == nil {
		return parsed.Raw
	}
	return ref
}

// MCPToolName returns the remote MCP tool name to call, defaulting to the wire
// name when the binding does not override it. It returns "" for non-MCP bindings.
func (t ToolBinding) MCPToolName() string {
	if t.MCP == nil {
		return ""
	}
	if n := strings.TrimSpace(t.MCP.Tool); n != "" {
		return n
	}
	return t.ToolName()
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
	MaxHITLWaitMinutes  *int   `yaml:"max_hitl_wait_minutes,omitempty" json:"max_hitl_wait_minutes,omitempty"`
	// MaxSubagentDepth caps agent-to-agent delegation nesting across the run
	// tree, preventing unbounded recursion (e.g. A->B->A). Enforced runtime-wide.
	MaxSubagentDepth *int   `yaml:"max_subagent_depth,omitempty" json:"max_subagent_depth,omitempty"`
	OnLimit          string `yaml:"on_limit,omitempty" json:"on_limit,omitempty"`
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
