package manifest_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/phrony-platform/runtime/internal/manifest"
)

func TestResolveBundle_fullAgent(t *testing.T) {
	t.Parallel()
	bundle := filepath.Join("testdata", "bundle")
	agentPath := filepath.Join(bundle, "agent.yaml")

	data := readTestdata(t, "full-agent.yaml")
	agent, err := manifest.Parse(data)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if err := manifest.Validate(agent); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	resolved, err := manifest.ResolveBundle(agentPath, agent)
	if err != nil {
		t.Fatalf("ResolveBundle() error = %v", err)
	}
	if resolved.Agent.Spec.Instructions.Ref != "" {
		t.Fatalf("instructions.ref = %q, want cleared after resolve", resolved.Agent.Spec.Instructions.Ref)
	}
	if !strings.Contains(resolved.Agent.Spec.Instructions.Text, "echo the user message") {
		t.Fatalf("instructions.text = %q, want loaded prompt", resolved.Agent.Spec.Instructions.Text)
	}
	if resolved.Agent.Output == nil || resolved.Agent.Output.Schema == nil {
		t.Fatal("output.schema missing")
	}
	if resolved.Agent.Output.Schema.Ref != "" {
		t.Fatalf("output.schema.ref = %q, want cleared", resolved.Agent.Output.Schema.Ref)
	}
	if resolved.Agent.Output.Schema.Inline["type"] != "object" {
		t.Fatalf("output.schema.inline = %v", resolved.Agent.Output.Schema.Inline)
	}

	raw, err := resolved.JSON()
	if err != nil {
		t.Fatalf("JSON() error = %v", err)
	}
	if !strings.Contains(string(raw), `"text"`) || !strings.Contains(string(raw), `"inline"`) {
		t.Fatalf("JSON() = %s, want resolved instructions.text and schema.inline", string(raw))
	}
	// Bundle file refs (instructions, schemas) must be resolved away; tool refs
	// are stable identifiers and are retained.
	if strings.Contains(string(raw), "prompts/system") || strings.Contains(string(raw), "schemas/result") {
		t.Fatalf("JSON() = %s, want bundle refs cleared after resolve", string(raw))
	}
}

func TestResolveBundle_inlineInstructionsAndSchema(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	agentPath := filepath.Join(dir, "agent.yaml")
	manifestYAML := `apiVersion: phrony.com/v1
kind: Agent
metadata:
  name: inline
  namespace: demo
  version: 1.0.0
spec:
  purpose: test
  instructions:
    text: hello
  model:
    provider: anthropic
    name: claude
output:
  format: json
  schema:
    inline:
      type: object
`
	if err := os.WriteFile(agentPath, []byte(manifestYAML), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	agent, err := manifest.Parse([]byte(manifestYAML))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	resolved, err := manifest.ResolveBundle(agentPath, agent)
	if err != nil {
		t.Fatalf("ResolveBundle() error = %v", err)
	}
	if resolved.Agent.Spec.Instructions.Text != "hello" {
		t.Fatalf("instructions.text = %q", resolved.Agent.Spec.Instructions.Text)
	}
	if resolved.Agent.Output.Schema.Inline["type"] != "object" {
		t.Fatalf("schema.inline = %v", resolved.Agent.Output.Schema.Inline)
	}
}

func TestResolveBundle_missingInstructionsRef(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	agentPath := filepath.Join(dir, "agent.yaml")
	if err := os.WriteFile(agentPath, []byte(readTestdata(t, "minimal.yaml")), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	agent, err := manifest.Parse(readTestdata(t, "minimal.yaml"))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	_, err = manifest.ResolveBundle(agentPath, agent)
	if err == nil {
		t.Fatal("ResolveBundle() = nil, want error")
	}
	var fieldErr manifest.FieldError
	if !errors.As(err, &fieldErr) {
		t.Fatalf("error type = %T, want FieldError", err)
	}
	if fieldErr.Path != "spec.instructions.ref" {
		t.Fatalf("error path = %q", fieldErr.Path)
	}
}

func TestResolveBundle_refWithExtension(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "prompt.md"), []byte("  markdown prompt  "), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	agentPath := filepath.Join(dir, "agent.yaml")
	manifestYAML := `apiVersion: phrony.com/v1
kind: Agent
metadata:
  name: ext
  namespace: demo
  version: 1.0.0
spec:
  purpose: test
  instructions:
    ref: prompt.md
  model:
    provider: anthropic
    name: claude
`
	if err := os.WriteFile(agentPath, []byte(manifestYAML), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	agent, err := manifest.Parse([]byte(manifestYAML))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	resolved, err := manifest.ResolveBundle(agentPath, agent)
	if err != nil {
		t.Fatalf("ResolveBundle() error = %v", err)
	}
	if resolved.Agent.Spec.Instructions.Text != "markdown prompt" {
		t.Fatalf("instructions.text = %q", resolved.Agent.Spec.Instructions.Text)
	}
}

func TestResolveBundle_missingSchemaRef(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	agentPath := filepath.Join(dir, "agent.yaml")
	if err := os.WriteFile(agentPath, []byte(`apiVersion: phrony.com/v1
kind: Agent
metadata:
  name: no-schema
  namespace: demo
  version: 1.0.0
spec:
  purpose: test
  instructions:
    text: ok
  model:
    provider: anthropic
    name: claude
output:
  format: json
  schema:
    ref: schemas/missing
`), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	agent, err := manifest.Parse(readTestdata(t, "minimal.yaml"))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	agent.Spec.Instructions = manifest.InstructionsSpec{Text: "ok"}
	agent.Output = &manifest.OutputSpec{
		Format: "json",
		Schema: &manifest.SchemaSpec{Ref: "schemas/missing"},
	}

	_, err = manifest.ResolveBundle(agentPath, agent)
	if err == nil {
		t.Fatal("ResolveBundle() = nil, want error")
	}
	var fieldErr manifest.FieldError
	if !errors.As(err, &fieldErr) || fieldErr.Path != "output.schema.ref" {
		t.Fatalf("error = %v", err)
	}
}

func TestResolveBundle_versionSubpathLayout(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "prompts", "system"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "prompts", "system", "v3.yaml"), []byte("text: subpath version\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	agentPath := filepath.Join(dir, "agent.yaml")
	manifestYAML := `apiVersion: phrony.com/v1
kind: Agent
metadata:
  name: subpath
  namespace: demo
  version: 1.0.0
spec:
  purpose: test
  instructions:
    ref: prompts/system
    version: 3
  model:
    provider: anthropic
    name: claude
`
	if err := os.WriteFile(agentPath, []byte(manifestYAML), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	agent, err := manifest.Parse([]byte(manifestYAML))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	resolved, err := manifest.ResolveBundle(agentPath, agent)
	if err != nil {
		t.Fatalf("ResolveBundle() error = %v", err)
	}
	if resolved.Agent.Spec.Instructions.Text != "subpath version" {
		t.Fatalf("instructions.text = %q", resolved.Agent.Spec.Instructions.Text)
	}
}

func TestResolveBundle_invalidPromptYAML(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "prompts"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "prompts", "bad.yaml"), []byte("text: [\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	agentPath := filepath.Join(dir, "agent.yaml")
	manifestYAML := `apiVersion: phrony.com/v1
kind: Agent
metadata:
  name: bad-prompt
  namespace: demo
  version: 1.0.0
spec:
  purpose: test
  instructions:
    ref: prompts/bad
  model:
    provider: anthropic
    name: claude
`
	if err := os.WriteFile(agentPath, []byte(manifestYAML), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	agent, err := manifest.Parse([]byte(manifestYAML))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	_, err = manifest.ResolveBundle(agentPath, agent)
	if err == nil {
		t.Fatal("ResolveBundle() = nil, want error")
	}
	if !strings.Contains(err.Error(), "spec.instructions.ref") || !strings.Contains(err.Error(), "parse prompt YAML") {
		t.Fatalf("error = %v", err)
	}
}

func TestResolveBundle_invalidSchemaJSON(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "schemas"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "schemas", "bad.json"), []byte(`["not","object"]`), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	agentPath := filepath.Join(dir, "agent.yaml")
	manifestYAML := `apiVersion: phrony.com/v1
kind: Agent
metadata:
  name: bad-schema
  namespace: demo
  version: 1.0.0
spec:
  purpose: test
  instructions:
    text: ok
  model:
    provider: anthropic
    name: claude
output:
  format: json
  schema:
    ref: schemas/bad
`
	if err := os.WriteFile(agentPath, []byte(manifestYAML), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	agent, err := manifest.Parse([]byte(manifestYAML))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	_, err = manifest.ResolveBundle(agentPath, agent)
	if err == nil {
		t.Fatal("ResolveBundle() = nil, want error")
	}
	if !strings.Contains(err.Error(), "output.schema.ref") {
		t.Fatalf("error = %v", err)
	}
}
