package manifest

import (
	"encoding/json"
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

// ResolveBundle loads instructions.ref and output.schema.ref relative to the agent YAML directory.
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

	resolved := cloneAgent(agent)

	if ref := strings.TrimSpace(agent.Spec.Instructions.Ref); ref != "" {
		path, err := locateBundleFile(bundleRoot, ref, agent.Spec.Instructions.Version, refKindInstructions)
		if err != nil {
			return nil, err
		}
		text, err := loadInstructionsFile(path)
		if err != nil {
			return nil, fieldResolveError("spec.instructions.ref", err)
		}
		resolved.Spec.Instructions = InstructionsSpec{Text: text}
	}

	if agent.Output != nil && agent.Output.Schema != nil {
		if ref := strings.TrimSpace(agent.Output.Schema.Ref); ref != "" {
			path, err := locateBundleFile(bundleRoot, ref, agent.Output.Schema.Version, refKindSchema)
			if err != nil {
				return nil, err
			}
			inline, err := loadSchemaFile(path)
			if err != nil {
				return nil, fieldResolveError("output.schema.ref", err)
			}
			if resolved.Output == nil {
				resolved.Output = &OutputSpec{}
			}
			resolved.Output.Schema = &SchemaSpec{Inline: inline}
		}
	}

	return &ResolvedAgent{Agent: resolved}, nil
}

type refKind int

const (
	refKindInstructions refKind = iota
	refKindSchema
)

func (k refKind) extensions() []string {
	switch k {
	case refKindSchema:
		return []string{".json"}
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
		return "", resolveNotFoundError(kindPath(kind), ref, version, "ref must be bundle-relative")
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
	return "", resolveNotFoundError(kindPath(kind), ref, version, triedPaths(bundleRoot, tried))
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
	default:
		return "spec.instructions.ref"
	}
}

func resolveNotFoundError(fieldPath, ref string, version any, detail string) error {
	ver, hasVer := formatRefVersion(version)
	msg := fmt.Sprintf("no file found for ref %q", ref)
	if hasVer {
		msg = fmt.Sprintf("no file found for ref %q (version %q)", ref, ver)
	}
	if detail != "" {
		msg += ": " + detail
	}
	return FieldError{Path: fieldPath, Message: msg}
}

func triedPaths(bundleRoot string, rel []string) string {
	if len(rel) == 0 {
		return "no candidates"
	}
	paths := make([]string, len(rel))
	for i, p := range rel {
		paths[i] = filepath.Join(bundleRoot, p)
	}
	return "tried " + strings.Join(paths, ", ")
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
	if len(agent.Metadata.Labels) > 0 {
		out.Metadata.Labels = make(map[string]string, len(agent.Metadata.Labels))
		for k, v := range agent.Metadata.Labels {
			out.Metadata.Labels[k] = v
		}
	}
	if agent.Output != nil {
		o := *agent.Output
		if agent.Output.Schema != nil {
			s := *agent.Output.Schema
			if len(s.Inline) > 0 {
				s.Inline = copyAnyMap(s.Inline)
			}
			o.Schema = &s
		}
		out.Output = &o
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
