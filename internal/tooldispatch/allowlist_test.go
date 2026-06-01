package tooldispatch_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/phrony-platform/runtime/internal/tooldispatch"
)

func TestAllowlist_checker(t *testing.T) {
	list, err := tooldispatch.NewAllowlist([]tooldispatch.AllowlistEntry{{
		Agent:              "demo/echo",
		Tool:               "tools.echo",
		Version:            "v1",
		ContractVersion:    "2",
		WorkloadIdentities: []string{"spiffe://demo/worker"},
		ImageDigests:       []string{"sha256:abc"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	check := list.Checker()

	call := tooldispatch.ToolCall{
		AgentKey: "demo/echo",
		Tool:     "tools.echo",
		Version:  "v1",
	}
	worker := &tooldispatch.WorkerInfo{
		WorkloadIdentity: "spiffe://demo/worker",
		ImageDigest:      "sha256:abc",
		ContractVersions: map[string]string{
			tooldispatch.ToolKey("tools.echo", "v1"): "2",
		},
	}
	if err := check(call, worker); err != nil {
		t.Fatalf("expected pass, got %v", err)
	}

	worker.ImageDigest = "sha256:bad"
	err = check(call, worker)
	if !tooldispatch.IsIntegrityError(err) {
		t.Fatalf("expected integrity error, got %v", err)
	}
}

func TestAllowlist_loadFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "allowlist.yaml")
	const doc = `
entries:
  - agent: demo/echo
    tool: tools.echo
    version: v1
    contract_version: "1"
    workload_identities: [worker-a]
    image_digests: [sha256:deadbeef]
`
	if err := os.WriteFile(path, []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("RUNTIME_TOOL_ALLOWLIST", path)
	list, err := tooldispatch.LoadAllowlistFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	entry, ok := list.Lookup("demo/echo", "tools.echo", "v1")
	if !ok {
		t.Fatal("expected entry")
	}
	if entry.ContractVersion != "1" {
		t.Fatalf("contract_version = %q", entry.ContractVersion)
	}
}

func TestAllowlist_missingAgent(t *testing.T) {
	list, err := tooldispatch.NewAllowlist([]tooldispatch.AllowlistEntry{{
		Agent:              "demo/echo",
		Tool:               "echo",
		Version:            "default",
		WorkloadIdentities: []string{"id-a"},
		ImageDigests:       []string{"digest-a"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	check := list.Checker()
	err = check(tooldispatch.ToolCall{
		AgentKey: "other/agent",
		Tool:     "echo",
		Version:  "default",
	}, &tooldispatch.WorkerInfo{WorkloadIdentity: "id-a", ImageDigest: "digest-a"})
	if !tooldispatch.IsIntegrityError(err) {
		t.Fatalf("expected integrity error, got %v", err)
	}
}
