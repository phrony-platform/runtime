package core

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/phrony-platform/runtime/internal/executor"
	"github.com/phrony-platform/runtime/internal/manifest"
	"github.com/phrony-platform/runtime/internal/model"
	"github.com/phrony-platform/runtime/internal/provider"
	"github.com/phrony-platform/runtime/internal/providertest"
	"github.com/phrony-platform/runtime/internal/store"
	"github.com/phrony-platform/runtime/internal/tooldispatch"
)

func agentBindingVersion(agent *manifest.Agent) *executor.Version {
	return executor.NewVersionWithProvider("av-1", agent, nil)
}

func agentDelegatingAgent() *manifest.Agent {
	return &manifest.Agent{
		Metadata: manifest.AgentMetadata{Namespace: "demo", Name: "orchestrator"},
		Spec: manifest.AgentSpec{
			Tools: []manifest.ToolBinding{{
				Ref:         "ask_billing",
				As:          "ask_billing",
				Description: "Delegate billing questions.",
				Agent:       &manifest.ToolAgentBinding{Namespace: "support", Name: "billing"},
			}},
		},
	}
}

func TestBuildAgentDispatcher_nilWithoutAgentBindings(t *testing.T) {
	srv := &runtimeServer{}
	agent := &manifest.Agent{Spec: manifest.AgentSpec{Tools: []manifest.ToolBinding{{Ref: "weather.get-forecast"}}}}
	if d := srv.buildAgentDispatcher(context.Background(), nil, "sess-1", agent, rootSessionDepth); d != nil {
		t.Fatalf("expected nil dispatcher when no agent bindings, got %#v", d)
	}
}

func TestBuildAgentDispatcher_handlesCompiledAgentRef(t *testing.T) {
	srv := &runtimeServer{}
	d := srv.buildAgentDispatcher(context.Background(), nil, "sess-1", agentDelegatingAgent(), rootSessionDepth)
	if d == nil {
		t.Fatal("expected dispatcher for agent binding")
	}
	if !d.Handles("support.billing") {
		t.Fatal("dispatcher should handle the compiled agent ref")
	}
	if d.Handles("weather.get-forecast") {
		t.Fatal("dispatcher should not handle worker-backed tools")
	}
	if err := d.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestSessionToolDispatch_chainsAgentDispatcher(t *testing.T) {
	worker := &tooldispatch.FakeDispatcher{}
	srv := &runtimeServer{toolDispatch: worker}

	got, err := srv.sessionToolDispatch(context.Background(), nil, "sess-1", agentBindingVersion(agentDelegatingAgent()), rootSessionDepth)
	if err != nil {
		t.Fatalf("sessionToolDispatch: %v", err)
	}
	routing, ok := got.(*tooldispatch.RoutingDispatcher)
	if !ok {
		t.Fatalf("expected *RoutingDispatcher, got %T", got)
	}
	if routing.Fallback != tooldispatch.Dispatcher(worker) {
		t.Fatal("routing fallback must be the worker dispatcher")
	}
	if !routing.Primary.Handles("support.billing") {
		t.Fatal("routing primary should handle the agent-backed ref")
	}
}

func TestAgentDispatcher_depthCapReturnsToolError(t *testing.T) {
	d := &agentDispatcher{
		server:   &runtimeServer{},
		depth:    2,
		maxDepth: 2,
		bindings: map[string]agentBinding{"support.billing": {namespace: "support", name: "billing", result: manifest.SubagentResultSummary}},
	}
	res, err := d.Dispatch(context.Background(), tooldispatch.ToolCall{CallID: "c1", Tool: "support.billing"})
	if err != nil {
		t.Fatalf("Dispatch returned infra error: %v", err)
	}
	if res.Err == nil || res.Err.Code != "subagent_depth_exceeded" {
		t.Fatalf("res.Err = %#v, want subagent_depth_exceeded tool error", res.Err)
	}
}

func TestAgentDispatcher_dispatchUnknownToolIsNoHandler(t *testing.T) {
	d := &agentDispatcher{bindings: map[string]agentBinding{}}
	_, err := d.Dispatch(context.Background(), tooldispatch.ToolCall{CallID: "c1", Tool: "support.billing"})
	if !tooldispatch.IsNoHandler(err) {
		t.Fatalf("err = %v, want ErrNoHandler", err)
	}
}

func TestSubagentResultPayload_summary(t *testing.T) {
	output := json.RawMessage(`{"message":"resolved answer","session_usage":{"input_tokens":3,"output_tokens":5}}`)
	payload, err := subagentResultPayload(output, manifest.SubagentResultSummary)
	if err != nil {
		t.Fatalf("subagentResultPayload: %v", err)
	}
	var obj map[string]string
	if err := json.Unmarshal(payload, &obj); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if obj["output"] != "resolved answer" {
		t.Fatalf("summary output = %q, want resolved answer", obj["output"])
	}
}

func TestSubagentResultPayload_fullReturnsWholeOutput(t *testing.T) {
	output := json.RawMessage(`{"message":"resolved answer","turns":[{"stop_reason":"end_turn"}]}`)
	payload, err := subagentResultPayload(output, manifest.SubagentResultFull)
	if err != nil {
		t.Fatalf("subagentResultPayload: %v", err)
	}
	if string(payload) != string(output) {
		t.Fatalf("full payload = %s, want whole output", payload)
	}
}

func TestSubagentResultPayload_emptyOutput(t *testing.T) {
	payload, err := subagentResultPayload(nil, manifest.SubagentResultSummary)
	if err != nil {
		t.Fatalf("subagentResultPayload: %v", err)
	}
	if string(payload) != `{"output":""}` {
		t.Fatalf("payload = %s", payload)
	}
}

func TestChildInputFromArgs(t *testing.T) {
	if got := childInputFromArgs(nil); string(got) != "{}" {
		t.Fatalf("empty args = %s, want {}", got)
	}
	args := json.RawMessage(`{"task":"do it"}`)
	if got := childInputFromArgs(args); string(got) != string(args) {
		t.Fatalf("args passthrough = %s", got)
	}
}

func TestResolveMaxSubagentDepth(t *testing.T) {
	if got := resolveMaxSubagentDepth(&manifest.Agent{}); got != defaultMaxSubagentDepth {
		t.Fatalf("default depth = %d, want %d", got, defaultMaxSubagentDepth)
	}
	depth := 3
	agent := &manifest.Agent{Spec: manifest.AgentSpec{Limits: &manifest.Limits{MaxSubagentDepth: &depth}}}
	if got := resolveMaxSubagentDepth(agent); got != 3 {
		t.Fatalf("configured depth = %d, want 3", got)
	}
}

func TestAgentDispatcher_childResultSummaryCarriesUsage(t *testing.T) {
	db, mock := testSQLxDB(t)
	now := time.Now()
	output := []byte(`{"message":"child answer","session_usage":{"input_tokens":4,"output_tokens":6}}`)
	mock.ExpectQuery(`FROM sessions`).
		WithArgs("child-sess").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "agent_version_id", "input", "status", "output", "error", "history", "created_at", "updated_at",
		}).AddRow("child-sess", "child-ver", []byte(`{}`), model.SessionStatusCompleted, output, nil, []byte(`[]`), now, now))

	d := &agentDispatcher{server: &runtimeServer{db: db}}
	res, err := d.childResult(context.Background(), store.New(db), "call-1", "child-sess", manifest.SubagentResultSummary)
	if err != nil {
		t.Fatalf("childResult: %v", err)
	}
	if res.Err != nil {
		t.Fatalf("unexpected tool error: %#v", res.Err)
	}
	var obj map[string]string
	if err := json.Unmarshal(res.Payload, &obj); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if obj["output"] != "child answer" {
		t.Fatalf("output = %q", obj["output"])
	}
	if res.Usage == nil || res.Usage.Total() != 10 {
		t.Fatalf("usage = %#v, want total 10", res.Usage)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestAgentDispatcher_childResultFailedBecomesToolError(t *testing.T) {
	db, mock := testSQLxDB(t)
	now := time.Now()
	errMsg := "model unavailable"
	mock.ExpectQuery(`FROM sessions`).
		WithArgs("child-sess").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "agent_version_id", "input", "status", "output", "error", "history", "created_at", "updated_at",
		}).AddRow("child-sess", "child-ver", []byte(`{}`), model.SessionStatusFailed, nil, errMsg, []byte(`[]`), now, now))

	d := &agentDispatcher{server: &runtimeServer{db: db}}
	res, err := d.childResult(context.Background(), store.New(db), "call-1", "child-sess", manifest.SubagentResultSummary)
	if err != nil {
		t.Fatalf("childResult: %v", err)
	}
	if res.Err == nil || res.Err.Code != "subagent_failed" {
		t.Fatalf("res.Err = %#v, want subagent_failed", res.Err)
	}
	if res.Err.Message != errMsg {
		t.Fatalf("message = %q, want %q", res.Err.Message, errMsg)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestAgentDispatcher_childResultNonTerminalBecomesToolError(t *testing.T) {
	db, mock := testSQLxDB(t)
	now := time.Now()
	mock.ExpectQuery(`FROM sessions`).
		WithArgs("child-sess").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "agent_version_id", "input", "status", "output", "error", "history", "created_at", "updated_at",
		}).AddRow("child-sess", "child-ver", []byte(`{}`), model.SessionStatusAwaitingApproval, nil, nil, []byte(`[]`), now, now))

	d := &agentDispatcher{server: &runtimeServer{db: db}}
	res, err := d.childResult(context.Background(), store.New(db), "call-1", "child-sess", manifest.SubagentResultSummary)
	if err != nil {
		t.Fatalf("childResult: %v", err)
	}
	if res.Err == nil || res.Err.Code != "subagent_incomplete" {
		t.Fatalf("res.Err = %#v, want subagent_incomplete", res.Err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestInheritSessionSecrets_copiesByName(t *testing.T) {
	enc := mustTestEncryptor(t)
	db, mock := testSQLxDB(t)
	srv := &runtimeServer{db: db, secretsEnc: enc}

	sealed, err := enc.Encrypt("parent-sess", "openai", []byte("sk-secret"))
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	mock.ExpectQuery(`SELECT key_version, nonce, ciphertext`).
		WithArgs("parent-sess", "openai").
		WillReturnRows(sqlmock.NewRows([]string{"key_version", "nonce", "ciphertext"}).
			AddRow(sealed.KeyVersion, sealed.Nonce, sealed.Ciphertext))

	agent := &manifest.Agent{Secrets: map[string]manifest.SecretDefinition{"openai": {}}}
	resolved, err := srv.inheritSessionSecrets(context.Background(), store.New(db), "parent-sess", agent)
	if err != nil {
		t.Fatalf("inheritSessionSecrets: %v", err)
	}
	if string(resolved["openai"]) != "sk-secret" {
		t.Fatalf("inherited secret = %q, want sk-secret", resolved["openai"])
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestInheritSessionSecrets_missingParentSecret(t *testing.T) {
	enc := mustTestEncryptor(t)
	db, mock := testSQLxDB(t)
	srv := &runtimeServer{db: db, secretsEnc: enc}

	mock.ExpectQuery(`SELECT key_version, nonce, ciphertext`).
		WithArgs("parent-sess", "openai").
		WillReturnError(context.DeadlineExceeded)

	agent := &manifest.Agent{Secrets: map[string]manifest.SecretDefinition{"openai": {}}}
	_, err := srv.inheritSessionSecrets(context.Background(), store.New(db), "parent-sess", agent)
	var missing *missingSecretError
	if !errors.As(err, &missing) {
		t.Fatalf("err = %v, want missingSecretError", err)
	}
}

func TestAgentDispatcher_childRunContextAppliesParentDeadline(t *testing.T) {
	d := &agentDispatcher{sessionCtx: context.Background()}
	deadline := time.Now().Add(45 * time.Second)
	ctx, cancel := d.childRunContext(tooldispatch.ToolCall{Deadline: deadline})
	defer cancel()
	got, ok := ctx.Deadline()
	if !ok || !got.Equal(deadline) {
		t.Fatalf("child ctx deadline = %v (ok=%v), want %v", got, ok, deadline)
	}
}

func TestAgentDispatcher_childRunContextNoDeadline(t *testing.T) {
	d := &agentDispatcher{sessionCtx: context.Background()}
	ctx, cancel := d.childRunContext(tooldispatch.ToolCall{})
	defer cancel()
	if _, ok := ctx.Deadline(); ok {
		t.Fatal("expected no deadline when the parent call has none")
	}
}

func TestRunChildSessionToCompletion_drivesChildToCompleted(t *testing.T) {
	db, mock := testSQLxDB(t)
	mock.MatchExpectationsInOrder(false)
	now := time.Now()
	agent := &manifest.Agent{
		Spec: manifest.AgentSpec{
			Instructions: manifest.InstructionsSpec{Text: "System."},
			Model:        manifest.ModelConfig{Provider: provider.IDAnthropic, Name: "m"},
		},
	}
	srv := &runtimeServer{
		db:             db,
		activeSessions: &sync.Map{},
		loadSessionVersionFn: func(context.Context, *store.Queries, string, string) (*executor.Version, error) {
			return executor.NewVersionWithProvider("child-ver", agent, providertest.DeltaCompleted()), nil
		},
	}
	ver := executor.NewVersionWithProvider("child-ver", agent, providertest.DeltaCompleted())

	// Turn records user_message + assistant_message; completion records session_completed.
	for i := 0; i < 3; i++ {
		mock.ExpectQuery(`INSERT INTO session_events`).
			WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(i + 1)))
	}
	// After the turn the driver persists output, then completes the child
	// autonomously (the closed input stream yields no operator messages) and
	// purges its inherited secrets.
	mock.ExpectQuery(`UPDATE sessions`).
		WithArgs("child-sess", model.SessionStatusAwaitingInput, sqlmock.AnyArg(), nil, sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"updated_at"}).AddRow(now))
	mock.ExpectQuery(`UPDATE sessions`).
		WithArgs("child-sess", model.SessionStatusCompleted, sqlmock.AnyArg(), nil, nil).
		WillReturnRows(sqlmock.NewRows([]string{"updated_at"}).AddRow(now))
	mock.ExpectExec(`DELETE FROM session_secrets`).
		WithArgs("child-sess").
		WillReturnResult(sqlmock.NewResult(0, 0))

	if err := srv.runChildSessionToCompletion(
		context.Background(), store.New(db), "child-sess", "child-ver", ver, []byte(`{"message":"hi"}`), 1,
	); err != nil {
		t.Fatalf("runChildSessionToCompletion: %v", err)
	}
	if srv.sessionIsActive("child-sess") {
		t.Fatal("child session should be unregistered after completion")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestInheritSessionSecrets_noSecretsNoLookup(t *testing.T) {
	srv := &runtimeServer{}
	resolved, err := srv.inheritSessionSecrets(context.Background(), nil, "parent-sess", &manifest.Agent{})
	if err != nil {
		t.Fatalf("inheritSessionSecrets: %v", err)
	}
	if resolved != nil {
		t.Fatalf("resolved = %#v, want nil", resolved)
	}
}
