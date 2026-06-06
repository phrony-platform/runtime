package core

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/phrony-platform/runtime/internal/manifest"
	"github.com/phrony-platform/runtime/internal/model"
	"github.com/phrony-platform/runtime/internal/provider"
	"github.com/phrony-platform/runtime/internal/store"
	"github.com/phrony-platform/runtime/internal/tooldispatch"
)

// These tests drive agent-to-agent delegation end to end against a real Postgres
// ledger using the scripted stub provider (RUNTIME_ENABLE_STUB_PROVIDER): a
// parent agent emits a tool_use for a compiled spec.agents binding, the agent
// dispatcher runs the target agent in a nested child session through the same
// session machinery, and the child's final output flows back as the tool result.

// stubDelegationAgent builds a compiled-snapshot agent backed by the stub
// provider whose model behaviour is the supplied scripted turns.
func stubDelegationAgent(namespace, name, script string, mutate func(*manifest.Agent)) *manifest.Agent {
	a := &manifest.Agent{
		APIVersion: manifest.APIVersionV1,
		Kind:       manifest.KindAgent,
		Metadata: manifest.AgentMetadata{
			Namespace:   namespace,
			Name:        name,
			Version:     "1.0.0",
			Annotations: map[string]string{manifest.AnnotationStubScript: script},
		},
		Spec: manifest.AgentSpec{
			Purpose:      "e2e",
			Instructions: manifest.InstructionsSpec{Text: "System."},
			Model:        manifest.ModelConfig{Provider: provider.IDStub, Name: "stub"},
		},
	}
	if mutate != nil {
		mutate(a)
	}
	return a
}

// delegationBinding returns a compiled agent-backed tool binding (the shape
// spec.agents compiles to) with a frozen AgentVersionID from bundle publish.
func delegationBinding(wire, namespace, name, agentVersionID string, mutate func(*manifest.ToolBinding)) manifest.ToolBinding {
	tb := manifest.ToolBinding{
		Ref:             manifest.LogicalID(namespace, name),
		As:              wire,
		Description:     "Delegate to " + name,
		InputSchema:     &manifest.SchemaSpec{Inline: map[string]any{"type": "object"}},
		SideEffectClass: manifest.SideEffectNonIdempotentWrite,
		Agent:           &manifest.ToolAgentBinding{Namespace: namespace, Name: name, AgentVersionID: agentVersionID},
	}
	if mutate != nil {
		mutate(&tb)
	}
	return tb
}

// redeployStubAgent inserts a new agent version and deployment for an existing
// agent, making that version the active target for late_bound delegation bindings.
func redeployStubAgent(t *testing.T, db *sqlx.DB, namespace, name string, agent *manifest.Agent) string {
	t.Helper()
	var agentID string
	if err := db.QueryRow(`SELECT id FROM agents WHERE namespace = $1 AND name = $2`, namespace, name).Scan(&agentID); err != nil {
		t.Fatalf("lookup agent %s/%s: %v", namespace, name, err)
	}
	manifestJSON, err := json.Marshal(agent)
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	agentVersionID := uuid.NewString()
	deploymentID := uuid.NewString()
	if _, err := db.Exec(`
		INSERT INTO agent_versions (id, agent_id, version, content_hash, manifest)
		VALUES ($1, $2, '2.0.0', $3, $4::jsonb)
	`, agentVersionID, agentID, uuid.NewString(), manifestJSON); err != nil {
		t.Fatalf("insert redeployed agent_version %s: %v", name, err)
	}
	if _, err := db.Exec(`
		INSERT INTO deployments (id, agent_id, agent_version_id, action, actor)
		VALUES ($1, $2, $3, 'deploy', 'e2e')
	`, deploymentID, agentID, agentVersionID); err != nil {
		t.Fatalf("insert redeployed deployment %s: %v", name, err)
	}
	return agentVersionID
}

func insertDeployedStubAgent(t *testing.T, db *sqlx.DB, namespace, name string, agent *manifest.Agent) string {
	t.Helper()
	manifestJSON, err := json.Marshal(agent)
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	agentID := uuid.NewString()
	agentVersionID := uuid.NewString()
	deploymentID := uuid.NewString()
	if _, err := db.Exec(`
		INSERT INTO agents (id, namespace, name, owner, labels)
		VALUES ($1, $2, $3, 'e2e', '{}'::jsonb)
	`, agentID, namespace, name); err != nil {
		t.Fatalf("insert agent %s: %v", name, err)
	}
	if _, err := db.Exec(`
		INSERT INTO agent_versions (id, agent_id, version, content_hash, manifest)
		VALUES ($1, $2, '1.0.0', $3, $4::jsonb)
	`, agentVersionID, agentID, uuid.NewString(), manifestJSON); err != nil {
		t.Fatalf("insert agent_version %s: %v", name, err)
	}
	if _, err := db.Exec(`
		INSERT INTO deployments (id, agent_id, agent_version_id, action, actor)
		VALUES ($1, $2, $3, 'deploy', 'e2e')
	`, deploymentID, agentID, agentVersionID); err != nil {
		t.Fatalf("insert deployment %s: %v", name, err)
	}
	return agentVersionID
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

// driveStubSessionToCompletion creates a run session for the agent version and
// drives it autonomously to a terminal state, reusing the same completion driver
// the runtime uses for delegated children (a closed input stream means the run
// finishes after its turns rather than parking for an operator).
func driveStubSessionToCompletion(t *testing.T, srv *runtimeServer, agentVersionID string, input json.RawMessage) string {
	t.Helper()
	ctx := context.Background()
	q, err := srv.queries()
	if err != nil {
		t.Fatalf("queries: %v", err)
	}
	sessionID, err := srv.createRunSession(ctx, agentVersionID, input, nil)
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

func cleanupAgentDelegationFixture(t *testing.T, db *sqlx.DB, namespace string) {
	t.Helper()
	versions := `SELECT av.id FROM agent_versions av JOIN agents a ON a.id = av.agent_id WHERE a.namespace = $1`
	_, _ = db.Exec(`DELETE FROM tool_invocations WHERE session_id IN (SELECT id FROM sessions WHERE agent_version_id IN (`+versions+`))`, namespace)
	_, _ = db.Exec(`DELETE FROM sessions WHERE agent_version_id IN (`+versions+`)`, namespace)
	_, _ = db.Exec(`DELETE FROM deployments WHERE agent_id IN (SELECT id FROM agents WHERE namespace = $1)`, namespace)
	_, _ = db.Exec(`DELETE FROM agent_versions WHERE agent_id IN (SELECT id FROM agents WHERE namespace = $1)`, namespace)
	_, _ = db.Exec(`DELETE FROM agents WHERE namespace = $1`, namespace)
}

func TestAgentDelegationE2E_pinnedVersionRunsNonActive(t *testing.T) {
	t.Setenv("RUNTIME_ENABLE_STUB_PROVIDER", "true")
	db := openToolTestPostgres(t)
	ns := "agentdeleg-" + uuid.NewString()[:8]
	t.Cleanup(func() { cleanupAgentDelegationFixture(t, db, ns) })

	specialistV1ID := insertDeployedStubAgent(t, db, ns, "specialist",
		stubDelegationAgent(ns, "specialist",
			`{"turns":[[{"type":"text_delta","text":"answer from v1"},{"type":"completed","stop_reason":"end_turn"}]]}`, nil))

	orchestrator := stubDelegationAgent(ns, "orchestrator",
		`{"turns":[[{"type":"tool_call","name":"ask_specialist","args":{"task":"help"}},{"type":"completed","stop_reason":"tool_use"}],[{"type":"text_delta","text":"orchestrator done"},{"type":"completed","stop_reason":"end_turn"}]]}`,
		func(a *manifest.Agent) {
			a.Spec.Tools = []manifest.ToolBinding{delegationBinding("ask_specialist", ns, "specialist", specialistV1ID, nil)}
		})
	orchestratorVersionID := insertDeployedStubAgent(t, db, ns, "orchestrator", orchestrator)

	redeployStubAgent(t, db, ns, "specialist",
		stubDelegationAgent(ns, "specialist",
			`{"turns":[[{"type":"text_delta","text":"answer from v2"},{"type":"completed","stop_reason":"end_turn"}]]}`, nil))

	srv := newAgentDelegationServer(db)
	rootID := driveStubSessionToCompletion(t, srv, orchestratorVersionID, json.RawMessage(`{"message":"please delegate"}`))

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
		t.Fatalf("delegation result output = %q, want pinned v1 answer", payload["output"])
	}
}

func TestAgentDelegationE2E_happyPath(t *testing.T) {
	t.Setenv("RUNTIME_ENABLE_STUB_PROVIDER", "true")
	db := openToolTestPostgres(t)
	ns := "agentdeleg-" + uuid.NewString()[:8]
	t.Cleanup(func() { cleanupAgentDelegationFixture(t, db, ns) })

	specialistVersionID := insertDeployedStubAgent(t, db, ns, "specialist",
		stubDelegationAgent(ns, "specialist",
			`{"turns":[[{"type":"text_delta","text":"resolved by specialist"},{"type":"completed","stop_reason":"end_turn"}]]}`, nil))

	orchestrator := stubDelegationAgent(ns, "orchestrator",
		`{"turns":[[{"type":"tool_call","name":"ask_specialist","args":{"task":"help"}},{"type":"completed","stop_reason":"tool_use"}],[{"type":"text_delta","text":"orchestrator done"},{"type":"completed","stop_reason":"end_turn"}]]}`,
		func(a *manifest.Agent) {
			a.Spec.Tools = []manifest.ToolBinding{delegationBinding("ask_specialist", ns, "specialist", specialistVersionID, nil)}
		})
	orchestratorVersionID := insertDeployedStubAgent(t, db, ns, "orchestrator", orchestrator)

	srv := newAgentDelegationServer(db)
	rootID := driveStubSessionToCompletion(t, srv, orchestratorVersionID, json.RawMessage(`{"message":"please delegate"}`))

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
	if childID != childSessionID(callID) {
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
	ns := "agentdeleg-" + uuid.NewString()[:8]
	t.Cleanup(func() { cleanupAgentDelegationFixture(t, db, ns) })

	leafVersionID := insertDeployedStubAgent(t, db, ns, "leaf",
		stubDelegationAgent(ns, "leaf",
			`{"turns":[[{"type":"text_delta","text":"leaf answer"},{"type":"completed","stop_reason":"end_turn"}]]}`, nil))

	middleVersionID := insertDeployedStubAgent(t, db, ns, "middle",
		stubDelegationAgent(ns, "middle",
			`{"turns":[[{"type":"tool_call","name":"ask_leaf","args":{"task":"x"}},{"type":"completed","stop_reason":"tool_use"}],[{"type":"text_delta","text":"middle answer"},{"type":"completed","stop_reason":"end_turn"}]]}`,
			func(a *manifest.Agent) {
				a.Spec.Tools = []manifest.ToolBinding{delegationBinding("ask_leaf", ns, "leaf", leafVersionID, nil)}
			}))

	rootVersionID := insertDeployedStubAgent(t, db, ns, "root",
		stubDelegationAgent(ns, "root",
			`{"turns":[[{"type":"tool_call","name":"ask_middle","args":{"task":"x"}},{"type":"completed","stop_reason":"tool_use"}],[{"type":"text_delta","text":"root answer"},{"type":"completed","stop_reason":"end_turn"}]]}`,
			func(a *manifest.Agent) {
				a.Spec.Tools = []manifest.ToolBinding{delegationBinding("ask_middle", ns, "middle", middleVersionID, nil)}
			}))

	srv := newAgentDelegationServer(db)
	driveStubSessionToCompletion(t, srv, rootVersionID, json.RawMessage(`{"message":"go"}`))

	// root (0) -> middle (1) -> leaf (2): assert the deepest session reached depth 2.
	var maxDepth int
	if err := db.QueryRow(`
		SELECT COALESCE(MAX(depth), 0) FROM sessions
		WHERE agent_version_id IN (
			SELECT av.id FROM agent_versions av JOIN agents a ON a.id = av.agent_id WHERE a.namespace = $1
		)
	`, ns).Scan(&maxDepth); err != nil {
		t.Fatalf("max depth query: %v", err)
	}
	if maxDepth != 2 {
		t.Fatalf("max delegation depth = %d, want 2", maxDepth)
	}

	var completed, total int
	if err := db.QueryRow(`
		SELECT COUNT(*) FILTER (WHERE status = $2), COUNT(*) FROM sessions
		WHERE agent_version_id IN (
			SELECT av.id FROM agent_versions av JOIN agents a ON a.id = av.agent_id WHERE a.namespace = $1
		)
	`, ns, model.SessionStatusCompleted).Scan(&completed, &total); err != nil {
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
	ns := "agentdeleg-" + uuid.NewString()[:8]
	t.Cleanup(func() { cleanupAgentDelegationFixture(t, db, ns) })

	specialistVersionID := insertDeployedStubAgent(t, db, ns, "specialist",
		stubDelegationAgent(ns, "specialist",
			`{"turns":[[{"type":"text_delta","text":"recovered answer"},{"type":"completed","stop_reason":"end_turn"}]]}`, nil))

	// The orchestrator resumes after the tool result, so its script holds only
	// the post-delegation completion turn (the pre-crash tool_use turn is not
	// replayed: recovery resumes from the durable ledger).
	orchestratorVersionID := insertDeployedStubAgent(t, db, ns, "orchestrator",
		stubDelegationAgent(ns, "orchestrator",
			`{"turns":[[{"type":"text_delta","text":"orchestrator recovered"},{"type":"completed","stop_reason":"end_turn"}]]}`,
			func(a *manifest.Agent) {
				a.Spec.Tools = []manifest.ToolBinding{delegationBinding("ask_specialist", ns, "specialist", specialistVersionID, nil)}
			}))

	parentID := uuid.NewString()
	callID := uuid.NewString()
	historyJSON, err := encodeHistory([]provider.Message{{Role: provider.RoleUser, Content: "please delegate"}})
	if err != nil {
		t.Fatalf("encodeHistory: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO sessions (id, agent_version_id, input, status, history)
		VALUES ($1, $2, '{"message":"please delegate"}'::jsonb, $3, $4::jsonb)
	`, parentID, orchestratorVersionID, model.SessionStatusAwaitingTool, historyJSON); err != nil {
		t.Fatalf("insert parent session: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO tool_invocations (call_id, session_id, agent_version_id, turn, tool, version, args, status)
		VALUES ($1, $2, $3, 1, $4, '', '{"task":"help"}'::jsonb, $5)
	`, callID, parentID, orchestratorVersionID, manifest.LogicalID(ns, "specialist"), model.ToolInvocationDispatched); err != nil {
		t.Fatalf("insert dispatched delegation invocation: %v", err)
	}

	srv := newAgentDelegationServer(db)
	srv.recoverDetachedSession(parentID)

	q := store.New(db.DB)
	inv, err := q.GetToolInvocation(context.Background(), callID)
	if err != nil {
		t.Fatalf("GetToolInvocation: %v", err)
	}
	if inv.Status != model.ToolInvocationSucceeded {
		t.Fatalf("delegation invocation status = %q, want succeeded", inv.Status)
	}

	child, err := q.GetSession(context.Background(), childSessionID(callID))
	if err != nil {
		t.Fatalf("recovered child session %s not found: %v", childSessionID(callID), err)
	}
	if child.Status != model.SessionStatusCompleted {
		t.Fatalf("recovered child status = %q, want completed", child.Status)
	}

	// The parent resumed past the delegation: it consumed the recovered tool
	// result, ran its next turn, and parked at awaiting_input (a recovered
	// interactive session waits for the next operator message after a turn). The
	// crucial guarantee is that it is no longer stuck at awaiting_tool.
	parent, err := q.GetSession(context.Background(), parentID)
	if err != nil {
		t.Fatalf("GetSession parent: %v", err)
	}
	if parent.Status != model.SessionStatusAwaitingInput {
		t.Fatalf("parent status = %q, want awaiting_input after recovery resumed the turn", parent.Status)
	}
}

// TestAgentDelegationE2E_recoveryResumesChildStoredVersionAfterRedeploy proves
// that recovery drives an existing non-terminal child with the agent version
// stored on its session row, not the target's current active deployment.
func TestAgentDelegationE2E_recoveryResumesChildStoredVersionAfterRedeploy(t *testing.T) {
	t.Setenv("RUNTIME_ENABLE_STUB_PROVIDER", "true")
	db := openToolTestPostgres(t)
	ns := "agentdeleg-" + uuid.NewString()[:8]
	t.Cleanup(func() { cleanupAgentDelegationFixture(t, db, ns) })

	specialistV1ID := insertDeployedStubAgent(t, db, ns, "specialist",
		stubDelegationAgent(ns, "specialist",
			`{"turns":[[{"type":"text_delta","text":"answer from v1"},{"type":"completed","stop_reason":"end_turn"}]]}`, nil))

	orchestratorVersionID := insertDeployedStubAgent(t, db, ns, "orchestrator",
		stubDelegationAgent(ns, "orchestrator",
			`{"turns":[[{"type":"text_delta","text":"orchestrator recovered"},{"type":"completed","stop_reason":"end_turn"}]]}`,
			func(a *manifest.Agent) {
				a.Spec.Tools = []manifest.ToolBinding{delegationBinding("ask_specialist", ns, "specialist", specialistV1ID, nil)}
			}))

	parentID := uuid.NewString()
	callID := uuid.NewString()
	childID := childSessionID(callID)
	historyJSON, err := encodeHistory([]provider.Message{{Role: provider.RoleUser, Content: "please delegate"}})
	if err != nil {
		t.Fatalf("encodeHistory: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO sessions (id, agent_version_id, input, status, history)
		VALUES ($1, $2, '{"message":"please delegate"}'::jsonb, $3, $4::jsonb)
	`, parentID, orchestratorVersionID, model.SessionStatusAwaitingTool, historyJSON); err != nil {
		t.Fatalf("insert parent session: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO tool_invocations (call_id, session_id, agent_version_id, turn, tool, version, args, status)
		VALUES ($1, $2, $3, 1, $4, '', '{"task":"help"}'::jsonb, $5)
	`, callID, parentID, orchestratorVersionID, manifest.LogicalID(ns, "specialist"), model.ToolInvocationDispatched); err != nil {
		t.Fatalf("insert dispatched delegation invocation: %v", err)
	}
	// Child was created under v1 but never finished before the crash.
	if _, err := db.Exec(`
		INSERT INTO sessions (id, agent_version_id, parent_session_id, input, status, history, depth)
		VALUES ($1, $2, $3, '{"task":"help"}'::jsonb, $4, '[]'::jsonb, 1)
	`, childID, specialistV1ID, parentID, model.SessionStatusRunning); err != nil {
		t.Fatalf("insert interrupted child session: %v", err)
	}

	// Target redeploy: active delegation now resolves to v2.
	redeployStubAgent(t, db, ns, "specialist",
		stubDelegationAgent(ns, "specialist",
			`{"turns":[[{"type":"text_delta","text":"answer from v2"},{"type":"completed","stop_reason":"end_turn"}]]}`, nil))

	srv := newAgentDelegationServer(db)
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

// soleCallID returns the single tool invocation call id recorded for a session,
// failing when the count is not exactly one.
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
