package core

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/jmoiron/sqlx"
	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
	"github.com/phrony-platform/runtime/internal/executor"
	"github.com/phrony-platform/runtime/internal/manifest"
	"github.com/phrony-platform/runtime/internal/model"
	"github.com/phrony-platform/runtime/internal/policy"
	"github.com/phrony-platform/runtime/internal/provider"
	"github.com/phrony-platform/runtime/internal/providertest"
	"github.com/phrony-platform/runtime/internal/store"
	"github.com/phrony-platform/runtime/internal/tooldispatch"
	"github.com/phrony-platform/runtime/internal/tooldispatch/testworker"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

const toolE2EDSN = "postgres://phrony_runtime:phrony_runtime@localhost:5432/phrony_runtime?sslmode=disable"

type toolE2EHarness struct {
	t            *testing.T
	srv          *runtimeServer
	reg          *tooldispatch.WorkerRegistry
	grpcCC       *grpc.ClientConn
	workerCancel context.CancelFunc
	workerWG     sync.WaitGroup
	// dispatchOverride, when set, replaces the worker dispatcher in the session
	// state so a turn can be routed through an alternate backend (e.g. MCP).
	dispatchOverride tooldispatch.Dispatcher
}

func (h *toolE2EHarness) sessionDispatch() tooldispatch.Dispatcher {
	if h.dispatchOverride != nil {
		return h.dispatchOverride
	}
	return h.srv.toolDispatch
}

type toolE2EConfig struct {
	DB             *sqlx.DB
	MaxQueuePerTool int
}

func newToolE2EHarness(t *testing.T, cfg toolE2EConfig) *toolE2EHarness {
	t.Helper()

	maxQueue := cfg.MaxQueuePerTool
	if maxQueue <= 0 {
		maxQueue = 8
	}
	reg := tooldispatch.NewWorkerRegistry(tooldispatch.RegistryConfig{
		LeaseTTL:        time.Minute,
		MaxQueuePerTool: maxQueue,
	})
	if cfg.DB != nil {
		reg.SetInvocationRecorder(NewToolInvocationRecorder(store.New(cfg.DB)))
	}

	lis := bufconn.Listen(1024 * 1024)
	grpcSrv := grpc.NewServer()
	srv := &runtimeServer{
		db:             cfg.DB,
		activeSessions: &sync.Map{},
		toolRegistry:   reg,
		toolDispatch:   &tooldispatch.StreamDispatcher{Registry: reg},
	}
	runtimev1.RegisterRuntimeServer(grpcSrv, srv)
	go func() { _ = grpcSrv.Serve(lis) }()
	t.Cleanup(func() { grpcSrv.Stop() })

	dialer := func(context.Context, string) (net.Conn, error) {
		return lis.Dial()
	}
	cc, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(dialer),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("grpc.NewClient: %v", err)
	}
	t.Cleanup(func() { _ = cc.Close() })

	return &toolE2EHarness{t: t, srv: srv, reg: reg, grpcCC: cc}
}

func (h *toolE2EHarness) startWorker(opts testworker.Options) {
	h.t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	h.workerCancel = cancel
	h.workerWG.Add(1)
	go func() {
		defer h.workerWG.Done()
		_ = testworker.Run(ctx, h.grpcCC, opts)
	}()
	time.Sleep(50 * time.Millisecond)
}

func (h *toolE2EHarness) stopWorker() {
	if h.workerCancel != nil {
		h.workerCancel()
	}
	h.workerWG.Wait()
}

func (h *toolE2EHarness) runTurn(
	agent *manifest.Agent,
	stub provider.Provider,
	stream *mockInteractiveStream,
	input json.RawMessage,
	gate policy.ApprovalGate,
) (stopReason, assistantText string, err error) {
	return h.runTurnWithSessionStart(agent, stub, stream, input, gate, time.Time{})
}

func (h *toolE2EHarness) runTurnWithSessionStart(
	agent *manifest.Agent,
	stub provider.Provider,
	stream *mockInteractiveStream,
	input json.RawMessage,
	gate policy.ApprovalGate,
	sessionStartedAt time.Time,
) (stopReason, assistantText string, err error) {
	st := &interactiveSessionState{
		sessionID:        "sess-e2e",
		agentVersionID:   "av-e2e",
		version:          executor.NewVersionWithProvider("av-e2e", agent, stub),
		toolDispatch:     h.sessionDispatch(),
		policies:         policy.NewEvaluator(agent),
		sessionStartedAt: sessionStartedAt,
	}
	runCtx, cancel := st.runContext(context.Background())
	defer cancel()

	ch := make(chan executor.Event, 32)
	runErrCh := make(chan error, 1)
	go func() {
		runErrCh <- st.version.StreamCompletion(runCtx, executor.RunParams{
			SessionID:     st.sessionID,
			Turn:          st.turnCount + 1,
			Input:         input,
			History:       st.history,
			Dispatcher:    st.toolDispatch,
			Policies:      st.policies,
			ApprovalGate:  gate,
			NewApprovalID: newApprovalID,
		}, ch)
	}()

	var builder strings.Builder
	for ev := range ch {
		switch ev.Type {
		case executor.EventTextDelta:
			builder.WriteString(ev.TextDelta)
			if err := stream.Send(&runtimev1.RunSessionInteractiveServerMsg{
				Body: &runtimev1.RunSessionInteractiveServerMsg_TextDelta{
					TextDelta: &runtimev1.RunSessionInteractiveTextDelta{Delta: ev.TextDelta},
				},
			}); err != nil {
				return "", "", err
			}
		case executor.EventToolCall:
			if err := sendToolCall(stream, ev.ToolCall); err != nil {
				return "", "", err
			}
		case executor.EventToolResult:
			if err := sendToolResult(stream, ev.ToolResult); err != nil {
				return "", "", err
			}
		case executor.EventCompleted:
			if err := <-runErrCh; err != nil {
				return "", "", err
			}
			return ev.StopReason, builder.String(), nil
		case executor.EventFailed:
			if ev.Err != nil {
				return "", "", ev.Err
			}
			return "", "", fmt.Errorf("model completion failed")
		case executor.EventEscalation:
			if err := <-runErrCh; err != nil {
				return "", "", err
			}
			return "", "", ev.Err
		}
	}
	if err := <-runErrCh; err != nil {
		return "", "", err
	}
	return "", "", fmt.Errorf("model completion ended without a terminal event")
}

func e2eWeatherAgent(extra func(*manifest.Agent)) *manifest.Agent {
	const toolWire = "weather_get_forecast"
	agent := &manifest.Agent{
		Metadata: manifest.AgentMetadata{
			Namespace: "e2e",
			Name:      "weather-agent",
		},
		Spec: manifest.AgentSpec{
			Instructions: manifest.InstructionsSpec{Text: "System."},
			Model: manifest.ModelConfig{
				Provider: provider.IDAnthropic,
				Name:     "claude-sonnet-4-5",
			},
			Tools: []manifest.ToolBinding{{
				Ref:             "weather.get-forecast@1.0.0",
				As:              toolWire,
				Version:         "1.0.0",
				InputSchema:     &manifest.SchemaSpec{Inline: map[string]any{"type": "object"}},
				SideEffectClass: manifest.SideEffectReadOnly,
			}},
		},
	}
	if extra != nil {
		extra(agent)
	}
	return agent
}

func e2eWeatherToolCall() provider.ToolCall {
	return provider.ToolCall{
		ID:   "call_1",
		Name: "weather_get_forecast",
		Args: json.RawMessage(`{"city":"NYC"}`),
	}
}

func e2eToolUseThenEndTurn(call provider.ToolCall) *providertest.SequenceStub {
	return providertest.Sequence(
		providertest.ToolUseCompleted(call).Events,
		providertest.DeltaCompleted().Events,
	)
}

func countStreamToolCalls(sent []*runtimev1.RunSessionInteractiveServerMsg) int {
	n := 0
	for _, msg := range sent {
		if msg.GetToolCall() != nil {
			n++
		}
	}
	return n
}

func countStreamToolResults(sent []*runtimev1.RunSessionInteractiveServerMsg) int {
	n := 0
	for _, msg := range sent {
		if msg.GetToolResult() != nil {
			n++
		}
	}
	return n
}

type e2eApprovalGate struct {
	stream  *mockInteractiveStream
	approve bool
	lastReq policy.ApprovalRequest
}

func (g *e2eApprovalGate) WaitApproval(_ context.Context, req policy.ApprovalRequest) (policy.ApprovalResult, error) {
	g.lastReq = req
	if g.stream != nil {
		_ = g.stream.Send(&runtimev1.RunSessionInteractiveServerMsg{
			Body: &runtimev1.RunSessionInteractiveServerMsg_ApprovalRequired{
				ApprovalRequired: approvalRequiredToProto(req),
			},
		})
	}
	return policy.ApprovalResult{Approved: g.approve}, nil
}

func TestToolDispatchE2E_workerRoundTrip(t *testing.T) {
	h := newToolE2EHarness(t, toolE2EConfig{})
	h.startWorker(testworker.Options{
		WorkerID: "w1",
		Handlers: []tooldispatch.HandlerAdvertisement{
			{Tool: "weather.get-forecast", Version: "1.0.0", MaxConcurrency: 2},
		},
		Handler: func(_ context.Context, inv *runtimev1.WorkInvoke) (json.RawMessage, *tooldispatch.ToolError) {
			if string(inv.GetArgs()) != `{"city":"NYC"}` {
				t.Errorf("args = %s", inv.GetArgs())
			}
			return json.RawMessage(`{"temp":72}`), nil
		},
	})
	defer h.stopWorker()

	stream := &mockInteractiveStream{ctx: context.Background()}
	stub := e2eToolUseThenEndTurn(e2eWeatherToolCall())

	stopReason, text, err := h.runTurn(
		e2eWeatherAgent(nil),
		stub,
		stream,
		json.RawMessage(`{"message":"weather?"}`),
		nil,
	)
	if err != nil {
		t.Fatalf("runTurn: %v", err)
	}
	if stopReason != provider.StopReasonEndTurn {
		t.Fatalf("stop_reason = %q, want end_turn", stopReason)
	}
	if text != "Hi there" {
		t.Fatalf("assistant text = %q", text)
	}
	if stub.Calls != 2 {
		t.Fatalf("provider completions = %d, want 2", stub.Calls)
	}
	if countStreamToolCalls(stream.sent) != 1 {
		t.Fatalf("tool_call events = %d, want 1", countStreamToolCalls(stream.sent))
	}
	if countStreamToolResults(stream.sent) != 1 {
		t.Fatalf("tool_result events = %d, want 1", countStreamToolResults(stream.sent))
	}
}

func TestToolDispatchE2E_policyDeny(t *testing.T) {
	h := newToolE2EHarness(t, toolE2EConfig{})
	h.startWorker(testworker.Options{
		Handlers: []tooldispatch.HandlerAdvertisement{
			{Tool: "routing.assign-queue", Version: "default", MaxConcurrency: 2},
		},
	})
	defer h.stopWorker()

	toolName := "assign_queue"
	agent := &manifest.Agent{
		Spec: manifest.AgentSpec{
			Instructions: manifest.InstructionsSpec{Text: "System."},
			Model:        manifest.ModelConfig{Provider: provider.IDAnthropic, Name: "claude"},
			Tools: []manifest.ToolBinding{{
				Ref:  "routing.assign-queue",
				As: toolName,
			}},
			Policies: []manifest.PolicySpec{{
				Name:  "route-only-known-queues",
				Scope: "tool:routing.assign-queue",
				Allow: []string{"motor-standard"},
			}},
		},
	}
	agent.Metadata.Annotations = map[string]string{manifest.AnnotationPoliciesCompiled: "true"}
	call := provider.ToolCall{ID: "c1", Name: toolName, Args: json.RawMessage(`{"queue":"unknown"}`)}
	stub := e2eToolUseThenEndTurn(call)

	stream := &mockInteractiveStream{ctx: context.Background()}
	stopReason, _, err := h.runTurn(agent, stub, stream, json.RawMessage(`{"message":"go"}`), nil)
	if err != nil {
		t.Fatalf("runTurn: %v", err)
	}
	if stopReason != provider.StopReasonEndTurn {
		t.Fatalf("stop_reason = %q", stopReason)
	}
	// A policy deny surfaces the attempt (tool_call) followed by the denial
	// (tool_result) so the timeline shows both, without dispatching to a worker.
	if countStreamToolCalls(stream.sent) != 1 {
		t.Fatalf("tool_call events = %d, want 1", countStreamToolCalls(stream.sent))
	}
	if countStreamToolResults(stream.sent) != 1 {
		t.Fatalf("tool_result events = %d, want 1", countStreamToolResults(stream.sent))
	}
}

func TestToolDispatchE2E_requireApproval(t *testing.T) {
	h := newToolE2EHarness(t, toolE2EConfig{})
	h.startWorker(testworker.Options{
		Handlers: []tooldispatch.HandlerAdvertisement{
			{Tool: "routing.assign-queue", Version: "default", MaxConcurrency: 2},
		},
		Handler: func(_ context.Context, _ *runtimev1.WorkInvoke) (json.RawMessage, *tooldispatch.ToolError) {
			return json.RawMessage(`{"ok":true}`), nil
		},
	})
	defer h.stopWorker()

	toolName := "assign_queue"
	agent := &manifest.Agent{
		Spec: manifest.AgentSpec{
			Instructions: manifest.InstructionsSpec{Text: "System."},
			Model:        manifest.ModelConfig{Provider: provider.IDAnthropic, Name: "claude"},
			Tools: []manifest.ToolBinding{{
				Ref:  "routing.assign-queue",
				As: toolName,
			}},
			Policies: []manifest.PolicySpec{{
				Name:   "severity-approval",
				Scope:  "tool:routing.assign-queue",
				Action: "require_approval",
				Conditions: map[string]any{
					"field": "severity",
					"op":    "gte",
					"value": 3,
				},
				Runtime: map[string]any{"phrony.com/approver_role": "supervisor"},
			}},
		},
	}
	agent.Metadata.Annotations = map[string]string{manifest.AnnotationPoliciesCompiled: "true"}
	call := provider.ToolCall{ID: "c1", Name: toolName, Args: json.RawMessage(`{"severity":4,"queue":"motor-standard"}`)}
	stub := e2eToolUseThenEndTurn(call)

	stream := &mockInteractiveStream{ctx: context.Background()}
	gate := &e2eApprovalGate{stream: stream, approve: true}
	_, _, err := h.runTurn(agent, stub, stream, json.RawMessage(`{"message":"go"}`), gate)
	if err != nil {
		t.Fatalf("runTurn: %v", err)
	}
	if gate.lastReq.Route != "supervisor" {
		t.Fatalf("approval route = %q", gate.lastReq.Route)
	}
	if countStreamToolCalls(stream.sent) != 1 {
		t.Fatal("expected tool_call after approval")
	}
}

func TestToolDispatchE2E_queueUntilWorkerRegisters(t *testing.T) {
	h := newToolE2EHarness(t, toolE2EConfig{})

	stream := &mockInteractiveStream{ctx: context.Background()}
	stub := e2eToolUseThenEndTurn(e2eWeatherToolCall())

	type turnResult struct {
		stopReason, text string
		err            error
	}
	done := make(chan turnResult, 1)
	go func() {
		stopReason, text, err := h.runTurn(
			e2eWeatherAgent(nil),
			stub,
			stream,
			json.RawMessage(`{"message":"weather?"}`),
			nil,
		)
		done <- turnResult{stopReason, text, err}
	}()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if countStreamToolCalls(stream.sent) >= 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if countStreamToolCalls(stream.sent) != 1 {
		t.Fatalf("tool_call events = %d, want 1 while waiting for worker", countStreamToolCalls(stream.sent))
	}
	if h.reg.QueuedCount("weather.get-forecast", "1.0.0") != 1 {
		t.Fatalf("queued = %d, want 1", h.reg.QueuedCount("weather.get-forecast", "1.0.0"))
	}

	h.startWorker(testworker.Options{
		WorkerID: "w1",
		Handlers: []tooldispatch.HandlerAdvertisement{
			{Tool: "weather.get-forecast", Version: "1.0.0", MaxConcurrency: 2},
		},
		Handler: func(_ context.Context, _ *runtimev1.WorkInvoke) (json.RawMessage, *tooldispatch.ToolError) {
			return json.RawMessage(`{"temp":72}`), nil
		},
	})
	defer h.stopWorker()

	select {
	case res := <-done:
		if res.err != nil {
			t.Fatalf("runTurn: %v", res.err)
		}
		if res.stopReason != provider.StopReasonEndTurn {
			t.Fatalf("stop_reason = %q, want end_turn", res.stopReason)
		}
		if countStreamToolResults(stream.sent) != 1 {
			t.Fatalf("tool_result events = %d, want 1", countStreamToolResults(stream.sent))
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for turn to complete")
	}
}

func TestToolDispatchE2E_noHandlerAfterTimeout(t *testing.T) {
	h := newToolE2EHarness(t, toolE2EConfig{})
	max := 1
	agent := e2eWeatherAgent(func(a *manifest.Agent) {
		a.Spec.Limits = &manifest.Limits{MaxWallClockSeconds: &max, OnLimit: "halt"}
		a.Spec.Policies = []manifest.PolicySpec{{
			Name: "no-handler",
			Conditions: map[string]any{
				"field": policy.FieldDispatchTrigger,
				"op":    "eq",
				"value": policy.TriggerDispatchNoHandler,
			},
			Action:  "escalate",
			Runtime: map[string]any{"phrony.com/approver_role": "ops"},
		}}
		a.Metadata.Annotations = map[string]string{manifest.AnnotationPoliciesCompiled: "true"}
	})
	stub := e2eToolUseThenEndTurn(e2eWeatherToolCall())

	stream := &mockInteractiveStream{ctx: context.Background()}
	gate := &e2eApprovalGate{stream: stream, approve: false}
	_, _, err := h.runTurnWithSessionStart(
		agent,
		stub,
		stream,
		json.RawMessage(`{"message":"go"}`),
		gate,
		time.Now().Add(-2*time.Second),
	)
	if err != nil {
		t.Fatalf("runTurn: %v", err)
	}
	if gate.lastReq.Route != "ops" {
		t.Fatalf("route = %q, want ops", gate.lastReq.Route)
	}
}

func TestToolDispatchE2E_capacityQueueFull(t *testing.T) {
	h := newToolE2EHarness(t, toolE2EConfig{MaxQueuePerTool: 1})
	block := make(chan struct{})
	ready := make(chan struct{})
	var readyOnce, unblockOnce sync.Once

	h.startWorker(testworker.Options{
		Handlers: []tooldispatch.HandlerAdvertisement{
			{Tool: "weather.get-forecast", Version: "1.0.0", MaxConcurrency: 1},
		},
		Handler: func(_ context.Context, _ *runtimev1.WorkInvoke) (json.RawMessage, *tooldispatch.ToolError) {
			readyOnce.Do(func() { close(ready) })
			<-block
			return json.RawMessage(`{"temp":1}`), nil
		},
	})
	defer func() {
		unblockOnce.Do(func() { close(block) })
		h.stopWorker()
	}()

	agent := e2eWeatherAgent(func(a *manifest.Agent) {
		a.Spec.Policies = []manifest.PolicySpec{{
			Name: "capacity",
			Conditions: map[string]any{
				"field": policy.FieldDispatchTrigger,
				"op":    "eq",
				"value": policy.TriggerDispatchCapacityExhausted,
			},
			Action:  "escalate",
			Runtime: map[string]any{"phrony.com/approver_role": "capacity-ops"},
		}}
		a.Metadata.Annotations = map[string]string{manifest.AnnotationPoliciesCompiled: "true"}
	})

	// Hold the single worker slot.
	firstDone := make(chan struct{})
	go func() {
		_, _ = h.srv.toolDispatch.Dispatch(context.Background(), tooldispatch.ToolCall{
			CallID:    "hold-slot",
			SessionID: "other",
			Tool:      "weather.get-forecast",
			Version:   "1.0.0",
			Deadline:  time.Now().Add(time.Minute),
		})
		close(firstDone)
	}()
	select {
	case <-ready:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for worker to accept first invoke")
	}

	// Fill the capacity wait queue (max 1).
	queuedDone := make(chan struct{})
	go func() {
		_, _ = h.srv.toolDispatch.Dispatch(context.Background(), tooldispatch.ToolCall{
			CallID:    "queued-1",
			SessionID: "other",
			Tool:      "weather.get-forecast",
			Version:   "1.0.0",
			Deadline:  time.Now().Add(time.Minute),
		})
		close(queuedDone)
	}()
	time.Sleep(30 * time.Millisecond)

	stream := &mockInteractiveStream{ctx: context.Background()}
	gate := &e2eApprovalGate{stream: stream, approve: false}
	stub := e2eToolUseThenEndTurn(e2eWeatherToolCall())
	_, _, err := h.runTurn(agent, stub, stream, json.RawMessage(`{"message":"go"}`), gate)
	if err != nil {
		t.Fatalf("runTurn: %v", err)
	}
	if gate.lastReq.Route != "capacity-ops" {
		t.Fatalf("route = %q, want capacity-ops", gate.lastReq.Route)
	}
	_ = firstDone
	_ = queuedDone
}

func TestToolDispatchE2E_recoveryAfterRestart(t *testing.T) {
	dsn := os.Getenv("RUNTIME_DATABASE_URL")
	if dsn == "" {
		dsn = toolE2EDSN
	}
	db, err := sqlx.Connect("pgx", dsn)
	if err != nil {
		t.Skipf("postgres not available: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := Migrate(db); err != nil {
		t.Skipf("Migrate: %v", err)
	}

	namespace := "tool-e2e-" + uuid.NewString()[:8]
	agentID := uuid.NewString()
	agentVersionID := uuid.NewString()
	sessionID := uuid.NewString()
	callID := tooldispatch.DeriveCallID(sessionID, agentVersionID, 1, 0)
	t.Cleanup(func() { cleanupToolE2EFixture(t, db, namespace, sessionID) })

	manifestJSON, err := json.Marshal(e2eWeatherAgent(nil))
	if err != nil {
		t.Fatalf("Marshal manifest: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO agents (id, namespace, name, owner, labels)
		VALUES ($1, $2, 'weather-agent', 'e2e', '{}'::jsonb)
	`, agentID, namespace); err != nil {
		t.Fatalf("insert agent: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO agent_versions (id, agent_id, version, content_hash, manifest)
		VALUES ($1, $2, '1.0.0', 'hash-e2e', $3::jsonb)
	`, agentVersionID, agentID, manifestJSON); err != nil {
		t.Fatalf("insert agent_version: %v", err)
	}

	historyJSON, err := encodeHistory([]provider.Message{
		{Role: provider.RoleUser, Content: "weather?"},
	})
	if err != nil {
		t.Fatalf("encodeHistory: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO sessions (id, agent_version_id, input, status, history)
		VALUES ($1, $2, '{"message":"weather?"}'::jsonb, $3, $4::jsonb)
	`, sessionID, agentVersionID, model.SessionStatusAwaitingTool, historyJSON); err != nil {
		t.Fatalf("insert session: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO tool_invocations (
			call_id, session_id, agent_version_id, turn, tool, version, args, status
		) VALUES ($1, $2, $3, 1, 'weather.get-forecast', '1.0.0', '{"city":"NYC"}'::jsonb, $4)
	`, callID, sessionID, agentVersionID, model.ToolInvocationQueued); err != nil {
		t.Fatalf("insert tool_invocation: %v", err)
	}

	// Simulated restart: fresh registry and worker, same durable ledger.
	h := newToolE2EHarness(t, toolE2EConfig{DB: db})
	h.srv.db = db
	h.srv.loadSessionVersionFn = func(context.Context, *store.Queries, string) (*executor.Version, error) {
		return executor.NewVersionWithProvider(agentVersionID, e2eWeatherAgent(nil), providertest.DeltaCompleted()), nil
	}
	h.startWorker(testworker.Options{
		Handlers: []tooldispatch.HandlerAdvertisement{
			{Tool: "weather.get-forecast", Version: "1.0.0", MaxConcurrency: 2},
		},
		Handler: func(_ context.Context, _ *runtimev1.WorkInvoke) (json.RawMessage, *tooldispatch.ToolError) {
			return json.RawMessage(`{"temp":72}`), nil
		},
	})
	defer h.stopWorker()

	h.srv.recoverDetachedSession(sessionID)

	q := store.New(db.DB)
	inv, err := waitForToolInvocationStatus(t, q, callID, model.ToolInvocationSucceeded, 30*time.Second)
	var got map[string]any
	if err := json.Unmarshal(inv.Result, &got); err != nil {
		t.Fatalf("result json: %v", err)
	}
	if got["temp"] != float64(72) {
		t.Fatalf("result = %s", inv.Result)
	}

	session, err := q.GetSession(context.Background(), sessionID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	switch session.Status {
	case model.SessionStatusAwaitingInput, model.SessionStatusCompleted:
	default:
		t.Fatalf("session status = %q, want awaiting_input or completed", session.Status)
	}
}

func waitForToolInvocationStatus(t *testing.T, q *store.Queries, callID, wantStatus string, timeout time.Duration) (store.ToolInvocation, error) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		inv, err := q.GetToolInvocation(context.Background(), callID)
		if err != nil {
			return store.ToolInvocation{}, err
		}
		if inv.Status == wantStatus {
			return inv, nil
		}
		if inv.Status == model.ToolInvocationFailed || inv.Status == model.ToolInvocationIndeterminate {
			t.Fatalf("invocation status = %q, waiting for %q", inv.Status, wantStatus)
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for invocation %s status %q", callID, wantStatus)
	return store.ToolInvocation{}, nil
}

func cleanupToolE2EFixture(t *testing.T, db *sqlx.DB, namespace, sessionID string) {
	t.Helper()
	_, _ = db.Exec(`DELETE FROM tool_invocations WHERE session_id = $1`, sessionID)
	_, _ = db.Exec(`DELETE FROM sessions WHERE id = $1`, sessionID)
	_, _ = db.Exec(`DELETE FROM agent_versions WHERE agent_id IN (SELECT id FROM agents WHERE namespace = $1)`, namespace)
	_, _ = db.Exec(`DELETE FROM agents WHERE namespace = $1`, namespace)
}
