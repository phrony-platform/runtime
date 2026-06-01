package manifest

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// ResolvedAgent is an Agent document with bundle-relative refs loaded into inline fields.
type ResolvedAgent struct {
	Agent *Agent
}

// JSON returns the resolved agent as canonical JSON for deploy transport.
func (r *ResolvedAgent) JSON() ([]byte, error) {
	if r == nil || r.Agent == nil {
		return nil, fmt.Errorf("resolved agent is nil")
	}
	return json.Marshal(r.Agent)
}

// ResolveBundle loads bundle-relative refs for instructions, schemas, tools, and policies.
// Ref paths are bundle-relative; default extensions are .yaml/.yml/.md for prompts and .json for schemas.
// When version is set, candidates include "<ref>-<version>.<ext>", "<ref>/<version>.<ext>", and "<ref>/v<version>.<ext>".
func ResolveBundle(agentPath string, agent *Agent) (*ResolvedAgent, error) {
	if agent == nil {
		return nil, fmt.Errorf("agent is nil")
	}
	absAgent, err := filepath.Abs(agentPath)
	if err != nil {
		return nil, fmt.Errorf("agent path: %w", err)
	}
	bundleRoot := filepath.Dir(absAgent)

	bundle, err := LoadBundle(bundleRoot)
	if err != nil {
		return nil, err
	}

	resolved := cloneAgent(agent)
	resolver := &bundleResolver{
		bundleRoot: bundleRoot,
		bundle:     bundle,
	}

	if err := resolver.resolveInstructions(resolved); err != nil {
		return nil, err
	}
	if err := resolver.resolveOutputSchema(resolved); err != nil {
		return nil, err
	}
	if err := resolver.resolveTools(resolved); err != nil {
		return nil, err
	}
	if err := resolver.resolvePolicies(resolved); err != nil {
		return nil, err
	}

	return &ResolvedAgent{Agent: resolved}, nil
}

type bundleResolver struct {
	bundleRoot string
	bundle     *Bundle
}

func (r *bundleResolver) resolveInstructions(agent *Agent) error {
	ref := strings.TrimSpace(agent.Spec.Instructions.Ref)
	if ref == "" {
		return nil
	}
	path, err := locateBundleFile(r.bundleRoot, ref, agent.Spec.Instructions.Version, refKindInstructions)
	if err != nil {
		return err
	}
	text, err := loadInstructionsFile(path)
	if err != nil {
		return fieldResolveError("spec.instructions.ref", err)
	}
	agent.Spec.Instructions = InstructionsSpec{Text: text}
	return nil
}

func (r *bundleResolver) resolveOutputSchema(agent *Agent) error {
	if agent.Output == nil || agent.Output.Schema == nil {
		return nil
	}
	ref := strings.TrimSpace(agent.Output.Schema.Ref)
	if ref == "" {
		return nil
	}
	inline, err := r.resolveSchema(agent.Output.Schema, "output.schema")
	if err != nil {
		return err
	}
	if agent.Output == nil {
		agent.Output = &OutputSpec{}
	}
	agent.Output.Schema = &SchemaSpec{Inline: inline}
	return nil
}

func (r *bundleResolver) resolveTools(agent *Agent) error {
	for i := range agent.Spec.Tools {
		fieldBase := fmt.Sprintf("spec.tools[%d]", i)
		if err := r.resolveToolBinding(&agent.Spec.Tools[i], fieldBase); err != nil {
			return err
		}
	}
	return nil
}

func (r *bundleResolver) resolveToolBinding(tb *ToolBinding, fieldBase string) error {
	parsed, err := ParseLogicalRef(tb.Ref)
	if err == nil {
		tool, ok := r.bundle.Tool(parsed.Raw)
		switch {
		case ok:
			if err := r.mergeToolCatalog(tb, parsed, tool, fieldBase); err != nil {
				return err
			}
		case strings.TrimSpace(parsed.Constraint) != "":
			return FieldError{
				Path:    fieldBase + ".ref",
				Message: fmt.Sprintf("tool %q not found in bundle tools/", parsed.Raw),
			}
		}
	}
	if err := r.resolveBindingSchema(tb, fieldBase+".input_schema"); err != nil {
		return retargetFieldError(err, fieldBase+".parameters")
	}
	return nil
}

func (r *bundleResolver) mergeToolCatalog(tb *ToolBinding, parsed ParsedLogicalRef, tool *Tool, fieldBase string) error {
	if err := parsed.MatchesVersion(tool.Metadata.Version); err != nil {
		return FieldError{Path: fieldBase + ".ref", Message: err.Error()}
	}
	tb.Ref = parsed.Raw
	if strings.TrimSpace(tb.Version) == "" {
		tb.Version = strings.TrimSpace(tool.Metadata.Version)
	}
	if strings.TrimSpace(tb.Description) == "" {
		tb.Description = strings.TrimSpace(tool.Spec.Description)
	}
	if strings.TrimSpace(tb.SideEffectClass) == "" {
		tb.SideEffectClass = strings.TrimSpace(tool.Spec.SideEffectClass)
	}
	if tb.BindingSchema() == nil && tool.Spec.InputSchema != nil {
		schema := cloneSchemaSpec(tool.Spec.InputSchema)
		tb.Parameters = schema
	}
	if len(tb.Policies) == 0 && len(tool.Spec.DefaultPolicies) > 0 {
		for _, id := range tool.Spec.DefaultPolicies {
			tb.Policies = append(tb.Policies, PolicyAttachment{Logical: strings.TrimSpace(id)})
		}
	}
	toolCopy := cloneTool(tool)
	if err := r.resolveToolDocSchemas(toolCopy, fieldBase); err != nil {
		return err
	}
	if tb.BindingSchema() == nil && toolCopy.Spec.InputSchema != nil && len(toolCopy.Spec.InputSchema.Inline) > 0 {
		tb.Parameters = cloneSchemaSpec(toolCopy.Spec.InputSchema)
	}
	return nil
}

func (r *bundleResolver) resolveToolDocSchemas(tool *Tool, fieldBase string) error {
	if tool.Spec.InputSchema != nil {
		inline, err := r.resolveSchema(tool.Spec.InputSchema, fieldBase+".tool.input_schema")
		if err != nil {
			return err
		}
		tool.Spec.InputSchema = &SchemaSpec{Inline: inline}
	}
	if tool.Spec.OutputSchema != nil {
		inline, err := r.resolveSchema(tool.Spec.OutputSchema, fieldBase+".tool.output_schema")
		if err != nil {
			return err
		}
		tool.Spec.OutputSchema = &SchemaSpec{Inline: inline}
	}
	return nil
}

func (r *bundleResolver) resolveBindingSchema(tb *ToolBinding, fieldPath string) error {
	schema := tb.BindingSchema()
	if schema == nil {
		return nil
	}
	if strings.TrimSpace(schema.Ref) == "" {
		return nil
	}
	inline, err := r.resolveSchema(schema, fieldPath)
	if err != nil {
		return err
	}
	if tb.InputSchema != nil {
		tb.InputSchema = &SchemaSpec{Inline: inline}
	} else {
		tb.Parameters = &SchemaSpec{Inline: inline}
	}
	return nil
}

func (r *bundleResolver) resolveSchema(spec *SchemaSpec, fieldPath string) (map[string]any, error) {
	ref := strings.TrimSpace(spec.Ref)
	if ref == "" {
		if len(spec.Inline) > 0 {
			return copyAnyMap(spec.Inline), nil
		}
		return nil, FieldError{Path: fieldPath, Message: "must set exactly one of ref or inline"}
	}
	path, err := locateBundleFile(r.bundleRoot, ref, spec.Version, refKindSchema)
	if err != nil {
		return nil, retargetFieldError(err, fieldPath+".ref")
	}
	inline, err := loadSchemaFile(path)
	if err != nil {
		return nil, fieldResolveError(fieldPath+".ref", err)
	}
	return inline, nil
}

func (r *bundleResolver) resolvePolicies(agent *Agent) error {
	seen := policyNameSet(agent.Spec.Policies)
	attachments := append([]PolicyAttachment(nil), agent.Spec.DefaultPolicies...)
	for i := range agent.Spec.Tools {
		attachments = append(attachments, agent.Spec.Tools[i].Policies...)
	}
	for _, att := range attachments {
		if att.IsZero() {
			continue
		}
		policy, fieldPath, err := r.lookupPolicy(att)
		if err != nil {
			return err
		}
		legacy, ok := policy.legacyPolicySpec()
		if !ok {
			continue
		}
		if _, exists := seen[legacy.Name]; exists {
			continue
		}
		agent.Spec.Policies = append(agent.Spec.Policies, legacy)
		seen[legacy.Name] = struct{}{}
		_ = fieldPath
	}
	return nil
}

func (r *bundleResolver) lookupPolicy(att PolicyAttachment) (*Policy, string, error) {
	if file := strings.TrimSpace(att.File); file != "" {
		path, err := locateBundleFile(r.bundleRoot, file, nil, refKindPolicy)
		if err != nil {
			return nil, "policy", retargetFieldError(err, "policy ref")
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, "policy", fieldResolveError("policy ref", err)
		}
		policy, err := ParsePolicy(data)
		if err != nil {
			return nil, "policy", fieldResolveError("policy ref", err)
		}
		if !IsSupportedAPIVersion(policy.APIVersion) {
			NormalizeAPIVersionForPolicy(policy)
		}
		return policy, "policy ref", nil
	}
	logical := strings.TrimSpace(att.Logical)
	policy, ok := r.bundle.Policy(logical)
	if !ok {
		return nil, logical, FieldError{
			Path:    "policy",
			Message: fmt.Sprintf("policy %q not found in bundle policies/", logical),
		}
	}
	return policy, logical, nil
}

// NormalizeAPIVersionForPolicy rewrites deprecated apiVersion on Policy documents.
func NormalizeAPIVersionForPolicy(policy *Policy) bool {
	if policy == nil {
		return false
	}
	if policy.APIVersion == APIVersionV1Deprecated {
		policy.APIVersion = APIVersionV1
		return true
	}
	return false
}

func policyNameSet(policies []PolicySpec) map[string]struct{} {
	seen := make(map[string]struct{}, len(policies))
	for _, p := range policies {
		if name := strings.TrimSpace(p.Name); name != "" {
			seen[name] = struct{}{}
		}
	}
	return seen
}

type refKind int

const (
	refKindInstructions refKind = iota
	refKindSchema
	refKindPolicy
)

func (k refKind) extensions() []string {
	switch k {
	case refKindSchema:
		return []string{".json"}
	case refKindPolicy:
		return []string{".yaml", ".yml"}
	default:
		return []string{".yaml", ".yml", ".md"}
	}
}

func locateBundleFile(bundleRoot, ref string, version any, kind refKind) (string, error) {
	ref = filepath.FromSlash(strings.TrimSpace(ref))
	if ref == "" {
		return "", fmt.Errorf("ref is empty")
	}
	if filepath.IsAbs(ref) {
		return "", resolveNotFoundError(kindPath(kind), ref, version, "", "ref must be bundle-relative")
	}

	exts := kind.extensions()
	if ext := filepath.Ext(ref); ext != "" {
		exts = []string{ext}
		ref = strings.TrimSuffix(ref, ext)
	}

	var tried []string
	for _, candidate := range bundleRefCandidates(ref, version, exts) {
		abs := filepath.Join(bundleRoot, candidate)
		if !isPathWithinRoot(bundleRoot, abs) {
			continue
		}
		tried = append(tried, candidate)
		if _, err := os.Stat(abs); err == nil {
			return abs, nil
		}
	}
	detail := ""
	if len(tried) == 0 {
		detail = "ref path escapes bundle directory"
	}
	return "", resolveNotFoundError(kindPath(kind), ref, version, bundleRoot, detail)
}

func bundleRefCandidates(ref string, version any, exts []string) []string {
	ver, hasVersion := formatRefVersion(version)
	var out []string
	add := func(base string) {
		for _, ext := range exts {
			out = append(out, base+ext)
		}
	}
	if hasVersion {
		add(ref + "-" + ver)
		add(filepath.Join(ref, ver))
		add(filepath.Join(ref, "v"+ver))
		return out
	}
	add(ref)
	return out
}

func formatRefVersion(version any) (string, bool) {
	if version == nil {
		return "", false
	}
	switch v := version.(type) {
	case string:
		if strings.TrimSpace(v) == "" {
			return "", false
		}
		return strings.TrimSpace(v), true
	case int:
		return fmt.Sprintf("%d", v), true
	case int64:
		return fmt.Sprintf("%d", v), true
	case float64:
		if v == float64(int64(v)) {
			return fmt.Sprintf("%d", int64(v)), true
		}
		return fmt.Sprintf("%v", v), true
	case uint64:
		return fmt.Sprintf("%d", v), true
	default:
		return fmt.Sprintf("%v", v), true
	}
}

func kindPath(kind refKind) string {
	switch kind {
	case refKindSchema:
		return "output.schema.ref"
	case refKindPolicy:
		return "policy.ref"
	default:
		return "spec.instructions.ref"
	}
}

func resolveNotFoundError(fieldPath, ref string, version any, bundleRoot, detail string) error {
	ver, hasVer := formatRefVersion(version)
	msg := fmt.Sprintf("ref %q not found", ref)
	if hasVer {
		msg = fmt.Sprintf("ref %q (version %q) not found", ref, ver)
	}
	if bundleRoot != "" && detail == "" {
		msg += fmt.Sprintf(" in bundle directory %s", bundleRoot)
	}
	if detail != "" {
		msg += ": " + detail
	}
	return FieldError{Path: fieldPath, Message: msg}
}

func isPathWithinRoot(root, path string) bool {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(absRoot, absPath)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func loadInstructionsFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".md":
		return strings.TrimSpace(string(data)), nil
	case ".yaml", ".yml":
		var doc struct {
			Text    string `yaml:"text"`
			Content string `yaml:"content"`
		}
		if err := yaml.Unmarshal(data, &doc); err != nil {
			return "", fmt.Errorf("parse prompt YAML: %w", err)
		}
		if text := strings.TrimSpace(doc.Text); text != "" {
			return text, nil
		}
		if text := strings.TrimSpace(doc.Content); text != "" {
			return text, nil
		}
		var asString string
		if err := yaml.Unmarshal(data, &asString); err == nil && strings.TrimSpace(asString) != "" {
			return strings.TrimSpace(asString), nil
		}
		return strings.TrimSpace(string(data)), nil
	default:
		return strings.TrimSpace(string(data)), nil
	}
}

func loadSchemaFile(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var schema map[string]any
	if err := json.Unmarshal(data, &schema); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}
	if len(schema) == 0 {
		return nil, fmt.Errorf("schema must be a non-empty JSON object")
	}
	return schema, nil
}

func fieldResolveError(path string, err error) FieldError {
	return FieldError{Path: path, Message: err.Error()}
}

// retargetFieldError rewrites the Path of a FieldError so bundle-resolution
// errors report the caller's field path instead of the generic ref kind path.
func retargetFieldError(err error, path string) error {
	var fe FieldError
	if errors.As(err, &fe) {
		fe.Path = path
		return fe
	}
	return err
}

func cloneAgent(agent *Agent) *Agent {
	out := *agent
	if agent.Spec.Limits != nil {
		limits := *agent.Spec.Limits
		out.Spec.Limits = &limits
	}
	if agent.Spec.Model.Parameters != nil {
		params := *agent.Spec.Model.Parameters
		out.Spec.Model.Parameters = &params
	}
	if agent.Spec.Model.Reasoning != nil {
		reasoning := *agent.Spec.Model.Reasoning
		out.Spec.Model.Reasoning = &reasoning
	}
	if len(agent.Spec.Model.ProviderOptions) > 0 {
		out.Spec.Model.ProviderOptions = copyAnyMap(agent.Spec.Model.ProviderOptions)
	}
	if len(agent.Spec.DefaultPolicies) > 0 {
		out.Spec.DefaultPolicies = append([]PolicyAttachment(nil), agent.Spec.DefaultPolicies...)
	}
	if len(agent.Spec.Tools) > 0 {
		out.Spec.Tools = make([]ToolBinding, len(agent.Spec.Tools))
		for i, t := range agent.Spec.Tools {
			tool := t
			if t.Parameters != nil {
				tool.Parameters = cloneSchemaSpec(t.Parameters)
			}
			if t.InputSchema != nil {
				tool.InputSchema = cloneSchemaSpec(t.InputSchema)
			}
			if len(t.Policies) > 0 {
				tool.Policies = append([]PolicyAttachment(nil), t.Policies...)
			}
			out.Spec.Tools[i] = tool
		}
	}
	if len(agent.Spec.Policies) > 0 {
		out.Spec.Policies = make([]PolicySpec, len(agent.Spec.Policies))
		for i, p := range agent.Spec.Policies {
			policy := p
			if len(p.Allow) > 0 {
				policy.Allow = append([]string(nil), p.Allow...)
			}
			out.Spec.Policies[i] = policy
		}
	}
	if len(agent.Spec.HITL) > 0 {
		out.Spec.HITL = append([]HITLTrigger(nil), agent.Spec.HITL...)
	}
	if len(agent.Metadata.Labels) > 0 {
		out.Metadata.Labels = make(map[string]string, len(agent.Metadata.Labels))
		for k, v := range agent.Metadata.Labels {
			out.Metadata.Labels[k] = v
		}
	}
	if len(agent.Secrets) > 0 {
		out.Secrets = make(map[string]SecretDefinition, len(agent.Secrets))
		for k, v := range agent.Secrets {
			out.Secrets[k] = v
		}
	}
	if agent.Output != nil {
		o := *agent.Output
		if agent.Output.Schema != nil {
			o.Schema = cloneSchemaSpec(agent.Output.Schema)
		}
		out.Output = &o
	}
	return &out
}

func cloneSchemaSpec(s *SchemaSpec) *SchemaSpec {
	if s == nil {
		return nil
	}
	out := *s
	if len(s.Inline) > 0 {
		out.Inline = copyAnyMap(s.Inline)
	}
	return &out
}

func cloneTool(t *Tool) *Tool {
	if t == nil {
		return nil
	}
	out := *t
	if t.Spec.InputSchema != nil {
		out.Spec.InputSchema = cloneSchemaSpec(t.Spec.InputSchema)
	}
	if t.Spec.OutputSchema != nil {
		out.Spec.OutputSchema = cloneSchemaSpec(t.Spec.OutputSchema)
	}
	if len(t.Spec.DefaultPolicies) > 0 {
		out.Spec.DefaultPolicies = append([]string(nil), t.Spec.DefaultPolicies...)
	}
	if len(t.Metadata.Labels) > 0 {
		out.Metadata.Labels = make(map[string]string, len(t.Metadata.Labels))
		for k, v := range t.Metadata.Labels {
			out.Metadata.Labels[k] = v
		}
	}
	return &out
}

func copyAnyMap(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
