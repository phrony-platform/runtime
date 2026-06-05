package manifest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func writeClosureAgent(t *testing.T, dir, name, relPath, instructions string, agents []SubagentBinding) {
	t.Helper()
	agent := &Agent{
		APIVersion: APIVersionV1,
		Kind:       KindAgent,
		Metadata: AgentMetadata{
			Name:      name,
			Namespace: "support",
			Version:   "1.0.0",
		},
		Spec: AgentSpec{
			Purpose:      "Test agent " + name,
			Instructions: InstructionsSpec{Text: instructions},
			Model: ModelConfig{
				Provider: "anthropic",
				Name:     "claude-sonnet-4-5",
			},
			Agents: agents,
		},
	}
	path := filepath.Join(dir, relPath)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	data, err := marshalAgentYAML(agent)
	if err != nil {
		t.Fatalf("marshal agent: %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write agent: %v", err)
	}
}

func writeBundleManifest(t *testing.T, dir, root string) *BundleManifest {
	t.Helper()
	bundle := &BundleManifest{
		APIVersion: APIVersionV1,
		Kind:       KindBundle,
		Metadata: BundleMetadata{
			Name:      "support",
			Namespace: "support",
		},
		Spec: BundleManifestSpec{Root: root},
	}
	path := filepath.Join(dir, "bundle.yaml")
	data, err := marshalBundleYAML(bundle)
	if err != nil {
		t.Fatalf("marshal bundle: %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write bundle: %v", err)
	}
	return bundle
}

func marshalAgentYAML(agent *Agent) ([]byte, error) {
	return yaml.Marshal(agent)
}

func marshalBundleYAML(bundle *BundleManifest) ([]byte, error) {
	return yaml.Marshal(bundle)
}

func TestWalkBundle_linearClosure(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeClosureAgent(t, dir, "billing", "billing.yaml", "Handle billing.", nil)
	writeClosureAgent(t, dir, "orchestrator", "orchestrator.yaml", "Route tasks.", []SubagentBinding{{
		Ref: "./billing.yaml",
	}})
	bundle := writeBundleManifest(t, dir, "./orchestrator.yaml")

	pkg, err := WalkBundle(dir, bundle)
	if err != nil {
		t.Fatalf("WalkBundle() error = %v", err)
	}
	if pkg.RootChildName != "orchestrator" {
		t.Fatalf("root_child_name = %q, want orchestrator", pkg.RootChildName)
	}
	if len(pkg.Members) != 2 {
		t.Fatalf("len(members) = %d, want 2", len(pkg.Members))
	}
	if pkg.Members[0].ChildName != "orchestrator" || !pkg.Members[0].IsRoot {
		t.Fatalf("first member = %+v, want root orchestrator", pkg.Members[0])
	}
	if pkg.Members[1].ChildName != "billing" {
		t.Fatalf("second member = %+v, want billing", pkg.Members[1])
	}
	if pkg.Members[1].ContentHash == "" {
		t.Fatal("billing content_hash is empty")
	}
	if pkg.Version == "" || !strings.HasPrefix(pkg.Version, "sha256:") {
		t.Fatalf("version = %q, want sha256: prefix", pkg.Version)
	}
	if pkg.Lockfile.Version != pkg.Version {
		t.Fatalf("lockfile.version = %q, want %q", pkg.Lockfile.Version, pkg.Version)
	}

	ctx := NewClosureContext(pkg)
	edge, err := ParseAgentEdgeRef("./billing.yaml", false)
	if err != nil {
		t.Fatalf("ParseAgentEdgeRef: %v", err)
	}
	target, ok := ctx.Lookup(edge)
	if !ok || target.ChildName != "billing" {
		t.Fatalf("Lookup() = %+v, ok=%v, want billing", target, ok)
	}
}

func TestWalkBundle_childChangeBumpsVersion(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeClosureAgent(t, dir, "billing", "billing.yaml", "Handle billing.", nil)
	writeClosureAgent(t, dir, "orchestrator", "orchestrator.yaml", "Route tasks.", []SubagentBinding{{
		Ref: "./billing.yaml",
	}})
	bundle := writeBundleManifest(t, dir, "./orchestrator.yaml")

	first, err := WalkBundle(dir, bundle)
	if err != nil {
		t.Fatalf("WalkBundle() first error = %v", err)
	}

	writeClosureAgent(t, dir, "billing", "billing.yaml", "Handle billing v2.", nil)
	second, err := WalkBundle(dir, bundle)
	if err != nil {
		t.Fatalf("WalkBundle() second error = %v", err)
	}
	if first.Version == second.Version {
		t.Fatalf("version unchanged after child edit: %q", first.Version)
	}
	if first.Members[1].ContentHash == second.Members[1].ContentHash {
		t.Fatal("billing content_hash unchanged after child edit")
	}
}

func TestWalkBundle_rejectsCycle(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeClosureAgent(t, dir, "billing", "billing.yaml", "Billing.", []SubagentBinding{{
		Ref: "./orchestrator.yaml",
	}})
	writeClosureAgent(t, dir, "orchestrator", "orchestrator.yaml", "Route.", []SubagentBinding{{
		Ref: "./billing.yaml",
	}})
	bundle := writeBundleManifest(t, dir, "./orchestrator.yaml")

	_, err := WalkBundle(dir, bundle)
	if err == nil {
		t.Fatal("WalkBundle() error = nil, want cycle error")
	}
	if !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("error = %v, want cycle mention", err)
	}
}

func TestWalkBundle_rejectsDuplicateChildName(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeClosureAgent(t, dir, "billing", "specialists/a.yaml", "A.", nil)
	writeClosureAgent(t, dir, "billing", "specialists/b.yaml", "B.", nil)
	writeClosureAgent(t, dir, "orchestrator", "orchestrator.yaml", "Route.", []SubagentBinding{
		{Ref: "./specialists/a.yaml"},
		{Ref: "./specialists/b.yaml"},
	})
	bundle := writeBundleManifest(t, dir, "./orchestrator.yaml")

	_, err := WalkBundle(dir, bundle)
	if err == nil {
		t.Fatal("WalkBundle() error = nil, want duplicate child_name error")
	}
	if !strings.Contains(err.Error(), "duplicate bundle child_name") {
		t.Fatalf("error = %v, want duplicate child_name", err)
	}
}

func TestWalkBundle_excludesLateBound(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeClosureAgent(t, dir, "orchestrator", "orchestrator.yaml", "Route.", []SubagentBinding{{
		Ref:       "support.billing@1.0.0",
		LateBound: true,
	}})
	bundle := writeBundleManifest(t, dir, "./orchestrator.yaml")

	pkg, err := WalkBundle(dir, bundle)
	if err != nil {
		t.Fatalf("WalkBundle() error = %v", err)
	}
	if len(pkg.Members) != 1 {
		t.Fatalf("len(members) = %d, want 1 (late_bound excluded)", len(pkg.Members))
	}
}

func TestExpandSubagentBindings_closureContextPinsChildName(t *testing.T) {
	t.Parallel()
	agent := &Agent{
		Spec: AgentSpec{
			Agents: []SubagentBinding{{
				Ref: "./billing.yaml",
			}},
		},
	}
	ctx := NewClosureContext(&ClosurePackage{
		Members: []ClosureMember{{
			ChildName: "billing",
			Origin:    ClosureMemberOriginVendored,
			Ref:       "./billing.yaml",
		}},
	})
	if err := expandSubagentBindings(agent, ctx); err != nil {
		t.Fatalf("expandSubagentBindings() error = %v", err)
	}
	if len(agent.Spec.Tools) != 1 {
		t.Fatalf("len(tools) = %d, want 1", len(agent.Spec.Tools))
	}
	got := agent.Spec.Tools[0].Agent
	if got == nil || got.ChildName != "billing" {
		t.Fatalf("agent.child_name = %+v, want billing", got)
	}
}
