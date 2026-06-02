package manifest_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/phrony-platform/runtime/internal/manifest"
)

func TestCompile_inlinesStubScript(t *testing.T) {
	dir := t.TempDir()
	script := `{"turns":[[{"type":"completed","stop_reason":"end_turn"}]]}`
	if err := os.WriteFile(filepath.Join(dir, "stub-script.json"), []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}
	agentPath := filepath.Join(dir, "agent.yaml")
	agentYAML := `
apiVersion: phrony.com/v1
kind: Agent
metadata:
  name: stub-agent
  namespace: demo
  version: 1.0.0
spec:
  purpose: test
  instructions:
    text: hi
  model:
    provider: stub
    name: scripted
`
	if err := os.WriteFile(agentPath, []byte(agentYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	agent, err := manifest.Parse([]byte(agentYAML))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	resolved, err := manifest.Compile(agentPath, agent)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	got := manifest.StubScriptFromAgent(resolved.Agent)
	if strings.TrimSpace(got) != strings.TrimSpace(script) {
		t.Fatalf("stub script = %q, want %q", got, script)
	}
}
