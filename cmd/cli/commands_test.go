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
	if !strings.Contains(outStr, "Schema meta") || !strings.Contains(outStr, "2") {
		t.Fatalf("output = %q, want schema meta version", outStr)
	}
	if !strings.Contains(outStr, "● SERVING") {
		t.Fatalf("output = %q, want SERVING health", outStr)
	}
}

func TestRunCommand_missingAgentArg(t *testing.T) {
	root := NewRootCommand()
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"run"})

	err := root.Execute()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "accepts 1 arg") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunCommand_success(t *testing.T) {
	addr := startTestRuntimeAddrForRun(t)

	var out bytes.Buffer
	root := NewRootCommand()
	root.SetOut(&out)
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"run", "demo/echo-agent", "--runtime-addr", addr})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out.String(), "session ") || !strings.Contains(out.String(), "created") {
		t.Fatalf("output = %q, want created session", out.String())
	}
}

func TestRunCommand_withVersionFlag(t *testing.T) {
	addr := startTestRuntimeAddrForRunWithVersion(t)

	var out bytes.Buffer
	root := NewRootCommand()
	root.SetOut(&out)
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"run", "demo/echo-agent", "-v", "1.2.0", "--runtime-addr", addr})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out.String(), "created") {
		t.Fatalf("output = %q, want created session", out.String())
	}
}

func TestRunCommand_withInput(t *testing.T) {
	addr := startTestRuntimeAddrForRun(t)

	root := NewRootCommand()
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{
		"run", "demo/echo-agent",
		"--input", `{"message":"hello"}`,
		"--runtime-addr", addr,
	})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
}

func TestRunCommand_invalidAgentName(t *testing.T) {
	root := NewRootCommand()
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"run", "echo-agent"})

	err := root.Execute()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "namespace/name") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunCommand_invalidInput(t *testing.T) {
	root := NewRootCommand()
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"run", "demo/echo-agent", "--input", "not-json"})

	err := root.Execute()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "valid JSON") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func writeDeployTestBundle(t *testing.T, dir string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, "prompts"), 0o755); err != nil {
		t.Fatalf("MkdirAll prompts: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "schemas"), 0o755); err != nil {
		t.Fatalf("MkdirAll schemas: %v", err)
	}
	manifest := filepath.Join(dir, "agent.yaml")
	if err := os.WriteFile(manifest, []byte(validAgentManifestYAML), 0o600); err != nil {
		t.Fatalf("WriteFile agent: %v", err)
	}
	prompt := []byte("text: |\n  You are an echo agent.\n")
	if err := os.WriteFile(filepath.Join(dir, "prompts", "system.yaml"), prompt, 0o600); err != nil {
		t.Fatalf("WriteFile prompt: %v", err)
	}
	schema := []byte(`{"type":"object","properties":{"message":{"type":"string"}}}`)
	if err := os.WriteFile(filepath.Join(dir, "schemas", "result.json"), schema, 0o600); err != nil {
		t.Fatalf("WriteFile schema: %v", err)
	}
	return manifest
}

func TestDeployCommand_success(t *testing.T) {
	manifest := writeDeployTestBundle(t, t.TempDir())
	addr := startTestRuntimeAddrForDeploy(t)

	var out bytes.Buffer
	root := NewRootCommand()
	root.SetOut(&out)
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"deploy", manifest, "--runtime-addr", addr})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out.String(), "demo/echo-agent 1.2.0") {
		t.Fatalf("output = %q, want agent name and version", out.String())
	}
}

func TestDeployCommand_dialRuntimeFailed(t *testing.T) {
	manifest := writeDeployTestBundle(t, t.TempDir())

	root := NewRootCommand()
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"deploy", manifest, "--runtime-addr", "127.0.0.1:1"})

	err := root.Execute()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "deploy") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDeployCommand_parseManifestFailed(t *testing.T) {
	manifest := filepath.Join(t.TempDir(), "agent.yaml")
	if err := os.WriteFile(manifest, []byte("apiVersion: [\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	root := NewRootCommand()
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"deploy", manifest})

	err := root.Execute()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "parse manifest") {
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
	root.SetArgs([]string{"deploy", manifest})

	err := root.Execute()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "invalid manifest") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDeployCommand_unresolvedBundleRef(t *testing.T) {
	manifest := filepath.Join(t.TempDir(), "agent.yaml")
	if err := os.WriteFile(manifest, []byte(validAgentManifestYAML), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	root := NewRootCommand()
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"deploy", manifest})

	err := root.Execute()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "spec.instructions.ref") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDeployCommand_readManifestFailed(t *testing.T) {
	root := NewRootCommand()
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"deploy", filepath.Join(t.TempDir(), "missing.json")})

	err := root.Execute()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "read manifest") {
		t.Fatalf("unexpected error: %v", err)
	}
}
