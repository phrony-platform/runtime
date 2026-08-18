package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/phrony-platform/runtime/internal/manifest"
	"github.com/phrony-platform/runtime/internal/version"
)

const validAgentManifestYAML = `apiVersion: phrony.com/v1
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
	if !strings.Contains(outStr, version.CLIVersion) {
		t.Fatalf("output = %q, want CLI version", outStr)
	}
	if !strings.Contains(outStr, version.RuntimeVersion) {
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

func TestRunsAttachCommand_missingSessionArg(t *testing.T) {
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

func TestRunsLsCommand_success(t *testing.T) {
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
	for _, want := range []string{"sess-await", "sess-done", "awaiting_input", "completed", "RESUMABLE", "yes", "demo/echo-agent@1.2.0", "TARGET"} {
		if !strings.Contains(got, want) {
			t.Fatalf("output = %q, want %q", got, want)
		}
	}
}

func TestSessionsAttachCommand_failedReadOnly(t *testing.T) {
	t.Setenv("PHRONY_NO_TUI", "1")
	addr := startTestRuntimeAddrForRunAttachFailed(t)

	var out, errOut bytes.Buffer
	root := NewRootCommand()
	root.SetOut(&out)
	root.SetErr(&errOut)
	root.SetArgs([]string{"sessions", "attach", "sess-failed", "--runtime-addr", addr})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	got := out.String() + errOut.String()
	if !strings.Contains(got, "session sess-failed started") {
		t.Fatalf("output = %q, want session started", got)
	}
	if !strings.Contains(got, "session failed") {
		t.Fatalf("output = %q, want session failed banner", got)
	}
}

func TestRunsLsCommand_statusFilter(t *testing.T) {
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

func TestRunsLsCommand_allRuns(t *testing.T) {
	addr := startTestRuntimeAddrForRunsListAll(t)

	var out bytes.Buffer
	root := NewRootCommand()
	root.SetOut(&out)
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"sessions", "ls", "--runtime-addr", addr})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "sess-await") {
		t.Fatalf("output = %q, want sess-await", got)
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
	got := out.String()
	if !strings.Contains(got, "session ") || !strings.Contains(got, " started") {
		t.Fatalf("output = %q, want session id started line", got)
	}
}

func TestRunCommand_attach_failure(t *testing.T) {
	t.Setenv("PHRONY_NO_TUI", "1")
	addr := startTestRuntimeAddrForRunAttach(t)

	var out bytes.Buffer
	root := NewRootCommand()
	root.SetOut(&out)
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"run", "demo/echo-agent", "--attach", "--runtime-addr", addr})

	err := root.Execute()
	if err == nil {
		t.Fatal("expected session failure after start, got nil")
	}
	if !strings.Contains(err.Error(), "session failed") && !strings.Contains(err.Error(), "not found") {
		t.Fatalf("err = %v, want session failed or agent version not found", err)
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
	if !strings.Contains(out.String(), " started") {
		t.Fatalf("output = %q, want session started line", out.String())
	}
}

func TestRunCommand_withInput(t *testing.T) {
	addr := startTestRuntimeAddrForRun(t)

	var out bytes.Buffer
	root := NewRootCommand()
	root.SetOut(&out)
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{
		"run", "demo/echo-agent",
		"--input", `{"message":"hello"}`,
		"--runtime-addr", addr,
	})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out.String(), " started") {
		t.Fatalf("output = %q, want session started line", out.String())
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
	root.SetArgs([]string{"agents", "validate", manifest})

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
	root.SetArgs([]string{"agents", "validate", manifest})

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
	root.SetArgs([]string{"agents", "validate", manifest})

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
	root.SetArgs([]string{"agents", "validate", manifest})

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
	root.SetArgs([]string{"agents", "validate", manifest})

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
	root.SetArgs([]string{"agents", "validate", filepath.Join(t.TempDir(), "missing.json")})

	err := root.Execute()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "read manifest") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPublishCommand_withSecretsManifestSucceedsWithoutEnv(t *testing.T) {
	manifest := writeDeployTestBundleWithSecrets(t, t.TempDir())
	t.Setenv("ANTHROPIC_API_KEY", "")
	addr := startTestRuntimeAddrForDeploy(t)

	var out bytes.Buffer
	root := NewRootCommand()
	root.SetOut(&out)
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"agents", "publish", manifest, "--runtime-addr", addr})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out.String(), "demo/echo-agent 1.2.0") {
		t.Fatalf("output = %q, want agent name and version", out.String())
	}
}

func TestPublishCommand_success(t *testing.T) {
	manifest := writeDeployTestBundle(t, t.TempDir())
	addr := startTestRuntimeAddrForDeploy(t)

	var out bytes.Buffer
	root := NewRootCommand()
	root.SetOut(&out)
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"agents", "publish", manifest, "--runtime-addr", addr})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out.String(), "demo/echo-agent 1.2.0") {
		t.Fatalf("output = %q, want agent name and version", out.String())
	}
}

func TestPublishCommand_successWithSecrets(t *testing.T) {
	manifest := writeDeployTestBundleWithSecrets(t, t.TempDir())
	t.Setenv("ANTHROPIC_API_KEY", "sk-test-deploy")
	addr := startTestRuntimeAddrForDeployWithSecrets(t)

	var out bytes.Buffer
	root := NewRootCommand()
	root.SetOut(&out)
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"agents", "publish", manifest, "--runtime-addr", addr})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out.String(), "demo/echo-agent 1.2.0") {
		t.Fatalf("output = %q, want agent name and version", out.String())
	}
}

func TestPublishCommand_dialRuntimeFailed(t *testing.T) {
	manifest := writeDeployTestBundle(t, t.TempDir())

	root := NewRootCommand()
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"agents", "publish", manifest, "--runtime-addr", "127.0.0.1:1"})

	err := root.Execute()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "publish") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPublishCommand_parseManifestFailed(t *testing.T) {
	manifest := filepath.Join(t.TempDir(), "agent.yaml")
	if err := os.WriteFile(manifest, []byte("apiVersion: [\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	root := NewRootCommand()
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"agents", "publish", manifest})

	err := root.Execute()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "parse manifest") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPublishCommand_invalidManifest(t *testing.T) {
	manifest := filepath.Join(t.TempDir(), "agent.yaml")
	if err := os.WriteFile(manifest, []byte("{}"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	root := NewRootCommand()
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"agents", "publish", manifest})

	err := root.Execute()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "invalid manifest") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPublishCommand_unresolvedBundleRef(t *testing.T) {
	manifest := filepath.Join(t.TempDir(), "agent.yaml")
	if err := os.WriteFile(manifest, []byte(validAgentManifestYAML), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	root := NewRootCommand()
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"agents", "publish", manifest})

	err := root.Execute()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "spec.instructions.ref") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBundlesListCommand_success(t *testing.T) {
	addr := startTestRuntimeAddrForBundlesList(t)

	var out bytes.Buffer
	root := NewRootCommand()
	root.SetOut(&out)
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"bundles", "ls", "--runtime-addr", addr})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	outStr := out.String()
	if !strings.Contains(outStr, "bundle-uuid") || !strings.Contains(outStr, "demo") {
		t.Fatalf("output = %q, want bundle table row", outStr)
	}
	if !strings.Contains(outStr, "NAMESPACE") {
		t.Fatalf("output = %q, want table headers", outStr)
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

func TestVersionsListCommand_success(t *testing.T) {
	addr := startTestRuntimeAddrForVersionsList(t)

	var out bytes.Buffer
	root := NewRootCommand()
	root.SetOut(&out)
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"agents", "versions", "demo/echo-agent", "--runtime-addr", addr})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	outStr := out.String()
	if !strings.Contains(outStr, "1.2.0") || !strings.Contains(outStr, "published") {
		t.Fatalf("output = %q, want version row", outStr)
	}
}

func TestDeprecateCommand_success(t *testing.T) {
	addr := startTestRuntimeAddrForDeprecate(t)

	var out bytes.Buffer
	root := NewRootCommand()
	root.SetOut(&out)
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{
		"agents", "deprecate", "demo/echo-agent@1.2.0", "--runtime-addr", addr,
	})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out.String(), "deprecated demo/echo-agent@1.2.0") {
		t.Fatalf("output = %q, want deprecation confirmation", out.String())
	}
}

func TestDeployActivateCommand_success(t *testing.T) {
	addr := startTestRuntimeAddrForDeployActivate(t)

	var out bytes.Buffer
	root := NewRootCommand()
	root.SetOut(&out)
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"agents", "deploy", "demo/echo-agent@1.2.0", "--runtime-addr", addr})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out.String(), "deployed demo/echo-agent@1.2.0") {
		t.Fatalf("output = %q, want deploy confirmation", out.String())
	}
}

func TestActiveCommand_success(t *testing.T) {
	addr := startTestRuntimeAddrForActive(t)

	var out bytes.Buffer
	root := NewRootCommand()
	root.SetOut(&out)
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"agents", "active", "demo/echo-agent", "--runtime-addr", addr})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "demo/echo-agent@1.2.0") {
		t.Fatalf("output = %q, want active version", got)
	}
	if !strings.Contains(got, "alice") {
		t.Fatalf("output = %q, want deploy actor", got)
	}
}

func TestBundleVersionsListCommand_success(t *testing.T) {
	addr := startTestRuntimeAddrForBundleVersionsList(t)

	var out bytes.Buffer
	root := NewRootCommand()
	root.SetOut(&out)
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"bundles", "versions", "demo/payment-desk-hitl", "--runtime-addr", addr})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	outStr := out.String()
	if !strings.Contains(outStr, "1.0.0") || !strings.Contains(outStr, "sha256:abc") || !strings.Contains(outStr, "bv-1") {
		t.Fatalf("output = %q, want version row", outStr)
	}
	if !strings.Contains(outStr, "LOCK_HASH") {
		t.Fatalf("output = %q, want LOCK_HASH column", outStr)
	}
	if !strings.Contains(outStr, "VERSION") {
		t.Fatalf("output = %q, want table headers", outStr)
	}
}

func TestBundleActiveCommand_success(t *testing.T) {
	addr := startTestRuntimeAddrForBundleActive(t)

	var out bytes.Buffer
	root := NewRootCommand()
	root.SetOut(&out)
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"bundles", "active", "demo/payment-desk-hitl", "--runtime-addr", addr})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "demo/payment-desk-hitl@1.0.0") || !strings.Contains(got, "sha256:abc") {
		t.Fatalf("output = %q, want active bundle version", got)
	}
	if !strings.Contains(got, "alice") {
		t.Fatalf("output = %q, want deploy actor", got)
	}
}

func TestBundleVersionsListCommand_invalidBundleName(t *testing.T) {
	root := NewRootCommand()
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"bundles", "versions", "payment-desk-hitl"})

	err := root.Execute()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "namespace/name") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBundleVersionsListCommand_bundleNotFound(t *testing.T) {
	addr := startTestRuntimeAddrForBundleVersionsNotFound(t)

	root := NewRootCommand()
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"bundles", "versions", "demo/missing", "--runtime-addr", addr})

	err := root.Execute()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "bundle demo/missing not found") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBundleVersionsListCommand_empty(t *testing.T) {
	addr := startTestRuntimeAddrForBundleVersionsEmpty(t)

	var out bytes.Buffer
	root := NewRootCommand()
	root.SetOut(&out)
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"bundles", "versions", "demo/payment-desk-hitl", "--runtime-addr", addr})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "VERSION") {
		t.Fatalf("output = %q, want table headers", got)
	}
	if strings.Contains(got, "sha256:") {
		t.Fatalf("output = %q, want no version rows", got)
	}
}

func TestBundleActiveCommand_noDeployment(t *testing.T) {
	addr := startTestRuntimeAddrForBundleActiveNoDeployment(t)

	root := NewRootCommand()
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"bundles", "active", "demo/payment-desk-hitl", "--runtime-addr", addr})

	err := root.Execute()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "no active deployment") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBundleHistoryCommand_bundleNotFound(t *testing.T) {
	addr := startTestRuntimeAddrForBundleHistoryNotFound(t)

	root := NewRootCommand()
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"bundles", "history", "demo/missing", "--runtime-addr", addr})

	err := root.Execute()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "bundle demo/missing not found") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBundleHistoryCommand_success(t *testing.T) {
	addr := startTestRuntimeAddrForBundleHistory(t)

	var out bytes.Buffer
	root := NewRootCommand()
	root.SetOut(&out)
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"bundles", "history", "demo/payment-desk-hitl", "--runtime-addr", addr})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	got := out.String()
	for _, want := range []string{"1.0.1", "sha256:def", "deploy", "alice", "1.0.0", "sha256:abc", "bob", "VERSION", "LOCK_HASH"} {
		if !strings.Contains(got, want) {
			t.Fatalf("output = %q, want %q", got, want)
		}
	}
}

func TestHistoryCommand_success(t *testing.T) {
	addr := startTestRuntimeAddrForHistory(t)

	var out bytes.Buffer
	root := NewRootCommand()
	root.SetOut(&out)
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"agents", "history", "demo/echo-agent", "--runtime-addr", addr})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	got := out.String()
	for _, want := range []string{"1.2.0", "deploy", "alice", "1.0.0", "rollback", "bob", "VERSION"} {
		if !strings.Contains(got, want) {
			t.Fatalf("output = %q, want %q", got, want)
		}
	}
}

func TestRollbackCommand_success(t *testing.T) {
	addr := startTestRuntimeAddrForRollback(t)

	var out bytes.Buffer
	root := NewRootCommand()
	root.SetOut(&out)
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"rollback", "demo/echo-agent", "--runtime-addr", addr})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "rolled back demo/echo-agent to 1.0.0") {
		t.Fatalf("output = %q, want rollback confirmation", got)
	}
	if !strings.Contains(got, "was: 1.2.0") {
		t.Fatalf("output = %q, want previous version", got)
	}
}

func TestRetireCommand_success(t *testing.T) {
	addr := startTestRuntimeAddrForRetire(t)

	var out bytes.Buffer
	root := NewRootCommand()
	root.SetOut(&out)
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"agents", "retire", "demo/echo-agent@1.0.0", "--runtime-addr", addr})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out.String(), "retired demo/echo-agent@1.0.0") {
		t.Fatalf("output = %q, want retire confirmation", out.String())
	}
}

func TestRunsCancelCommand_success(t *testing.T) {
	addr := startTestRuntimeAddrForRunsCancel(t)

	var out bytes.Buffer
	root := NewRootCommand()
	root.SetOut(&out)
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"sessions", "cancel", "run_abc", "--runtime-addr", addr})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out.String(), "cancelled run_abc") {
		t.Fatalf("output = %q, want cancel confirmation", out.String())
	}
}

func TestInitCommand_success(t *testing.T) {
	dir := t.TempDir()

	var out bytes.Buffer
	root := NewRootCommand()
	root.SetOut(&out)
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"init", dir})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out.String(), "created") {
		t.Fatalf("output = %q, want created message", out.String())
	}
	if _, err := os.Stat(filepath.Join(dir, "agent.yaml")); err != nil {
		t.Fatalf("agent.yaml: %v", err)
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

func TestPublishCommand_readManifestFailed(t *testing.T) {
	root := NewRootCommand()
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"agents", "publish", filepath.Join(t.TempDir(), "missing.json")})

	err := root.Execute()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "read manifest") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func writeTestSupportBundle(t *testing.T, dir string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, "specialists"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	billing := `apiVersion: phrony.com/v1
kind: Agent
metadata:
  name: billing
  namespace: support
  version: 1.0.0
spec:
  purpose: Handle billing.
  instructions:
    text: Answer billing questions.
  model:
    provider: anthropic
    name: claude-sonnet-4-5
`
	orchestrator := `apiVersion: phrony.com/v1
kind: Agent
metadata:
  name: orchestrator
  namespace: support
  version: 1.0.0
spec:
  purpose: Route billing tasks.
  instructions:
    text: Delegate billing questions.
  model:
    provider: anthropic
    name: claude-sonnet-4-5
  agents:
    - ref: ./specialists/billing.yaml
      as: ask_billing
      description: Billing specialist
      result: summary
`
	bundleYAML := `apiVersion: phrony.com/v1
kind: Bundle
metadata:
  name: helpdesk
  namespace: support
  version: 1.0.0
spec:
  root: ./orchestrator.yaml
`
	for path, content := range map[string]string{
		"specialists/billing.yaml": billing,
		"orchestrator.yaml":        orchestrator,
		"bundle.yaml":              bundleYAML,
	} {
		if err := os.WriteFile(filepath.Join(dir, path), []byte(content), 0o600); err != nil {
			t.Fatalf("WriteFile %s: %v", path, err)
		}
	}
	return filepath.Join(dir, "bundle.yaml")
}

func TestBundleValidateCommand_success(t *testing.T) {
	bundle := writeTestSupportBundle(t, t.TempDir())

	var out bytes.Buffer
	root := NewRootCommand()
	root.SetOut(&out)
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"bundles", "validate", bundle})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "valid: support/helpdesk sha256:") {
		t.Fatalf("output = %q, want valid bundle line", got)
	}
	if !strings.Contains(got, "members: 2 (root: orchestrator)") {
		t.Fatalf("output = %q, want member summary", got)
	}
}

func TestAgentValidateCommand_rejectsBundleKind(t *testing.T) {
	bundle := writeTestSupportBundle(t, t.TempDir())

	root := NewRootCommand()
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"agents", "validate", bundle})

	err := root.Execute()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "phrony bundles validate") {
		t.Fatalf("error = %v, want bundles validate hint", err)
	}
}

func TestBundleLockCommand_writesLockfile(t *testing.T) {
	bundle := writeTestSupportBundle(t, t.TempDir())

	var out bytes.Buffer
	root := NewRootCommand()
	root.SetOut(&out)
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"bundles", "lock", bundle})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	lockPath := manifest.LockfilePath(bundle)
	committed, err := manifest.ReadLockfile(lockPath)
	if err != nil {
		t.Fatalf("ReadLockfile: %v", err)
	}
	resolved, err := loadResolvedBundle(bundle)
	if err != nil {
		t.Fatalf("loadResolvedBundle: %v", err)
	}
	recomputed := manifest.LockfileFromClosure(resolved.Closure)
	if err := manifest.CompareLockfiles(*committed, recomputed); err != nil {
		t.Fatalf("committed lock does not match closure: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "locked: support/helpdesk "+committed.Version) {
		t.Fatalf("output = %q, want locked line with %s", got, committed.Version)
	}
	if !strings.Contains(got, "members: 2 (root: orchestrator)") {
		t.Fatalf("output = %q, want member summary", got)
	}
}

func TestBundleValidateCommand_withLockInSync(t *testing.T) {
	bundle := writeTestSupportBundle(t, t.TempDir())
	lockBundle(t, bundle)

	var out bytes.Buffer
	root := NewRootCommand()
	root.SetOut(&out)
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"bundles", "validate", bundle})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out.String(), "valid: support/helpdesk sha256:") {
		t.Fatalf("output = %q, want valid bundle line", out.String())
	}
}

func TestBundleValidateCommand_lockDrift(t *testing.T) {
	dir := t.TempDir()
	bundle := writeTestSupportBundle(t, dir)
	lockBundle(t, bundle)

	if err := mutateTestBundleBillingInstructions(t, dir); err != nil {
		t.Fatalf("mutate billing: %v", err)
	}

	root := NewRootCommand()
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"bundles", "validate", bundle})

	if err := root.Execute(); err == nil {
		t.Fatal("expected drift error, got nil")
	} else if !strings.Contains(err.Error(), "drift") {
		t.Fatalf("error = %v, want drift", err)
	}
}

func TestBundleValidateCommand_requireLock(t *testing.T) {
	bundle := writeTestSupportBundle(t, t.TempDir())

	root := NewRootCommand()
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"bundles", "validate", bundle, "--require-lock"})

	err := root.Execute()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "phrony bundles lock") {
		t.Fatalf("error = %v, want bundle lock hint", err)
	}
}

func TestBundlePublishCommand_requiresLock(t *testing.T) {
	bundle := writeTestSupportBundle(t, t.TempDir())

	root := NewRootCommand()
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"bundles", "publish", bundle})

	err := root.Execute()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "phrony bundles lock") {
		t.Fatalf("error = %v, want bundle lock hint", err)
	}
}

func TestAgentPublishCommand_rejectsBundleKind(t *testing.T) {
	bundle := writeTestSupportBundle(t, t.TempDir())

	root := NewRootCommand()
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"agents", "publish", bundle})

	err := root.Execute()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "phrony bundles publish") {
		t.Fatalf("error = %v, want bundles publish hint", err)
	}
}

func TestBundlePublishCommand_rejectsLockDrift(t *testing.T) {
	dir := t.TempDir()
	bundle := writeTestSupportBundle(t, dir)
	lockBundle(t, bundle)

	if err := mutateTestBundleBillingInstructions(t, dir); err != nil {
		t.Fatalf("mutate billing: %v", err)
	}

	root := NewRootCommand()
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"bundles", "publish", bundle})

	if err := root.Execute(); err == nil {
		t.Fatal("expected drift error, got nil")
	} else if !strings.Contains(err.Error(), "drift") {
		t.Fatalf("error = %v, want drift", err)
	}
}

func mutateTestBundleBillingInstructions(t *testing.T, dir string) error {
	t.Helper()
	billingPath := filepath.Join(dir, "specialists", "billing.yaml")
	data, err := os.ReadFile(billingPath)
	if err != nil {
		return err
	}
	updated := strings.Replace(string(data), "Answer billing questions.", "Answer billing questions with updated guidance.", 1)
	return os.WriteFile(billingPath, []byte(updated), 0o600)
}

func lockBundle(t *testing.T, bundlePath string) {
	t.Helper()
	root := NewRootCommand()
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"bundles", "lock", bundlePath})
	if err := root.Execute(); err != nil {
		t.Fatalf("bundle lock: %v", err)
	}
}

func TestBundleDeployCommand_requiresVersionedRef(t *testing.T) {
	root := NewRootCommand()
	root.SetArgs([]string{"bundles", "deploy", "support/helpdesk"})
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})

	err := root.Execute()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "@version") {
		t.Fatalf("unexpected error: %v", err)
	}
}
