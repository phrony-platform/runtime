package manifest_test

import (
	"path/filepath"
	"testing"

	"github.com/phrony-platform/runtime/internal/manifest"
)

func TestResolveBundle_multidocCatalog(t *testing.T) {
	t.Parallel()
	bundle := filepath.Join("testdata", "bundle-multidoc")
	agentPath := filepath.Join(bundle, "agent.yaml")
	data := readTestdataFile(t, filepath.Join("bundle-multidoc", "agent.yaml"))
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

	tb := resolved.Agent.Spec.Tools[0]
	if tb.Ref != "claims.approve-payment" {
		t.Fatalf("tool ref = %q, want claims.approve-payment", tb.Ref)
	}
	if tb.Version != "1.2.0" {
		t.Fatalf("tool version = %q, want pinned 1.2.0", tb.Version)
	}
}

func readTestdataFile(t *testing.T, name string) []byte {
	t.Helper()
	return readTestdata(t, name)
}
