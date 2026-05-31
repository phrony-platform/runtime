package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/phrony-platform/runtime/internal/version"
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
	if !strings.Contains(outStr, version.Version) {
		t.Fatalf("output = %q, want CLI version", outStr)
	}
	if !strings.Contains(outStr, version.Version) {
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

func TestSessionsAttachCommand_completedReadOnly(t *testing.T) {
	t.Setenv("PHRONY_NO_TUI", "1")
	addr := startTestRuntimeAddrForRunAttachCompleted(t)

	var out bytes.Buffer
	root := NewRootCommand()
	root.SetOut(&out)
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"sessions", "attach", "sess-completed", "--runtime-addr", addr})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "session sess-completed started") {
		t.Fatalf("output = %q, want session started", got)
	}
	if !strings.Contains(got, "session complete") {
		t.Fatalf("output = %q, want session complete", got)
	}
}

func TestSessionsAttachCommand_missingSessionArg(t *testing.T) {
	root := NewRootCommand()
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"sessions", "attach"})

	err := root.Execute()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "accepts 1 arg") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSessionsListCommand_success(t *testing.T) {
	addr := startTestRuntimeAddrForSessionsList(t)

	var out bytes.Buffer
	root := NewRootCommand()
	root.SetOut(&out)
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"sessions", "ls", "demo/echo-agent", "--runtime-addr", addr})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	got := out.String()
	for _, want := range []string{"sess-await", "sess-done", "awaiting_input", "completed", "RESUMABLE", "yes"} {
		if !strings.Contains(got, want) {
			t.Fatalf("output = %q, want %q", got, want)
		}
	}
}

func TestSessionsAttachCommand_failedReadOnly(t *testing.T) {
	t.Setenv("PHRONY_NO_TUI", "1")
	addr := startTestRuntimeAddrForRunAttachFailed(t)

	var out bytes.Buffer
	root := NewRootCommand()
	root.SetOut(&out)
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"sessions", "attach", "sess-failed", "--runtime-addr", addr})

	err := root.Execute()
	if err == nil || !strings.Contains(err.Error(), "session failed") {
		t.Fatalf("err = %v, want session failed", err)
	}
	got := out.String()
	if !strings.Contains(got, "session sess-failed started") {
		t.Fatalf("output = %q, want session started", got)
	}
}

func TestSessionsListCommand_statusFilter(t *testing.T) {
	addr := startTestRuntimeAddrForSessionsListFiltered(t)

	var out bytes.Buffer
	root := NewRootCommand()
	root.SetOut(&out)
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{
		"sessions", "ls", "demo/echo-agent",
		"--status", "awaiting_input",
		"--runtime-addr", addr,
	})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "sess-await") || !strings.Contains(got, "awaiting_input") {
		t.Fatalf("output = %q, want filtered awaiting session", got)
	}
	if strings.Contains(got, "sess-done") {
		t.Fatalf("output = %q, should not include completed session", got)
	}
}

func TestSessionsListCommand_missingAgentArg(t *testing.T) {
	root := NewRootCommand()
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"sessions", "ls"})

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

	err := root.Execute()
	if err == nil {
		t.Fatal("expected session failure after start, got nil")
	}
	if !strings.Contains(err.Error(), "session failed") {
		t.Fatalf("err = %v, want session failed", err)
	}
}

func TestRunCommand_withVersionFlag(t *testing.T) {
	addr := startTestRuntimeAddrForRunWithVersion(t)

	var out bytes.Buffer
	root := NewRootCommand()
	root.SetOut(&out)
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"run", "demo/echo-agent", "-v", "1.2.0", "--runtime-addr", addr})

	err := root.Execute()
	if err == nil {
		t.Fatal("expected session failure after start, got nil")
	}
	if !strings.Contains(err.Error(), "session failed") {
		t.Fatalf("err = %v, want session failed", err)
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

	err := root.Execute()
	if err == nil {
		t.Fatal("expected session failure after start, got nil")
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

const deployTestBundleSecretsYAML = `secrets:
  anthropic:
    fromEnv: ANTHROPIC_API_KEY
`

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

func writeDeployTestBundleWithSecrets(t *testing.T, dir string) string {
	t.Helper()
	manifest := writeDeployTestBundle(t, dir)
	data, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	lines := strings.Split(string(data), "\n")
	var out []string
	inserted := false
	for _, line := range lines {
		if !inserted && strings.HasPrefix(line, "spec:") {
			out = append(out, deployTestBundleSecretsYAML)
			inserted = true
		}
		out = append(out, line)
	}
	if !inserted {
		t.Fatal("could not insert secrets block into test manifest")
	}
	if err := os.WriteFile(manifest, []byte(strings.Join(out, "\n")), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return manifest
}

func TestValidateCommand_success(t *testing.T) {
	manifest := writeDeployTestBundle(t, t.TempDir())

	var out bytes.Buffer
	root := NewRootCommand()
	root.SetOut(&out)
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"validate", manifest})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out.String(), "valid: demo/echo-agent 1.2.0") {
		t.Fatalf("output = %q, want valid agent name and version", out.String())
	}
}

func TestValidateCommand_parseManifestFailed(t *testing.T) {
	manifest := filepath.Join(t.TempDir(), "agent.yaml")
	if err := os.WriteFile(manifest, []byte("apiVersion: [\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	root := NewRootCommand()
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"validate", manifest})

	err := root.Execute()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "parse manifest") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateCommand_invalidManifest(t *testing.T) {
	manifest := filepath.Join(t.TempDir(), "agent.yaml")
	if err := os.WriteFile(manifest, []byte("{}"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	root := NewRootCommand()
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"validate", manifest})

	err := root.Execute()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "invalid manifest") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateCommand_secretEnvWarning(t *testing.T) {
	manifest := writeDeployTestBundleWithSecrets(t, t.TempDir())
	t.Setenv("ANTHROPIC_API_KEY", "")

	var out, errOut bytes.Buffer
	root := NewRootCommand()
	root.SetOut(&out)
	root.SetErr(&errOut)
	root.SetArgs([]string{"validate", manifest})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out.String(), "valid: demo/echo-agent 1.2.0") {
		t.Fatalf("stdout = %q, want valid line", out.String())
	}
	if !strings.Contains(errOut.String(), "warning:") || !strings.Contains(errOut.String(), "ANTHROPIC_API_KEY") {
		t.Fatalf("stderr = %q, want secret env warning", errOut.String())
	}
}

func TestValidateCommand_unresolvedBundleRef(t *testing.T) {
	manifest := filepath.Join(t.TempDir(), "agent.yaml")
	if err := os.WriteFile(manifest, []byte(validAgentManifestYAML), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	root := NewRootCommand()
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"validate", manifest})

	err := root.Execute()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "spec.instructions.ref") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateCommand_readManifestFailed(t *testing.T) {
	root := NewRootCommand()
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"validate", filepath.Join(t.TempDir(), "missing.json")})

	err := root.Execute()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "read manifest") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDeployCommand_missingSecretEnv(t *testing.T) {
	manifest := writeDeployTestBundleWithSecrets(t, t.TempDir())
	t.Setenv("ANTHROPIC_API_KEY", "")

	root := NewRootCommand()
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"deploy", manifest, "--runtime-addr", "127.0.0.1:1"})

	err := root.Execute()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "ANTHROPIC_API_KEY") {
		t.Fatalf("unexpected error: %v", err)
	}
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

func TestDeployCommand_successWithSecrets(t *testing.T) {
	manifest := writeDeployTestBundleWithSecrets(t, t.TempDir())
	t.Setenv("ANTHROPIC_API_KEY", "sk-test-deploy")
	addr := startTestRuntimeAddrForDeployWithSecrets(t)

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

func TestAgentsListCommand_success(t *testing.T) {
	addr := startTestRuntimeAddrForAgentsList(t)

	var out bytes.Buffer
	root := NewRootCommand()
	root.SetOut(&out)
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"agents", "ls", "--runtime-addr", addr})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	outStr := out.String()
	if !strings.Contains(outStr, "agent-uuid") || !strings.Contains(outStr, "demo") {
		t.Fatalf("output = %q, want agent table row", outStr)
	}
	if !strings.Contains(outStr, "NAMESPACE") {
		t.Fatalf("output = %q, want table headers", outStr)
	}
}

func TestAgentsVersionsListCommand_success(t *testing.T) {
	addr := startTestRuntimeAddrForVersionsList(t)

	var out bytes.Buffer
	root := NewRootCommand()
	root.SetOut(&out)
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"agents", "versions", "ls", "demo/echo-agent", "--runtime-addr", addr})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	outStr := out.String()
	if !strings.Contains(outStr, "1.2.0") || !strings.Contains(outStr, "runnable") {
		t.Fatalf("output = %q, want version row", outStr)
	}
}

func TestAgentsVersionsListCommand_missingAgentArg(t *testing.T) {
	root := NewRootCommand()
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"agents", "versions", "ls"})

	err := root.Execute()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "accepts 1 arg") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestAgentsDeprecateCommand_success(t *testing.T) {
	addr := startTestRuntimeAddrForDeprecate(t)

	var out bytes.Buffer
	root := NewRootCommand()
	root.SetOut(&out)
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{
		"agents", "deprecate", "demo/echo-agent", "-v", "1.2.0", "--runtime-addr", addr,
	})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out.String(), "deprecated demo/echo-agent version 1.2.0") {
		t.Fatalf("output = %q, want deprecation confirmation", out.String())
	}
}

func TestAgentsArchiveCommand_success(t *testing.T) {
	addr := startTestRuntimeAddrForArchive(t)

	var out bytes.Buffer
	root := NewRootCommand()
	root.SetOut(&out)
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"agents", "archive", "demo/echo-agent", "--runtime-addr", addr})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out.String(), "archived agent demo/echo-agent") {
		t.Fatalf("output = %q, want archive confirmation", out.String())
	}
}

func TestAgentsArchiveCommand_invalidAgentName(t *testing.T) {
	root := NewRootCommand()
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"agents", "archive", "echo-agent"})

	err := root.Execute()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "namespace/name") {
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
