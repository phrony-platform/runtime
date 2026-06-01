package manifest_test

import (
	"path/filepath"
	"strings"
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
	if tb.ToolName() != "approve_payment" {
		t.Fatalf("tool name = %q", tb.ToolName())
	}
	schema := tb.BindingSchema()
	if schema == nil || len(schema.Inline) == 0 {
		t.Fatal("binding schema not inlined")
	}
	if schema.Ref != "" {
		t.Fatalf("binding schema ref = %q, want cleared", schema.Ref)
	}

	foundBoundary := false
	foundAudit := false
	for _, p := range resolved.Agent.Spec.Policies {
		switch p.Name {
		case "large-payment-boundary":
			foundBoundary = true
			if p.Scope != "tool:claims.approve-payment" {
				t.Fatalf("boundary scope = %q", p.Scope)
			}
		case "audit-required":
			foundAudit = true
		}
	}
	if !foundBoundary {
		t.Fatal("compiled large-payment-boundary policy missing")
	}
	if !foundAudit {
		t.Fatal("compiled audit-required policy missing")
	}

	raw, err := resolved.JSON()
	if err != nil {
		t.Fatalf("JSON() error = %v", err)
	}
	if strings.Contains(string(raw), "schemas/payment-input") {
		t.Fatalf("JSON() still contains unresolved schema ref: %s", string(raw))
	}
}

func readTestdataFile(t *testing.T, name string) []byte {
	t.Helper()
	return readTestdata(t, name)
}
