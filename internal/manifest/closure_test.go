package manifest

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

type stubExternalResolver map[string]string

func (r stubExternalResolver) ResolveExternal(_ context.Context, namespace, name, version string) (string, error) {
	key := LogicalID(namespace, name) + "@" + version
	hash, ok := r[key]
	if !ok {
		return "", fmt.Errorf("not found: %s", key)
	}
	return hash, nil
}

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
			Version:   "1.0.0",
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

func TestWalkBundle_includesPinnedExternal(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeClosureAgent(t, dir, "orchestrator", "orchestrator.yaml", "Route.", []SubagentBinding{{
		Ref: "support.billing@1.2.0",
	}})
	bundle := writeBundleManifest(t, dir, "./orchestrator.yaml")

	pkg, err := WalkBundle(dir, bundle)
	if err != nil {
		t.Fatalf("WalkBundle() error = %v", err)
	}
	if len(pkg.Members) != 2 {
		t.Fatalf("len(members) = %d, want 2", len(pkg.Members))
	}
	if pkg.Members[0].ChildName != "orchestrator" || pkg.Members[0].Origin != ClosureMemberOriginVendored {
		t.Fatalf("root member = %+v, want vendored orchestrator", pkg.Members[0])
	}
	ext := pkg.Members[1]
	if ext.ChildName != "billing" || ext.Origin != ClosureMemberOriginExternal {
		t.Fatalf("external member = %+v, want external billing", ext)
	}
	if ext.ContentHash != "" {
		t.Fatalf("external content_hash = %q, want empty", ext.ContentHash)
	}
	if ext.Namespace != "support" || ext.Name != "billing" || ext.Version != "1.2.0" {
		t.Fatalf("external identity = %s/%s@%s, want support/billing@1.2.0", ext.Namespace, ext.Name, ext.Version)
	}
	lockExt := pkg.Lockfile.Members[1]
	if lockExt.ContentHash != "" {
		t.Fatalf("lock external content_hash = %q, want empty", lockExt.ContentHash)
	}
	if lockExt.Namespace != "support" || lockExt.Name != "billing" || lockExt.Version != "1.2.0" {
		t.Fatalf("lock external identity = %s/%s@%s, want support/billing@1.2.0",
			lockExt.Namespace, lockExt.Name, lockExt.Version)
	}
}

func TestEnrichExternalMembers_requiresResolver(t *testing.T) {
	t.Parallel()
	pkg := &ClosurePackage{
		Members: []ClosureMember{{
			ChildName: "billing",
			Origin:    ClosureMemberOriginExternal,
			Namespace: "support",
			Name:      "billing",
			Version:   "1.2.0",
		}},
	}
	if err := EnrichExternalMembers(context.Background(), pkg, nil); err == nil {
		t.Fatal("EnrichExternalMembers() error = nil, want missing resolver error")
	}
}

func TestEnrichExternalMembers_resolvesContentHash(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeClosureAgent(t, dir, "orchestrator", "orchestrator.yaml", "Route.", []SubagentBinding{{
		Ref: "support.billing@1.2.0",
	}})
	bundle := writeBundleManifest(t, dir, "./orchestrator.yaml")

	pkg, err := WalkBundle(dir, bundle)
	if err != nil {
		t.Fatalf("WalkBundle() error = %v", err)
	}
	beforeVersion := pkg.Version

	resolver := stubExternalResolver{"support.billing@1.2.0": "resolved-hash"}
	if err := EnrichExternalMembers(context.Background(), pkg, resolver); err != nil {
		t.Fatalf("EnrichExternalMembers() error = %v", err)
	}
	ext := pkg.Members[1]
	if ext.ContentHash != "resolved-hash" {
		t.Fatalf("external content_hash = %q, want resolved-hash", ext.ContentHash)
	}
	if pkg.Lockfile.Members[1].ContentHash != "resolved-hash" {
		t.Fatalf("lock external content_hash = %q, want resolved-hash", pkg.Lockfile.Members[1].ContentHash)
	}
	if pkg.Version == beforeVersion {
		t.Fatal("bundle version unchanged after enriching external content_hash")
	}
	if pkg.Lockfile.Version != pkg.Version {
		t.Fatalf("lockfile.version = %q, want %q", pkg.Lockfile.Version, pkg.Version)
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

func TestPinCompiledAgentBindings_pinsAgentVersionID(t *testing.T) {
	t.Parallel()
	agent := &Agent{
		Metadata: AgentMetadata{
			Annotations: map[string]string{AnnotationPoliciesCompiled: "true"},
		},
		Spec: AgentSpec{
			Tools: []ToolBinding{{
				Ref:             "./billing.yaml",
				As:              "ask_billing",
				InputSchema:     &SchemaSpec{Inline: map[string]any{"type": "object"}},
				SideEffectClass: SideEffectNonIdempotentWrite,
				Agent: &ToolAgentBinding{
					ChildName: "billing",
				},
			}},
		},
	}
	ctx := NewClosureContext(&ClosurePackage{
		Members: []ClosureMember{{
			ChildName:      "billing",
			Origin:         ClosureMemberOriginVendored,
			Ref:            "./billing.yaml",
			Namespace:      "support",
			Name:           "billing",
			AgentVersionID: "ver-billing",
		}},
	})
	if err := PinCompiledAgentBindings(agent, ctx); err != nil {
		t.Fatalf("PinCompiledAgentBindings() error = %v", err)
	}
	got := agent.Spec.Tools[0].Agent
	if got == nil || got.AgentVersionID != "ver-billing" {
		t.Fatalf("agent_version_id = %+v, want ver-billing", got)
	}
	if got.Namespace != "support" || got.Name != "billing" {
		t.Fatalf("identity = %s/%s, want support/billing", got.Namespace, got.Name)
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
