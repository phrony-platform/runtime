package manifest

import (
	"fmt"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"

	"golang.org/x/mod/semver"
)

// ValidateOptions configures agent validation for standalone publish versus
// bundle closure walks.
type ValidateOptions struct {
	// BundleRoot is the absolute bundle directory used to check local agent refs.
	BundleRoot string
	// InBundleClosure allows spec.agents on authoring agents during bundle publish.
	InBundleClosure bool
}

var secretNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)

var (
	validOnLimit    = map[string]struct{}{"halt": {}, "escalate": {}}
	validOnInvalid  = map[string]struct{}{"retry": {}, "repair": {}, "escalate": {}, "fail": {}}
	validOutputFmt  = map[string]struct{}{"text": {}, "json": {}}
	validReasoning  = map[string]struct{}{"low": {}, "medium": {}, "high": {}}
	validSideEffect = map[string]struct{}{
		SideEffectReadOnly:           {},
		SideEffectIdempotentWrite:    {},
		SideEffectNonIdempotentWrite: {},
		SideEffectIrreversibleAction: {},
	}
	validMCPTransport   = map[string]struct{}{MCPTransportStreamableHTTP: {}}
	validMCPAuthScheme  = map[string]struct{}{MCPAuthSchemeBearer: {}, MCPAuthSchemeHeader: {}}
	validSubagentResult = map[string]struct{}{SubagentResultSummary: {}, SubagentResultFull: {}}
)

// Validate checks structural and semantic rules for a parsed Agent document.
func Validate(agent *Agent) error {
	return ValidateAgent(agent, nil)
}

// ValidateAgent checks structural and semantic rules for a parsed Agent document
// with optional bundle-closure context.
func ValidateAgent(agent *Agent, opts *ValidateOptions) error {
	if agent == nil {
		return ValidationErrors{{Path: "", Message: "manifest is nil"}}
	}
	var errs ValidationErrors

	if !IsSupportedAPIVersion(agent.APIVersion) {
		errs = append(errs, FieldError{
			Path:    "apiVersion",
			Message: "must be " + APIVersionV1,
		})
	}
	if agent.Kind != KindAgent {
		errs = append(errs, FieldError{
			Path:    "kind",
			Message: "must be " + KindAgent,
		})
	}

	errs = append(errs, validateMetadata(&agent.Metadata)...)
	if len(agent.Secrets) > 0 {
		errs = append(errs, validateSecrets(agent.Secrets)...)
	}
	errs = append(errs, validateSpec(&agent.Spec, agent.Secrets, agent, opts)...)
	if agent.Output != nil {
		errs = append(errs, validateOutput(agent.Output)...)
	}

	if len(errs) > 0 {
		return errs
	}
	return nil
}

func validateBundleMetadata(m *BundleMetadata) []FieldError {
	var errs []FieldError
	if strings.TrimSpace(m.Name) == "" {
		errs = append(errs, FieldError{Path: "metadata.name", Message: "is required"})
	}
	if strings.TrimSpace(m.Namespace) == "" {
		errs = append(errs, FieldError{Path: "metadata.namespace", Message: "is required"})
	}
	if strings.TrimSpace(m.Version) == "" {
		errs = append(errs, FieldError{Path: "metadata.version", Message: "is required"})
	} else if !isValidSemver(m.Version) {
		errs = append(errs, FieldError{Path: "metadata.version", Message: "must be valid semver"})
	}
	return errs
}

func validateMetadata(m *AgentMetadata) []FieldError {
	var errs []FieldError
	if strings.TrimSpace(m.Name) == "" {
		errs = append(errs, FieldError{Path: "metadata.name", Message: "is required"})
	}
	if strings.TrimSpace(m.Namespace) == "" {
		errs = append(errs, FieldError{Path: "metadata.namespace", Message: "is required"})
	}
	if strings.TrimSpace(m.Version) == "" {
		errs = append(errs, FieldError{Path: "metadata.version", Message: "is required"})
	} else if !isValidSemver(m.Version) {
		errs = append(errs, FieldError{Path: "metadata.version", Message: "must be valid semver"})
	}
	return errs
}

func validateSpec(spec *AgentSpec, secrets map[string]SecretDefinition, agent *Agent, opts *ValidateOptions) []FieldError {
	var errs []FieldError
	if strings.TrimSpace(spec.Purpose) == "" {
		errs = append(errs, FieldError{Path: "spec.purpose", Message: "is required"})
	}
	errs = append(errs, validateInstructions(&spec.Instructions)...)
	errs = append(errs, validateModel(&spec.Model, secrets)...)
	if !isCompiledPolicySnapshot(agent) && len(spec.Policies) > 0 {
		errs = append(errs, FieldError{
			Path:    "spec.policies",
			Message: "must not be set on the Agent; use kind: Policy documents referenced from default_policies or binding policies",
		})
	}
	mcpServers, mcpErrs := validateMCPServers(spec.MCPServers, secrets)
	errs = append(errs, mcpErrs...)
	errs = append(errs, validateTools(spec.Tools, isCompiledPolicySnapshot(agent), mcpServers)...)
	errs = append(errs, validateAgents(spec.Agents, spec.Tools, agent, opts)...)
	errs = append(errs, validateCompiledPolicies(spec.Policies, spec.Tools)...)
	if spec.Limits != nil {
		errs = append(errs, validateLimits(spec.Limits)...)
	}
	return errs
}

func validateTools(tools []ToolBinding, compiledSnapshot bool, mcpServers map[string]struct{}) []FieldError {
	var errs []FieldError
	seenRefs := make(map[string]struct{}, len(tools))
	seenNames := make(map[string]struct{}, len(tools))
	for i, t := range tools {
		base := fmt.Sprintf("spec.tools[%d]", i)
		ref := strings.TrimSpace(t.Ref)
		if ref == "" {
			errs = append(errs, FieldError{Path: base + ".ref", Message: "is required"})
		} else if _, dup := seenRefs[ref]; dup {
			errs = append(errs, FieldError{Path: base + ".ref", Message: "must be unique"})
		} else {
			seenRefs[ref] = struct{}{}
		}

		name := t.ToolName()
		if name == "" {
			errs = append(errs, FieldError{Path: base, Message: "tool name could not be derived from ref; set name"})
		} else if !toolNamePattern.MatchString(name) {
			errs = append(errs, FieldError{Path: base + ".name", Message: "must match [a-zA-Z0-9_-]{1,64}"})
		} else if _, dup := seenNames[name]; dup {
			errs = append(errs, FieldError{Path: base + ".name", Message: "resolves to a duplicate tool name"})
		} else {
			seenNames[name] = struct{}{}
		}

		errs = append(errs, validateToolBindingSchema(&t, base)...)

		if !compiledSnapshot {
			if strings.TrimSpace(t.Version) != "" {
				errs = append(errs, FieldError{
					Path:    base + ".version",
					Message: "must not be set on bindings; pin semver on ref (for example tool@1.0.0)",
				})
			}
		} else if version := strings.TrimSpace(t.Version); version != "" && !isValidSemver(version) {
			errs = append(errs, FieldError{Path: base + ".version", Message: "must be valid semver"})
		}

		if sec := strings.TrimSpace(t.SideEffectClass); sec != "" {
			if _, ok := validSideEffect[sec]; !ok {
				errs = append(errs, FieldError{
					Path:    base + ".side_effect_class",
					Message: "must be one of read_only, idempotent_write, non_idempotent_write, irreversible_action",
				})
			}
		}

		if t.MCP != nil {
			errs = append(errs, validateMCPToolBinding(&t, mcpServers, base)...)
		}
		if t.Agent != nil {
			if !compiledSnapshot {
				errs = append(errs, FieldError{
					Path:    base + ".agent",
					Message: "must not be set on the Agent; declare delegation under spec.agents, which compiles to an agent binding at publish",
				})
			} else {
				errs = append(errs, validateAgentToolBinding(&t, base)...)
			}
		}
	}
	return errs
}

// validateAgentToolBinding checks a compiled agent-backed tool binding produced
// by expanding a spec.agents entry at publish. Authoring manifests never set it
// directly (see validateTools); this validates the resolved snapshot shape.
func validateAgentToolBinding(t *ToolBinding, base string) []FieldError {
	var errs []FieldError
	if strings.TrimSpace(t.Agent.Namespace) == "" {
		errs = append(errs, FieldError{Path: base + ".agent.namespace", Message: "is required"})
	}
	if strings.TrimSpace(t.Agent.Name) == "" {
		errs = append(errs, FieldError{Path: base + ".agent.name", Message: "is required"})
	}
	if !t.Agent.LateBound &&
		strings.TrimSpace(t.Agent.AgentVersionID) == "" &&
		strings.TrimSpace(t.Agent.Version) == "" {
		errs = append(errs, FieldError{
			Path:    base + ".agent",
			Message: "delegation target must be pinned (version or agent_version_id) unless late_bound is true",
		})
	}
	if v := strings.TrimSpace(t.Agent.Version); v != "" && !isValidSemver(v) {
		errs = append(errs, FieldError{Path: base + ".agent.version", Message: "must be valid semver"})
	}
	if r := strings.TrimSpace(t.Agent.Result); r != "" {
		if _, ok := validSubagentResult[r]; !ok {
			errs = append(errs, FieldError{Path: base + ".agent.result", Message: "must be one of summary, full"})
		}
	}
	return errs
}

// validateAgents checks the authoring-only spec.agents delegation block. Each
// entry references a bundle-local agent path or a pinned external catalog id and
// compiles to an agent-backed tool binding at bundle publish, so its wire name
// shares the model-facing tool namespace and must not collide with spec.tools
// bindings or other delegations.
func validateAgents(agents []SubagentBinding, tools []ToolBinding, agent *Agent, opts *ValidateOptions) []FieldError {
	if len(agents) == 0 {
		return nil
	}
	if opts == nil || !opts.InBundleClosure {
		return []FieldError{{
			Path:    "spec.agents",
			Message: "agents with spec.agents must be published via a Bundle",
		}}
	}

	var errs []FieldError
	bundleRoot := ""
	if opts != nil {
		bundleRoot = strings.TrimSpace(opts.BundleRoot)
	}

	seenRefs := make(map[string]struct{}, len(agents))
	// Seed the wire-name set with tool wire names so an agent alias cannot
	// collide with a tool binding presented to the same model.
	seenNames := make(map[string]struct{}, len(tools)+len(agents))
	for _, t := range tools {
		if name := t.ToolName(); name != "" {
			seenNames[name] = struct{}{}
		}
	}
	selfID := LogicalID(agent.Metadata.Namespace, agent.Metadata.Name)
	for i, sub := range agents {
		base := fmt.Sprintf("spec.agents[%d]", i)
		wireSeed := ""
		ref := strings.TrimSpace(sub.Ref)
		if ref == "" {
			errs = append(errs, FieldError{Path: base + ".ref", Message: "is required"})
		} else {
			edge, err := ParseAgentEdgeRef(ref, sub.LateBound)
			if err != nil {
				errs = append(errs, FieldError{Path: base + ".ref", Message: err.Error()})
			} else {
				key := edgeRefKey(edge)
				if _, dup := seenRefs[key]; dup {
					errs = append(errs, FieldError{Path: base + ".ref", Message: "must be unique"})
				} else {
					seenRefs[key] = struct{}{}
				}

				switch edge.Kind {
				case AgentEdgeRefKindLocal:
					errs = append(errs, validateBundleRelativePath(bundleRoot, edge.Path, base+".ref")...)
					wireSeed = localAgentWireSeed(edge.Path)
				case AgentEdgeRefKindExternal:
					raw := edge.External.Raw
					wireSeed = raw
					constraint := strings.TrimSpace(edge.External.Constraint)
					switch {
					case constraint == "" && !sub.LateBound:
						errs = append(errs, FieldError{
							Path:    base + ".ref",
							Message: "external ref must pin @version unless late_bound is true",
						})
					case constraint != "" && !isValidSemver(constraint):
						errs = append(errs, FieldError{
							Path:    base + ".ref",
							Message: "version must be valid semver (for example namespace.name@1.2.0)",
						})
					}
					if selfID != "" && raw == selfID {
						errs = append(errs, FieldError{Path: base + ".ref", Message: "must not reference the declaring agent"})
					}
				}
			}
		}

		name := strings.TrimSpace(sub.As)
		namePath := base + ".as"
		if name == "" {
			namePath = base
			if wireSeed != "" {
				name = sanitizeToolName(wireSeed)
			}
		}
		if name != "" {
			if !toolNamePattern.MatchString(name) {
				errs = append(errs, FieldError{Path: namePath, Message: "must match [a-zA-Z0-9_-]{1,64}"})
			} else if _, dup := seenNames[name]; dup {
				errs = append(errs, FieldError{Path: namePath, Message: "resolves to a duplicate tool name"})
			} else {
				seenNames[name] = struct{}{}
			}
		}

		if sub.InputSchema != nil {
			errs = append(errs, validateSchemaAt(sub.InputSchema, base+".input_schema")...)
		}

		if r := strings.TrimSpace(sub.Result); r != "" {
			if _, ok := validSubagentResult[r]; !ok {
				errs = append(errs, FieldError{Path: base + ".result", Message: "must be one of summary, full"})
			}
		}
	}
	return errs
}

func localAgentWireSeed(path string) string {
	base := filepath.Base(filepath.FromSlash(strings.TrimSpace(path)))
	return strings.TrimSuffix(base, filepath.Ext(base))
}

// validateMCPServers checks the spec.mcp_servers declarations and returns the set
// of declared server names for cross-referencing MCP-backed tool bindings.
func validateMCPServers(servers []MCPServerSpec, secrets map[string]SecretDefinition) (map[string]struct{}, []FieldError) {
	var errs []FieldError
	seen := make(map[string]struct{}, len(servers))
	for i, s := range servers {
		base := fmt.Sprintf("spec.mcp_servers[%d]", i)
		name := strings.TrimSpace(s.Name)
		if name == "" {
			errs = append(errs, FieldError{Path: base + ".name", Message: "is required"})
		} else if _, dup := seen[name]; dup {
			errs = append(errs, FieldError{Path: base + ".name", Message: "must be unique"})
		} else {
			seen[name] = struct{}{}
		}

		if u := strings.TrimSpace(s.URL); u == "" {
			errs = append(errs, FieldError{Path: base + ".url", Message: "is required"})
		} else if parsed, err := url.Parse(u); err != nil || parsed.Host == "" || !strings.EqualFold(parsed.Scheme, "https") {
			errs = append(errs, FieldError{Path: base + ".url", Message: "must be a valid https URL"})
		}

		if transport := strings.TrimSpace(s.Transport); transport != "" {
			if _, ok := validMCPTransport[transport]; !ok {
				errs = append(errs, FieldError{Path: base + ".transport", Message: "must be streamable_http"})
			}
		}

		errs = append(errs, validateMCPAuth(s.Auth, secrets, base+".auth")...)
	}
	return seen, errs
}

func validateMCPAuth(auth *MCPServerAuth, secrets map[string]SecretDefinition, base string) []FieldError {
	if auth == nil {
		return nil
	}
	var errs []FieldError
	scheme := strings.TrimSpace(auth.Scheme)
	if scheme == "" {
		errs = append(errs, FieldError{Path: base + ".scheme", Message: "is required"})
	} else if _, ok := validMCPAuthScheme[scheme]; !ok {
		errs = append(errs, FieldError{Path: base + ".scheme", Message: "must be one of bearer, header"})
	}

	if secret := strings.TrimSpace(auth.Secret); secret == "" {
		errs = append(errs, FieldError{Path: base + ".secret", Message: "is required"})
	} else if _, ok := secrets[secret]; !ok {
		errs = append(errs, FieldError{Path: base + ".secret", Message: "must name a key in secrets"})
	}

	if scheme == MCPAuthSchemeHeader && strings.TrimSpace(auth.Header) == "" {
		errs = append(errs, FieldError{Path: base + ".header", Message: "is required when scheme is header"})
	}
	return errs
}

func validateMCPToolBinding(t *ToolBinding, mcpServers map[string]struct{}, base string) []FieldError {
	var errs []FieldError
	if server := strings.TrimSpace(t.MCP.Server); server == "" {
		errs = append(errs, FieldError{Path: base + ".mcp.server", Message: "is required"})
	} else if _, ok := mcpServers[server]; !ok {
		errs = append(errs, FieldError{
			Path:    base + ".mcp.server",
			Message: "references undeclared spec.mcp_servers entry " + server,
		})
	}
	if t.InputSchema == nil {
		errs = append(errs, FieldError{Path: base + ".input_schema", Message: "is required for MCP-backed tools"})
	}
	if strings.TrimSpace(t.SideEffectClass) == "" {
		errs = append(errs, FieldError{Path: base + ".side_effect_class", Message: "is required for MCP-backed tools"})
	}
	if ref := strings.TrimSpace(t.Ref); ref != "" {
		if parsed, err := ParseLogicalRef(ref); err != nil {
			errs = append(errs, FieldError{Path: base + ".ref", Message: "must be a logical id of the form namespace.name"})
		} else if strings.TrimSpace(parsed.Constraint) != "" {
			errs = append(errs, FieldError{Path: base + ".ref", Message: "must not pin a version constraint for MCP-backed tools"})
		}
	}
	return errs
}

func validateCompiledPolicies(policies []PolicySpec, tools []ToolBinding) []FieldError {
	var errs []FieldError
	seenNames := make(map[string]struct{}, len(policies))
	toolRefs := toolRefSet(tools)
	for i, p := range policies {
		base := fmt.Sprintf("spec.policies[%d]", i)
		name := strings.TrimSpace(p.Name)
		if name == "" {
			errs = append(errs, FieldError{Path: base + ".name", Message: "is required"})
		} else if _, dup := seenNames[name]; dup {
			errs = append(errs, FieldError{Path: base + ".name", Message: "must be unique"})
		} else {
			seenNames[name] = struct{}{}
		}

		hasAction := strings.TrimSpace(p.Action) != ""
		hasAllow := len(p.Allow) > 0
		hasConditions := len(p.Conditions) > 0
		switch {
		case hasAction && hasAllow:
			errs = append(errs, FieldError{
				Path:    base,
				Message: "set action or allow, not both",
			})
		case !hasAction && !hasAllow && !hasConditions:
			errs = append(errs, FieldError{
				Path:    base,
				Message: "must set action, allow, or conditions with a decision",
			})
		case hasConditions && !hasAction && !hasAllow:
			errs = append(errs, FieldError{
				Path:    base + ".conditions",
				Message: "conditions require action or allow",
			})
		case hasAllow:
			for j, v := range p.Allow {
				if strings.TrimSpace(v) == "" {
					errs = append(errs, FieldError{
						Path:    fmt.Sprintf("%s.allow[%d]", base, j),
						Message: "must not be empty",
					})
				}
			}
			if scope := strings.TrimSpace(p.Scope); scope == "" {
				errs = append(errs, FieldError{Path: base + ".scope", Message: "is required when allow is set"})
			} else if ref, ok := toolRefFromScoped(scope); ok {
				if _, known := toolRefs[ref]; !known {
					errs = append(errs, FieldError{
						Path:    base + ".scope",
						Message: "references undeclared tool ref " + ref,
					})
				}
			}
		}
	}
	return errs
}

func toolRefSet(tools []ToolBinding) map[string]struct{} {
	refs := make(map[string]struct{}, len(tools))
	for _, t := range tools {
		if ref := strings.TrimSpace(t.Ref); ref != "" {
			refs[ref] = struct{}{}
		}
	}
	return refs
}

// toolRefFromScoped returns the tool ref when scope/trigger uses the tool: prefix.
func toolRefFromScoped(scoped string) (string, bool) {
	const prefix = "tool:"
	if !strings.HasPrefix(scoped, prefix) {
		return "", false
	}
	ref := strings.TrimSpace(scoped[len(prefix):])
	if ref == "" {
		return "", false
	}
	return ref, true
}

func validateInstructions(in *InstructionsSpec) []FieldError {
	hasRef := strings.TrimSpace(in.Ref) != ""
	hasText := strings.TrimSpace(in.Text) != ""
	switch {
	case hasRef && hasText:
		return []FieldError{{
			Path:    "spec.instructions",
			Message: "set exactly one of ref or text, not both",
		}}
	case !hasRef && !hasText:
		return []FieldError{{
			Path:    "spec.instructions",
			Message: "must set exactly one of ref or text",
		}}
	}
	return nil
}

func validateSecrets(secrets map[string]SecretDefinition) []FieldError {
	var errs []FieldError
	for name, def := range secrets {
		path := "secrets." + name
		if !secretNamePattern.MatchString(name) {
			errs = append(errs, FieldError{
				Path:    path,
				Message: "name must match [a-z][a-z0-9_-]*",
			})
		}
		if strings.TrimSpace(def.FromEnv) == "" {
			errs = append(errs, FieldError{
				Path:    path,
				Message: "must set fromEnv",
			})
		}
	}
	return errs
}

func validateModel(m *ModelConfig, secrets map[string]SecretDefinition) []FieldError {
	var errs []FieldError
	if strings.TrimSpace(m.Provider) == "" {
		errs = append(errs, FieldError{Path: "spec.model.provider", Message: "is required"})
	}
	if strings.TrimSpace(m.Name) == "" {
		errs = append(errs, FieldError{Path: "spec.model.name", Message: "is required"})
	}
	errs = append(errs, validateModelSecret(m, secrets)...)
	if m.Parameters != nil {
		errs = append(errs, validateModelParameters(m.Parameters)...)
	}
	if m.Reasoning != nil && m.Reasoning.Effort != "" {
		if _, ok := validReasoning[m.Reasoning.Effort]; !ok {
			errs = append(errs, FieldError{
				Path:    "spec.model.reasoning.effort",
				Message: "must be one of low, medium, high",
			})
		}
	}
	return errs
}

func validateModelSecret(m *ModelConfig, secrets map[string]SecretDefinition) []FieldError {
	secretRef := strings.TrimSpace(m.Secret)
	if len(secrets) == 0 {
		if secretRef != "" {
			return []FieldError{{
				Path:    "spec.model.secret",
				Message: "must name a key in secrets",
			}}
		}
		return nil
	}
	if secretRef != "" {
		if !secretNamePattern.MatchString(secretRef) {
			return []FieldError{{
				Path:    "spec.model.secret",
				Message: "must match [a-z][a-z0-9_-]*",
			}}
		}
		if _, ok := secrets[secretRef]; !ok {
			return []FieldError{{
				Path:    "spec.model.secret",
				Message: "must name a key in secrets",
			}}
		}
		return nil
	}
	provider := strings.TrimSpace(m.Provider)
	if provider != "" {
		if _, ok := secrets[provider]; ok {
			return nil
		}
	}
	return []FieldError{{
		Path: "spec.model.secret",
		Message: "is required when secrets is set; omit to use the secret named after spec.model.provider, " +
			"or set secret to a secrets key",
	}}
}

func validateModelParameters(p *ModelParameters) []FieldError {
	if p.Temperature != nil && p.TopP != nil {
		return []FieldError{{
			Path:    "spec.model.parameters",
			Message: "set temperature or top_p, not both",
		}}
	}
	return nil
}

func validateLimits(l *Limits) []FieldError {
	var errs []FieldError
	if l.OnLimit != "" {
		if _, ok := validOnLimit[l.OnLimit]; !ok {
			errs = append(errs, FieldError{
				Path:    "spec.limits.on_limit",
				Message: "must be one of halt, escalate",
			})
		}
	}
	if l.MaxSubagentDepth != nil && *l.MaxSubagentDepth < 1 {
		errs = append(errs, FieldError{
			Path:    "spec.limits.max_subagent_depth",
			Message: "must be >= 1",
		})
	}
	return errs
}

func validateOutput(out *OutputSpec) []FieldError {
	var errs []FieldError
	if out.Format != "" {
		if _, ok := validOutputFmt[out.Format]; !ok {
			errs = append(errs, FieldError{
				Path:    "output.format",
				Message: "must be one of text, json",
			})
		}
	}
	if out.Schema != nil {
		errs = append(errs, validateSchema(out.Schema)...)
	}
	if out.OnInvalid != "" {
		if _, ok := validOnInvalid[out.OnInvalid]; !ok {
			errs = append(errs, FieldError{
				Path:    "output.on_invalid",
				Message: "must be one of retry, repair, escalate, fail",
			})
		}
	}
	return errs
}

func validateToolBindingSchema(t *ToolBinding, base string) []FieldError {
	if t.InputSchema == nil {
		return nil
	}
	return validateSchemaAt(t.InputSchema, base+".input_schema")
}

func validateSchema(s *SchemaSpec) []FieldError {
	return validateSchemaAt(s, "output.schema")
}

func validateSchemaAt(s *SchemaSpec, path string) []FieldError {
	hasRef := strings.TrimSpace(s.Ref) != ""
	hasInline := len(s.Inline) > 0
	switch {
	case hasRef && hasInline:
		return []FieldError{{
			Path:    path,
			Message: "set exactly one of ref or inline, not both",
		}}
	case !hasRef && !hasInline:
		return []FieldError{{
			Path:    path,
			Message: "must set exactly one of ref or inline",
		}}
	}
	return nil
}

func isValidSemver(v string) bool {
	candidate := v
	if !strings.HasPrefix(candidate, "v") {
		candidate = "v" + candidate
	}
	return semver.IsValid(candidate)
}
