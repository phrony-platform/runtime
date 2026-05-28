package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/phrony-platform/runtime/internal/core"
)

const validAgentManifestYAML = `apiVersion: phrony.dev/v1
kind: Agent

metadata:
  name: echo-agent
  namespace: demo
  version: 1.2.0

spec:
  purpose: Echo user messages as structured JSON.
  instructions:
    ref: prompts/system
  model:
    provider: anthropic
    name: claude-sonnet-4-5

output:
  format: json
  schema:
    ref: schemas/result
`

func TestStatusCommand_success(t *testing.T) {
	addr := startTestRuntimeAddr(t)

	var out bytes.Buffer
	root := NewRootCommand()
	root.SetOut(&out)
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"status", "--runtime-addr", addr})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	outStr := out.String()
	if !strings.Contains(outStr, "ÆÆÆÆÆ") {
		t.Fatalf("output = %q, want ASCII logo", outStr)
	}
	if !strings.Contains(outStr, CLIVersion) {
		t.Fatalf("output = %q, want CLI version", outStr)
	}
	if !strings.Contains(outStr, core.RuntimeVersion) {
		t.Fatalf("output = %q, want runtime version", outStr)
	}
	if !strings.Contains(outStr, "Schema meta") || !strings.Contains(outStr, "1") {
		t.Fatalf("output = %q, want schema meta version", outStr)
	}
	if !strings.Contains(outStr, "● SERVING") {
		t.Fatalf("output = %q, want SERVING health", outStr)
	}
}

func TestRunCommand_unimplemented(t *testing.T) {
	addr := startTestRuntimeAddr(t)

	root := NewRootCommand()
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"run", "sess-1", "--runtime-addr", addr})

	err := root.Execute()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "not implemented on this runtime yet") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDeployCommand_unimplemented(t *testing.T) {
	addr := startTestRuntimeAddr(t)
	manifest := filepath.Join(t.TempDir(), "agent.yaml")
	if err := os.WriteFile(manifest, []byte(validAgentManifestYAML), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	root := NewRootCommand()
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"deploy", "--file", manifest, "--runtime-addr", addr})

	err := root.Execute()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "not implemented on this runtime yet") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDeployCommand_invalidManifest(t *testing.T) {
	manifest := filepath.Join(t.TempDir(), "agent.yaml")
	if err := os.WriteFile(manifest, []byte("{}"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	root := NewRootCommand()
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"deploy", "--file", manifest})

	err := root.Execute()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "invalid manifest") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDeployCommand_readManifestFailed(t *testing.T) {
	root := NewRootCommand()
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"deploy", "--file", filepath.Join(t.TempDir(), "missing.json")})

	err := root.Execute()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "read manifest") {
		t.Fatalf("unexpected error: %v", err)
	}
}
