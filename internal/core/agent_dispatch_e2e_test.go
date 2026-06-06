package core

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
	"github.com/phrony-platform/runtime/internal/manifest"
	"github.com/phrony-platform/runtime/internal/model"
	"github.com/phrony-platform/runtime/internal/provider"
	"github.com/phrony-platform/runtime/internal/sessionids"
	"github.com/phrony-platform/runtime/internal/store"
	"github.com/phrony-platform/runtime/internal/tooldispatch"
	"gopkg.in/yaml.v3"
)

// These tests drive agent-to-agent delegation end to end against a real Postgres
// ledger using the scripted stub provider (RUNTIME_ENABLE_STUB_PROVIDER). Bundles
// are walked locally, published via PublishBundle (which pins AgentVersionID on
// every closure edge), deployed, and run: a parent agent emits a tool_use for a
// compiled spec.agents binding, the agent dispatcher runs the vendored target in
// a nested child session, and the child's final output flows back as the tool result.

type publishedStubBundle struct {
	bundleID        string
	bundleVersionID string
	lockHash        string
	rootVersionID   string
	memberVersions  map[string]string
}

func writeBundleStubAgent(t *testing.T, dir, relPath, namespace, name, script string, subagents []manifest.SubagentBinding) {
	t.Helper()
	agent := &manifest.Agent{
		APIVersion: manifest.APIVersionV1,
		Kind:       manifest.KindAgent,
		Metadata: manifest.AgentMetadata{
			Namespace: namespace,
			Name:      name,
			Version:   "1.0.0",
		},
		Spec: manifest.AgentSpec{
			Purpose:      "e2e",
			Instructions: manifest.InstructionsSpec{Text: "System."},
			Model:        manifest.ModelConfig{Provider: "stub", Name: "stub"},
			Agents:       subagents,
		},
	}
	path := filepath.Join(dir, relPath)
	agentDir := filepath.Dir(path)
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", agentDir, err)
	}
	data, err := yaml.Marshal(agent)
	if err != nil {
		t.Fatalf("marshal agent %s: %v", name, err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write agent %s: %v", name, err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "stub-script.json"), []byte(script), 0o644); err != nil {
		t.Fatalf("write stub-script.json for %s: %v", name, err)
	}
}

func writeBundleManifest(t *testing.T, dir, namespace, name, root, version string) {
	t.Helper()
	bundle := &manifest.BundleManifest{
		APIVersion: manifest.APIVersionV1,
		Kind:       manifest.KindBundle,
		Metadata: manifest.BundleMetadata{
			Namespace: namespace,
			Name:      name,
			Version:   version,
		},
		Spec: manifest.BundleManifestSpec{Root: root},
	}
	path := filepath.Join(dir, "bundle.yaml")
	data, err := yaml.Marshal(bundle)
	if err != nil {
		t.Fatalf("marshal bundle: %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write bundle: %v", err)
	}
}

func closureToTestMemberPackages(t *testing.T, closure *manifest.ClosurePackage) []*runtimev1.BundleMemberPackage {
	t.Helper()
	if closure == nil {
		t.Fatal("closure is nil")
	}
	members := make([]*runtimev1.BundleMemberPackage, 0, len(closure.Members))
	for _, m := range closure.Members {
		pkg := &runtimev1.BundleMemberPackage{
			ChildName:    m.ChildName,
			Origin:       m.Origin,
			AuthoringRef: m.Ref,
			IsRoot:       m.IsRoot,
		}
		switch m.Origin {
		case manifest.ClosureMemberOriginVendored:
			if m.Resolved == nil {
				t.Fatalf("vendored member %q is missing resolved manifest", m.ChildName)
			}
			raw, err := m.Resolved.JSON()
			if err != nil {
				t.Fatalf("encode vendored member %q: %v", m.ChildName, err)
			}
			pkg.ResolvedManifest = raw
		case manifest.ClosureMemberOriginExternal:
		default:
			t.Fatalf("member %q has unsupported origin %q", m.ChildName, m.Origin)
		}
		members = append(members, pkg)
	}
	return members
}

func publishStubBundle(t *testing.T, srv *runtimeServer, bundleDir, namespace, bundleName string) publishedStubBundle {
	t.Helper()
	ctx := context.Background()
	data, err := os.ReadFile(filepath.Join(bundleDir, "bundle.yaml"))
	if err != nil {
		t.Fatalf("read bundle.yaml: %v", err)
	}
	bundle, err := manifest.ParseBundle(data)
	if err != nil {
		t.Fatalf("ParseBundle: %v", err)
	}
	closure, err := manifest.WalkBundle(bundleDir, bundle)
	if err != nil {
		t.Fatalf("WalkBundle: %v", err)
	}
	members := closureToTestMemberPackages(t, closure)
	bundleJSON, err := json.Marshal(bundle)
	if err != nil {
		t.Fatalf("marshal bundle: %v", err)
	}
	lock := manifest.LockfileFromClosure(closure)
	lockRaw, err := manifest.MarshalLockfile(lock)
	if err != nil {
		t.Fatalf("MarshalLockfile: %v", err)
	}
	resp, err := srv.PublishBundle(ctx, &runtimev1.PublishBundleRequest{
		BundleManifest: bundleJSON,
		Members:        members,
		Actor:          "e2e",
		CommittedLock:  lockRaw,
	})
	if err != nil {
		t.Fatalf("PublishBundle: %v", err)
	}

	memberVersions := make(map[string]string)
	rows, err := srv.db.Query(`
		SELECT child_name, member_version_id
		FROM bundle_members
		WHERE bundle_version_id = $1
	`, resp.GetBundleVersionId())
	if err != nil {
		t.Fatalf("query bundle members: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var childName, versionID string
		if err := rows.Scan(&childName, &versionID); err != nil {
			t.Fatalf("scan bundle member: %v", err)
		}
		memberVersions[childName] = versionID
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("bundle members rows: %v", err)
	}

	rootVersionID := memberVersions[closure.RootChildName]
	if rootVersionID == "" {
		t.Fatalf("root member %q not found in bundle members", closure.RootChildName)
	}

	return publishedStubBundle{
		bundleID:        resp.GetBundleId(),
		bundleVersionID: resp.GetBundleVersionId(),
		lockHash:        resp.GetLockHash(),
		rootVersionID:   rootVersionID,
		memberVersions:  memberVersions,
	}
}

func deployStubBundle(t *testing.T, srv *runtimeServer, namespace, bundleName, lockHash string) {
	t.Helper()
	_, err := srv.DeployBundle(context.Background(), &runtimev1.DeployBundleRequest{
		BundleRef: &runtimev1.BundleRef{
			Namespace: namespace,
			Name:      bundleName,
			Version:   lockHash,
		},
		Actor: "e2e",
	})
	if err != nil {
		t.Fatalf("DeployBundle: %v", err)
	}
}

func newAgentDelegationServer(db *sqlx.DB) *runtimeServer {
	return &runtimeServer{
		db:             db,
		activeSessions: &sync.Map{},
		toolDispatch: &tooldispatch.StreamDispatcher{
			Registry: tooldispatch.NewWorkerRegistry(tooldispatch.RegistryConfig{LeaseTTL: time.Minute}),
		},
	}
}

func driveStubSessionToCompletion(t *testing.T, srv *runtimeServer, agentVersionID string, input json.RawMessage) string {
	t.Helper()
	ctx := context.Background()
	q, err := srv.queries()
	if err != nil {
		t.Fatalf("queries: %v", err)
	}
	sessionID, err := srv.createRunSession(ctx, agentVersionID, nil, input, nil)
	if err != nil {
		t.Fatalf("createRunSession: %v", err)
	}
	ver, err := srv.loadSessionVersion(ctx, q, sessionID, agentVersionID)
	if err != nil {
		t.Fatalf("loadSessionVersion: %v", err)
	}
	if err := srv.runChildSessionToCompletion(ctx, q, sessionID, agentVersionID, ver, input, rootSessionDepth); err != nil {
		t.Fatalf("drive root session: %v", err)
	}
	return sessionID
}

func childSessionRow(t *testing.T, db *sqlx.DB, parentSessionID string) (id string, depth int, status string) {
	t.Helper()
	row := db.QueryRow(`SELECT id, depth, status FROM sessions WHERE parent_session_id = $1`, parentSessionID)
	if err := row.Scan(&id, &depth, &status); err != nil {
		t.Fatalf("load child session of %s: %v", parentSessionID, err)
	}
	return id, depth, status
}

func cleanupBundleDelegationFixture(t *testing.T, db *sqlx.DB, namespace string) {
	t.Helper()
	_, _ = db.Exec(`
		DELETE FROM tool_invocations
		WHERE session_id IN (
			SELECT s.id FROM sessions s
			WHERE s.bundle_version_id IN (
				SELECT bv.id FROM bundle_versions bv
				JOIN bundles b ON b.id = bv.bundle_id
				WHERE b.namespace = $1
			)
			OR s.agent_version_id IN (
				SELECT bm.member_version_id FROM bundle_members bm
				JOIN bundle_versions bv ON bv.id = bm.bundle_version_id
				JOIN bundles b ON b.id = bv.bundle_id
				WHERE b.namespace = $1
			)
		)
	`, namespace)
	_, _ = db.Exec(`
		DELETE FROM sessions
		WHERE bundle_version_id IN (
			SELECT bv.id FROM bundle_versions bv
			JOIN bundles b ON b.id = bv.bundle_id
			WHERE b.namespace = $1
		)
		OR agent_version_id IN (
			SELECT bm.member_version_id FROM bundle_members bm
			JOIN bundle_versions bv ON bv.id = bm.bundle_version_id
			JOIN bundles b ON b.id = bv.bundle_id
			WHERE b.namespace = $1
		)
	`, namespace)
	_, _ = db.Exec(`
		DELETE FROM bundle_deployments
		WHERE bundle_id IN (SELECT id FROM bundles WHERE namespace = $1)
	`, namespace)
	_, _ = db.Exec(`
		DELETE FROM bundle_versions
		WHERE bundle_id IN (SELECT id FROM bundles WHERE namespace = $1)
	`, namespace)
	_, _ = db.Exec(`DELETE FROM bundles WHERE namespace = $1`, namespace)
}

func TestAgentDelegationE2E_closurePinRunsFrozenChild(t *testing.T) {
	t.Setenv("RUNTIME_ENABLE_STUB_PROVIDER", "true")
	db := openToolTestPostgres(t)
	ns := "bundledeleg-" + uuid.NewString()[:8]
	bundleName := "support"
	t.Cleanup(func() { cleanupBundleDelegationFixture(t, db, ns) })

	dir := t.TempDir()
	writeBundleStubAgent(t, dir, "specialist/agent.yaml", ns, "specialist",
		`{"turns":[[{"type":"text_delta","text":"answer from v1"},{"type":"completed","stop_reason":"end_turn"}]]}`, nil)
	writeBundleStubAgent(t, dir, "orchestrator/agent.yaml", ns, "orchestrator",
		`{"turns":[[{"type":"tool_call","name":"ask_specialist","args":{"task":"help"}},{"type":"completed","stop_reason":"tool_use"}],[{"type":"text_delta","text":"orchestrator done"},{"type":"completed","stop_reason":"end_turn"}]]}`,
		[]manifest.SubagentBinding{{Ref: "./specialist/agent.yaml", As: "ask_specialist"}})
	writeBundleManifest(t, dir, ns, bundleName, "./orchestrator/agent.yaml", "1.0.0")

	srv := newAgentDelegationServer(db)
	v1 := publishStubBundle(t, srv, dir, ns, bundleName)
	deployStubBundle(t, srv, ns, bundleName, v1.lockHash)

	// Change the vendored specialist and publish a new bundle version.
	writeBundleStubAgent(t, dir, "specialist/agent.yaml", ns, "specialist",
		`{"turns":[[{"type":"text_delta","text":"answer from v2"},{"type":"completed","stop_reason":"end_turn"}]]}`, nil)
	writeBundleManifest(t, dir, ns, bundleName, "./orchestrator/agent.yaml", "1.0.1")
	v2 := publishStubBundle(t, srv, dir, ns, bundleName)
	if v1.lockHash == v2.lockHash {
		t.Fatal("child change should bump bundle lock hash")
	}

	rootID := driveStubSessionToCompletion(t, srv, v1.rootVersionID, json.RawMessage(`{"message":"please delegate"}`))

	q := store.New(db.DB)
	inv, err := q.GetToolInvocation(context.Background(), soleCallID(t, db, rootID))
	if err != nil {
		t.Fatalf("GetToolInvocation: %v", err)
	}
	if inv.Status != model.ToolInvocationSucceeded {
		t.Fatalf("delegation invocation status = %q, want succeeded", inv.Status)
	}
	var payload map[string]string
	if err := json.Unmarshal(inv.Result, &payload); err != nil {
		t.Fatalf("decode delegation result %s: %v", inv.Result, err)
	}
	if payload["output"] != "answer from v1" {
		t.Fatalf("delegation result output = %q, want pinned v1 answer from active bundle deployment", payload["output"])
	}

	deployStubBundle(t, srv, ns, bundleName, v2.lockHash)
	rootID2 := driveStubSessionToCompletion(t, srv, v2.rootVersionID, json.RawMessage(`{"message":"please delegate again"}`))
	inv2, err := q.GetToolInvocation(context.Background(), soleCallID(t, db, rootID2))
	if err != nil {
		t.Fatalf("GetToolInvocation v2: %v", err)
	}
	var payload2 map[string]string
	if err := json.Unmarshal(inv2.Result, &payload2); err != nil {
		t.Fatalf("decode delegation result v2: %v", err)
	}
	if payload2["output"] != "answer from v2" {
		t.Fatalf("delegation result output after v2 deploy = %q, want answer from v2", payload2["output"])
	}
}

func TestAgentDelegationE2E_happyPath(t *testing.T) {
	t.Setenv("RUNTIME_ENABLE_STUB_PROVIDER", "true")
	db := openToolTestPostgres(t)
	ns := "bundledeleg-" + uuid.NewString()[:8]
	bundleName := "support"
	t.Cleanup(func() { cleanupBundleDelegationFixture(t, db, ns) })

	dir := t.TempDir()
	writeBundleStubAgent(t, dir, "specialist/agent.yaml", ns, "specialist",
		`{"turns":[[{"type":"text_delta","text":"resolved by specialist"},{"type":"completed","stop_reason":"end_turn"}]]}`, nil)
	writeBundleStubAgent(t, dir, "orchestrator/agent.yaml", ns, "orchestrator",
		`{"turns":[[{"type":"tool_call","name":"ask_specialist","args":{"task":"help"}},{"type":"completed","stop_reason":"tool_use"}],[{"type":"text_delta","text":"orchestrator done"},{"type":"completed","stop_reason":"end_turn"}]]}`,
		[]manifest.SubagentBinding{{Ref: "./specialist/agent.yaml", As: "ask_specialist"}})
	writeBundleManifest(t, dir, ns, bundleName, "./orchestrator/agent.yaml", "1.0.0")

	srv := newAgentDelegationServer(db)
	published := publishStubBundle(t, srv, dir, ns, bundleName)
	deployStubBundle(t, srv, ns, bundleName, published.lockHash)

	rootID := driveStubSessionToCompletion(t, srv, published.rootVersionID, json.RawMessage(`{"message":"please delegate"}`))

	q := store.New(db.DB)
	root, err := q.GetSession(context.Background(), rootID)
	if err != nil {
		t.Fatalf("GetSession root: %v", err)
	}
	if root.Status != model.SessionStatusCompleted {
		t.Fatalf("root status = %q, want completed", root.Status)
	}

	childID, depth, childStatus := childSessionRow(t, db, rootID)
	if depth != 1 {
		t.Fatalf("child depth = %d, want 1", depth)
	}
	if childStatus != model.SessionStatusCompleted {
		t.Fatalf("child status = %q, want completed", childStatus)
	}

	callID := soleCallID(t, db, rootID)
	if childID != sessionids.ChildFromCallID(callID) {
		t.Fatalf("child id %q is not derived from the delegation call id %q", childID, callID)
	}

	inv, err := q.GetToolInvocation(context.Background(), callID)
	if err != nil {
		t.Fatalf("GetToolInvocation: %v", err)
	}
	if inv.Status != model.ToolInvocationSucceeded {
		t.Fatalf("delegation invocation status = %q, want succeeded", inv.Status)
	}
	var payload map[string]string
	if err := json.Unmarshal(inv.Result, &payload); err != nil {
		t.Fatalf("decode delegation result %s: %v", inv.Result, err)
	}
	if payload["output"] != "resolved by specialist" {
		t.Fatalf("delegation result output = %q, want specialist answer", payload["output"])
	}
}

func TestAgentDelegationE2E_recursionDepthTwo(t *testing.T) {
	t.Setenv("RUNTIME_ENABLE_STUB_PROVIDER", "true")
	db := openToolTestPostgres(t)
	ns := "bundledeleg-" + uuid.NewString()[:8]
	bundleName := "support"
	t.Cleanup(func() { cleanupBundleDelegationFixture(t, db, ns) })

	dir := t.TempDir()
	writeBundleStubAgent(t, dir, "leaf/agent.yaml", ns, "leaf",
		`{"turns":[[{"type":"text_delta","text":"leaf answer"},{"type":"completed","stop_reason":"end_turn"}]]}`, nil)
	writeBundleStubAgent(t, dir, "middle/agent.yaml", ns, "middle",
		`{"turns":[[{"type":"tool_call","name":"ask_leaf","args":{"task":"x"}},{"type":"completed","stop_reason":"tool_use"}],[{"type":"text_delta","text":"middle answer"},{"type":"completed","stop_reason":"end_turn"}]]}`,
		[]manifest.SubagentBinding{{Ref: "./leaf/agent.yaml", As: "ask_leaf"}})
	writeBundleStubAgent(t, dir, "root/agent.yaml", ns, "root",
		`{"turns":[[{"type":"tool_call","name":"ask_middle","args":{"task":"x"}},{"type":"completed","stop_reason":"tool_use"}],[{"type":"text_delta","text":"root answer"},{"type":"completed","stop_reason":"end_turn"}]]}`,
		[]manifest.SubagentBinding{{Ref: "./middle/agent.yaml", As: "ask_middle"}})
	writeBundleManifest(t, dir, ns, bundleName, "./root/agent.yaml", "1.0.0")

	srv := newAgentDelegationServer(db)
	published := publishStubBundle(t, srv, dir, ns, bundleName)
	deployStubBundle(t, srv, ns, bundleName, published.lockHash)

	driveStubSessionToCompletion(t, srv, published.rootVersionID, json.RawMessage(`{"message":"go"}`))

	var maxDepth int
	if err := db.QueryRow(`
		SELECT COALESCE(MAX(depth), 0) FROM sessions
		WHERE agent_version_id IN (
			SELECT member_version_id FROM bundle_members WHERE bundle_version_id = $1
		)
	`, published.bundleVersionID).Scan(&maxDepth); err != nil {
		t.Fatalf("max depth query: %v", err)
	}
	if maxDepth != 2 {
		t.Fatalf("max delegation depth = %d, want 2", maxDepth)
	}

	var completed, total int
	if err := db.QueryRow(`
		SELECT COUNT(*) FILTER (WHERE status = $2), COUNT(*) FROM sessions
		WHERE agent_version_id IN (
			SELECT member_version_id FROM bundle_members WHERE bundle_version_id = $1
		)
	`, published.bundleVersionID, model.SessionStatusCompleted).Scan(&completed, &total); err != nil {
		t.Fatalf("status count query: %v", err)
	}
	if total != 3 || completed != 3 {
		t.Fatalf("completed/total sessions = %d/%d, want 3/3", completed, total)
	}
}

// TestAgentDelegationE2E_recoveryResumesDelegation simulates a crash mid
// delegation: the parent ledger holds a dispatched agent invocation but the
// child run never finished (here it had not even been created). On recovery the
// runtime resumes the delegation by driving the durable child to completion and
// records its output into the parent ledger, then finishes the parent turn.
func TestAgentDelegationE2E_recoveryResumesDelegation(t *testing.T) {
	t.Setenv("RUNTIME_ENABLE_STUB_PROVIDER", "true")
	db := openToolTestPostgres(t)
	ns := "bundledeleg-" + uuid.NewString()[:8]
	bundleName := "support"
	t.Cleanup(func() { cleanupBundleDelegationFixture(t, db, ns) })

	dir := t.TempDir()
	writeBundleStubAgent(t, dir, "specialist/agent.yaml", ns, "specialist",
		`{"turns":[[{"type":"text_delta","text":"recovered answer"},{"type":"completed","stop_reason":"end_turn"}]]}`, nil)
	writeBundleStubAgent(t, dir, "orchestrator/agent.yaml", ns, "orchestrator",
		`{"turns":[[{"type":"text_delta","text":"orchestrator recovered"},{"type":"completed","stop_reason":"end_turn"}]]}`,
		[]manifest.SubagentBinding{{Ref: "./specialist/agent.yaml", As: "ask_specialist"}})
	writeBundleManifest(t, dir, ns, bundleName, "./orchestrator/agent.yaml", "1.0.0")

	srv := newAgentDelegationServer(db)
	published := publishStubBundle(t, srv, dir, ns, bundleName)
	deployStubBundle(t, srv, ns, bundleName, published.lockHash)

	parentID := uuid.NewString()
	callID := uuid.NewString()
	historyJSON, err := encodeHistory([]provider.Message{{Role: provider.RoleUser, Content: "please delegate"}})
	if err != nil {
		t.Fatalf("encodeHistory: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO sessions (id, agent_version_id, input, status, history)
		VALUES ($1, $2, '{"message":"please delegate"}'::jsonb, $3, $4::jsonb)
	`, parentID, published.rootVersionID, model.SessionStatusAwaitingTool, historyJSON); err != nil {
		t.Fatalf("insert parent session: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO tool_invocations (call_id, session_id, agent_version_id, turn, tool, version, args, status)
		VALUES ($1, $2, $3, 1, $4, '', '{"task":"help"}'::jsonb, $5)
	`, callID, parentID, published.rootVersionID, manifest.LogicalID(ns, "specialist"), model.ToolInvocationDispatched); err != nil {
		t.Fatalf("insert dispatched delegation invocation: %v", err)
	}

	srv.recoverDetachedSession(parentID)

	q := store.New(db.DB)
	inv, err := q.GetToolInvocation(context.Background(), callID)
	if err != nil {
		t.Fatalf("GetToolInvocation: %v", err)
	}
	if inv.Status != model.ToolInvocationSucceeded {
		t.Fatalf("delegation invocation status = %q, want succeeded", inv.Status)
	}

	child, err := q.GetSession(context.Background(), sessionids.ChildFromCallID(callID))
	if err != nil {
		t.Fatalf("recovered child session %s not found: %v", sessionids.ChildFromCallID(callID), err)
	}
	if child.Status != model.SessionStatusCompleted {
		t.Fatalf("recovered child status = %q, want completed", child.Status)
	}

	parent, err := q.GetSession(context.Background(), parentID)
	if err != nil {
		t.Fatalf("GetSession parent: %v", err)
	}
	if parent.Status != model.SessionStatusAwaitingInput {
		t.Fatalf("parent status = %q, want awaiting_input after recovery resumed the turn", parent.Status)
	}
}

// TestAgentDelegationE2E_recoveryResumesChildStoredVersionAfterRepublish proves
// that recovery drives an existing non-terminal child with the agent version
// stored on its session row, not a newer bundle publish of the same member.
func TestAgentDelegationE2E_recoveryResumesChildStoredVersionAfterRepublish(t *testing.T) {
	t.Setenv("RUNTIME_ENABLE_STUB_PROVIDER", "true")
	db := openToolTestPostgres(t)
	ns := "bundledeleg-" + uuid.NewString()[:8]
	bundleName := "support"
	t.Cleanup(func() { cleanupBundleDelegationFixture(t, db, ns) })

	dir := t.TempDir()
	writeBundleStubAgent(t, dir, "specialist/agent.yaml", ns, "specialist",
		`{"turns":[[{"type":"text_delta","text":"answer from v1"},{"type":"completed","stop_reason":"end_turn"}]]}`, nil)
	writeBundleStubAgent(t, dir, "orchestrator/agent.yaml", ns, "orchestrator",
		`{"turns":[[{"type":"text_delta","text":"orchestrator recovered"},{"type":"completed","stop_reason":"end_turn"}]]}`,
		[]manifest.SubagentBinding{{Ref: "./specialist/agent.yaml", As: "ask_specialist"}})
	writeBundleManifest(t, dir, ns, bundleName, "./orchestrator/agent.yaml", "1.0.0")

	srv := newAgentDelegationServer(db)
	v1 := publishStubBundle(t, srv, dir, ns, bundleName)
	deployStubBundle(t, srv, ns, bundleName, v1.lockHash)
	specialistV1ID := v1.memberVersions["specialist"]

	parentID := uuid.NewString()
	callID := uuid.NewString()
	childID := sessionids.ChildFromCallID(callID)
	historyJSON, err := encodeHistory([]provider.Message{{Role: provider.RoleUser, Content: "please delegate"}})
	if err != nil {
		t.Fatalf("encodeHistory: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO sessions (id, agent_version_id, input, status, history)
		VALUES ($1, $2, '{"message":"please delegate"}'::jsonb, $3, $4::jsonb)
	`, parentID, v1.rootVersionID, model.SessionStatusAwaitingTool, historyJSON); err != nil {
		t.Fatalf("insert parent session: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO tool_invocations (call_id, session_id, agent_version_id, turn, tool, version, args, status)
		VALUES ($1, $2, $3, 1, $4, '', '{"task":"help"}'::jsonb, $5)
	`, callID, parentID, v1.rootVersionID, manifest.LogicalID(ns, "specialist"), model.ToolInvocationDispatched); err != nil {
		t.Fatalf("insert dispatched delegation invocation: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO sessions (id, agent_version_id, parent_session_id, input, status, history, depth)
		VALUES ($1, $2, $3, '{"task":"help"}'::jsonb, $4, '[]'::jsonb, 1)
	`, childID, specialistV1ID, parentID, model.SessionStatusRunning); err != nil {
		t.Fatalf("insert interrupted child session: %v", err)
	}

	writeBundleStubAgent(t, dir, "specialist/agent.yaml", ns, "specialist",
		`{"turns":[[{"type":"text_delta","text":"answer from v2"},{"type":"completed","stop_reason":"end_turn"}]]}`, nil)
	writeBundleManifest(t, dir, ns, bundleName, "./orchestrator/agent.yaml", "1.0.1")
	v2 := publishStubBundle(t, srv, dir, ns, bundleName)
	deployStubBundle(t, srv, ns, bundleName, v2.lockHash)

	srv.recoverDetachedSession(parentID)

	q := store.New(db.DB)
	inv, err := q.GetToolInvocation(context.Background(), callID)
	if err != nil {
		t.Fatalf("GetToolInvocation: %v", err)
	}
	if inv.Status != model.ToolInvocationSucceeded {
		t.Fatalf("delegation invocation status = %q, want succeeded", inv.Status)
	}
	var payload map[string]string
	if err := json.Unmarshal(inv.Result, &payload); err != nil {
		t.Fatalf("decode delegation result %s: %v", inv.Result, err)
	}
	if payload["output"] != "answer from v1" {
		t.Fatalf("delegation result output = %q, want answer from v1 (stored child version)", payload["output"])
	}

	child, err := q.GetSession(context.Background(), childID)
	if err != nil {
		t.Fatalf("GetSession child: %v", err)
	}
	if child.AgentVersionID != specialistV1ID {
		t.Fatalf("child agent_version_id = %q, want v1 %q", child.AgentVersionID, specialistV1ID)
	}
	if child.Status != model.SessionStatusCompleted {
		t.Fatalf("recovered child status = %q, want completed", child.Status)
	}
}

func soleCallID(t *testing.T, db *sqlx.DB, sessionID string) string {
	t.Helper()
	rows, err := db.Query(`SELECT call_id FROM tool_invocations WHERE session_id = $1`, sessionID)
	if err != nil {
		t.Fatalf("query call ids: %v", err)
	}
	defer rows.Close()
	var callIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan call id: %v", err)
		}
		callIDs = append(callIDs, id)
	}
	if len(callIDs) != 1 {
		t.Fatalf("call ids for %s = %d, want 1", sessionID, len(callIDs))
	}
	return callIDs[0]
}
