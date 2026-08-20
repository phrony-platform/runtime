package manifest_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/phrony-platform/runtime/internal/manifest"
	"github.com/santhosh-tekuri/jsonschema/v6"
	"gopkg.in/yaml.v3"
)

const initScaffoldYAML = `# yaml-language-server: $schema=` + manifest.AgentSpecSchemaURL + `
apiVersion: phrony.com/v1
kind: Agent

metadata:
  name: my-agent
  namespace: default
  version: 0.1.0

spec:
  purpose: Describe what this agent does.
  instructions:
    text: |
      You are a helpful assistant.
  model:
    provider: anthropic
    name: claude-sonnet-4-5

output:
  format: text
`

func TestAgentSpecSchema_meta(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile(agentSpecSchemaPath(t))
	if err != nil {
		t.Fatalf("read schema: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("schema JSON: %v", err)
	}
	if got := doc["$id"]; got != manifest.AgentSpecSchemaURL {
		t.Fatalf("$id = %v, want %s", got, manifest.AgentSpecSchemaURL)
	}
	if got := doc["$schema"]; got != "http://json-schema.org/draft-07/schema#" {
		t.Fatalf("$schema = %v, want draft-07", got)
	}
}

func TestAgentSpecSchema_acceptsAuthoringYAML(t *testing.T) {
	t.Parallel()
	sch := compileAgentSpecSchema(t)

	t.Run("init-scaffold", func(t *testing.T) {
		t.Parallel()
		assertSchemaValid(t, sch, []byte(initScaffoldYAML))
	})

	files := []string{
		"minimal.yaml",
		"full-agent.yaml",
		"with-secrets.yaml",
		"bundle/tools/weather.get-forecast.yaml",
		"bundle/tools/routing.assign-queue.yaml",
		"bundle-multidoc/tools/approve-payment.yaml",
		"bundle-multidoc/policies/large-payment-boundary.yaml",
		"bundle-multidoc/policies/audit-required.yaml",
		"bundle-mcp/agent.yaml",
		"bundle-mcp/policies/web-search-allow.yaml",
	}
	for _, file := range files {
		t.Run(file, func(t *testing.T) {
			t.Parallel()
			assertSchemaValid(t, sch, readTestdata(t, file))
		})
	}

	t.Run("e2e-bundle", func(t *testing.T) {
		t.Parallel()
		assertSchemaValid(t, sch, readRepoFile(t, "e2e/scenarios/22-bundle-payment-auto/bundle.yaml"))
	})
	t.Run("e2e-delegation-orchestrator", func(t *testing.T) {
		t.Parallel()
		assertSchemaValid(t, sch, readRepoFile(t, "e2e/scenarios/24-bundle-delegation/orchestrator.yaml"))
	})
}

func TestAgentSpecSchema_rejectsForbiddenFields(t *testing.T) {
	t.Parallel()
	sch := compileAgentSpecSchema(t)

	t.Run("instructions-both", func(t *testing.T) {
		t.Parallel()
		assertSchemaInvalid(t, sch, readTestdata(t, "invalid-instructions-both.yaml"))
	})
	t.Run("schema-both", func(t *testing.T) {
		t.Parallel()
		assertSchemaInvalid(t, sch, readTestdata(t, "invalid-schema-both.yaml"))
	})
	t.Run("secret-plaintext", func(t *testing.T) {
		t.Parallel()
		assertSchemaInvalid(t, sch, readTestdata(t, "invalid-secret-plaintext.yaml"))
	})
	t.Run("envelope", func(t *testing.T) {
		t.Parallel()
		assertSchemaInvalid(t, sch, readTestdata(t, "invalid-envelope.yaml"))
	})

	cases := []struct {
		name string
		yaml string
	}{
		{
			name: "spec.policies",
			yaml: `
apiVersion: phrony.com/v1
kind: Agent
metadata: {name: a, namespace: n, version: 1.0.0}
spec:
  purpose: p
  instructions: {text: hi}
  model: {provider: anthropic, name: claude-sonnet-4-5}
  policies:
    - name: x
      action: deny
`,
		},
		{
			name: "spec.hitl",
			yaml: `
apiVersion: phrony.com/v1
kind: Agent
metadata: {name: a, namespace: n, version: 1.0.0}
spec:
  purpose: p
  instructions: {text: hi}
  model: {provider: anthropic, name: claude-sonnet-4-5}
  hitl:
    - trigger: dispatch:indeterminate
`,
		},
		{
			name: "unknown-top-level",
			yaml: `
apiVersion: phrony.com/v1
kind: Agent
metadata: {name: a, namespace: n, version: 1.0.0}
spec:
  purpose: p
  instructions: {text: hi}
  model: {provider: anthropic, name: claude-sonnet-4-5}
extra: true
`,
		},
		{
			name: "secret-value",
			yaml: `
apiVersion: phrony.com/v1
kind: Agent
metadata: {name: a, namespace: n, version: 1.0.0}
secrets:
  anthropic:
    fromEnv: ANTHROPIC_API_KEY
    value: sk-should-not-commit
spec:
  purpose: p
  instructions: {text: hi}
  model: {provider: anthropic, name: claude-sonnet-4-5}
`,
		},
		{
			name: "tool-name",
			yaml: `
apiVersion: phrony.com/v1
kind: Agent
metadata: {name: a, namespace: n, version: 1.0.0}
spec:
  purpose: p
  instructions: {text: hi}
  model: {provider: anthropic, name: claude-sonnet-4-5}
  tools:
    - ref: t.x
      name: wire
`,
		},
		{
			name: "tool-version",
			yaml: `
apiVersion: phrony.com/v1
kind: Agent
metadata: {name: a, namespace: n, version: 1.0.0}
spec:
  purpose: p
  instructions: {text: hi}
  model: {provider: anthropic, name: claude-sonnet-4-5}
  tools:
    - ref: t.x
      version: 1.0.0
`,
		},
		{
			name: "tool-parameters",
			yaml: `
apiVersion: phrony.com/v1
kind: Agent
metadata: {name: a, namespace: n, version: 1.0.0}
spec:
  purpose: p
  instructions: {text: hi}
  model: {provider: anthropic, name: claude-sonnet-4-5}
  tools:
    - ref: t.x
      parameters:
        inline: {type: object}
`,
		},
		{
			name: "tool-policy",
			yaml: `
apiVersion: phrony.com/v1
kind: Agent
metadata: {name: a, namespace: n, version: 1.0.0}
spec:
  purpose: p
  instructions: {text: hi}
  model: {provider: anthropic, name: claude-sonnet-4-5}
  tools:
    - ref: t.x
      policy: deny
`,
		},
		{
			name: "tool-agent",
			yaml: `
apiVersion: phrony.com/v1
kind: Agent
metadata: {name: a, namespace: n, version: 1.0.0}
spec:
  purpose: p
  instructions: {text: hi}
  model: {provider: anthropic, name: claude-sonnet-4-5}
  tools:
    - ref: t.x
      agent:
        namespace: demo
        name: child
`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assertSchemaInvalid(t, sch, []byte(tc.yaml))
		})
	}
}

func compileAgentSpecSchema(t *testing.T) *jsonschema.Schema {
	t.Helper()
	c := jsonschema.NewCompiler()
	sch, err := c.Compile(agentSpecSchemaPath(t))
	if err != nil {
		t.Fatalf("compile schema: %v", err)
	}
	return sch
}

func assertSchemaValid(t *testing.T, sch *jsonschema.Schema, yamlBytes []byte) {
	t.Helper()
	if err := sch.Validate(yamlToJSON(t, yamlBytes)); err != nil {
		t.Fatalf("schema reject: %v", err)
	}
}

func assertSchemaInvalid(t *testing.T, sch *jsonschema.Schema, yamlBytes []byte) {
	t.Helper()
	if err := sch.Validate(yamlToJSON(t, yamlBytes)); err == nil {
		t.Fatal("schema accept, want reject")
	}
}

func yamlToJSON(t *testing.T, data []byte) any {
	t.Helper()
	var raw any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		t.Fatalf("yaml: %v", err)
	}
	b, err := json.Marshal(raw)
	if err != nil {
		t.Fatalf("json marshal: %v", err)
	}
	inst, err := jsonschema.UnmarshalJSON(bytes.NewReader(b))
	if err != nil {
		t.Fatalf("json unmarshal: %v", err)
	}
	return inst
}

func agentSpecSchemaPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(repoRoot(t), "schemas", "agent-spec", "v1.json")
}

func readRepoFile(t *testing.T, rel string) []byte {
	t.Helper()
	path := filepath.Join(repoRoot(t), filepath.FromSlash(rel))
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return data
}

func repoRoot(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller")
	}
	return filepath.Join(filepath.Dir(thisFile), "..", "..")
}
