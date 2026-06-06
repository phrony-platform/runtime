package core

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
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
)

func sessionEventTypes(events []store.SessionEvent) []string {
	out := make([]string, len(events))
	for i, ev := range events {
		out[i] = ev.Type
	}
	return out
}

func insertSessionEventsE2ESession(t *testing.T, db *sqlx.DB, sessionID, agentVersionID, status string, history []provider.Message) {
	t.Helper()
	historyJSON, err := encodeHistory(history)
	if err != nil {
		t.Fatalf("encodeHistory: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO sessions (id, agent_version_id, input, status, history)
		VALUES ($1, $2, '{"message":"go"}'::jsonb, $3, $4::jsonb)
	`, sessionID, agentVersionID, status, historyJSON); err != nil {
		t.Fatalf("insert session: %v", err)
	}
}

func cleanupSessionEventsE2EFixture(t *testing.T, db *sqlx.DB, namespace, sessionID string) {
	t.Helper()
	_, _ = db.Exec(`DELETE FROM approval_votes WHERE approval_id IN (SELECT id FROM approvals WHERE session_id = $1)`, sessionID)
	_, _ = db.Exec(`DELETE FROM approvals WHERE session_id = $1`, sessionID)
	_, _ = db.Exec(`DELETE FROM tool_invocations WHERE session_id = $1`, sessionID)
	_, _ = db.Exec(`DELETE FROM session_events WHERE session_id = $1`, sessionID)
	_, _ = db.Exec(`DELETE FROM session_evidence WHERE session_id = $1`, sessionID)
	_, _ = db.Exec(`DELETE FROM sessions WHERE id = $1`, sessionID)
	_, _ = db.Exec(`DELETE FROM agent_versions WHERE agent_id IN (SELECT id FROM agents WHERE namespace = $1)`, namespace)
	_, _ = db.Exec(`DELETE FROM agents WHERE namespace = $1`, namespace)
}

func assistantMessageContents(events []store.SessionEvent) []string {
	var out []string
	for _, ev := range events {
		if ev.Type != string(model.SessionEventAssistantMessage) {
			continue
		}
		msg, err := conversationMessageFromSessionEvent(ev.Payload)
		if err != nil {
			continue
		}
		out = append(out, msg.GetContent())
	}
	return out
}

func countSessionEventType(events []store.SessionEvent, typ model.SessionEventType) int {
	n := 0
	for _, ev := range events {
		if ev.Type == string(typ) {
			n++
		}
	}
	return n
}

func (h *toolE2EHarness) runTurnRecorded(
	sessionID, agentVersionID string,
	agent *manifest.Agent,
	stub provider.Provider,
	stream *mockInteractiveStream,
	input json.RawMessage,
	q *store.Queries,
) (stopReason, assistantText string, err error) {
	return h.runTurnRecordedWithGate(sessionID, agentVersionID, agent, stub, stream, input, q,
		newSessionApprovalGate(nil, sessionID, stream, q, agentVersionID))
}

func (h *toolE2EHarness) runTurnRecordedWithGate(
	sessionID, agentVersionID string,
	agent *manifest.Agent,
	stub provider.Provider,
	stream *mockInteractiveStream,
	input json.RawMessage,
	q *store.Queries,
	gate *sessionApprovalGate,
) (stopReason, assistantText string, err error) {
	if gate != nil && gate.coord != nil {
		gate.coord.registerGate(sessionID, gate)
		defer gate.coord.unregisterGate(sessionID)
	}
	st := &interactiveSessionState{
		sessionID:      sessionID,
		agentVersionID: agentVersionID,
		version:        executor.NewVersionWithProvider(agentVersionID, agent, stub),
		toolDispatch:   h.sessionDispatch(),
		policies:       policy.NewEvaluator(agent),
		approvalGate:   gate,
	}
	runCtx, cancel := st.runContext(context.Background())
	defer cancel()
	stopReason, assistantText, _, err = st.runTurn(runCtx, q, stream, input)
	return stopReason, assistantText, err
}

func e2eRequireApprovalAgent() *manifest.Agent {
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
	return agent
}

func e2eRequireApprovalToolCall() provider.ToolCall {
	return provider.ToolCall{
		ID:   "c1",
		Name: "assign_queue",
		Args: json.RawMessage(`{"severity":4,"queue":"motor-standard"}`),
	}
}

func e2ePolicyDenyAgent() *manifest.Agent {
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
	return agent
}

func e2ePolicyDenyToolCall() provider.ToolCall {
	return provider.ToolCall{
		ID:   "c1",
		Name: "assign_queue",
		Args: json.RawMessage(`{"queue":"unknown"}`),
	}
}

func interactiveStreamStepKinds(msgs []*runtimev1.RunSessionInteractiveServerMsg) []string {
	var kinds []string
	for _, msg := range msgs {
		switch {
		case msg.GetSessionStarted() != nil:
			kinds = append(kinds, "session_started")
		case msg.GetToolCall() != nil:
			kinds = append(kinds, "tool_call")
		case msg.GetToolResult() != nil:
			kinds = append(kinds, "tool_result")
		case msg.GetCompleted() != nil:
			kinds = append(kinds, "completed")
		default:
			kinds = append(kinds, "other")
		}
	}
	return kinds
}

func indexOfKind(kinds []string, kind string) int {
	for i, k := range kinds {
		if k == kind {
			return i
		}
	}
	return -1
}

func TestSessionEventsE2E_policyDenyRecordsOrderedAuditLog(t *testing.T) {
	db := openToolTestPostgres(t)
	q := store.New(db.DB)
	h := newToolE2EHarness(t, toolE2EConfig{DB: db})

	namespace := "sevt-" + uuid.NewString()[:8]
	sessionID := uuid.NewString()
	_, agentVersionID, _ := insertToolE2EAgentFixture(t, db, namespace)
	insertSessionEventsE2ESession(t, db, sessionID, agentVersionID, model.SessionStatusRunning, nil)
	t.Cleanup(func() { cleanupSessionEventsE2EFixture(t, db, namespace, sessionID) })

	agent := e2ePolicyDenyAgent()
	stub := e2eToolUseThenEndTurn(e2ePolicyDenyToolCall())
	stream := &mockInteractiveStream{ctx: context.Background()}

	stopReason, text, err := h.runTurnRecorded(sessionID, agentVersionID, agent, stub, stream, json.RawMessage(`{"message":"go"}`), q)
	if err != nil {
		t.Fatalf("runTurnRecorded: %v", err)
	}
	if stopReason != provider.StopReasonEndTurn {
		t.Fatalf("stop_reason = %q, want end_turn", stopReason)
	}
	if text != "Hi there" {
		t.Fatalf("assistant text = %q", text)
	}

	events, err := q.ListSessionEventsBySessionID(context.Background(), sessionID)
	if err != nil {
		t.Fatalf("ListSessionEventsBySessionID: %v", err)
	}
	got := sessionEventTypes(events)
	want := []string{
		string(model.SessionEventUserMessage),
		string(model.SessionEventToolCall),
		string(model.SessionEventPolicyDenied),
		string(model.SessionEventAssistantMessage),
	}
	if len(got) != len(want) {
		t.Fatalf("event types = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("event types = %v, want %v", got, want)
		}
	}
	for i := 1; i < len(events); i++ {
		if events[i].ID <= events[i-1].ID {
			t.Fatalf("events not ordered by id: %+v", events)
		}
	}
}

func TestSessionEventsE2E_attachReplaysToolTimelineOnCompletedSession(t *testing.T) {
	db := openToolTestPostgres(t)
	q := store.New(db.DB)
	h := newToolE2EHarness(t, toolE2EConfig{DB: db})

	namespace := "sevt-" + uuid.NewString()[:8]
	sessionID := uuid.NewString()
	_, agentVersionID, _ := insertToolE2EAgentFixture(t, db, namespace)
	insertSessionEventsE2ESession(t, db, sessionID, agentVersionID, model.SessionStatusRunning, nil)
	t.Cleanup(func() { cleanupSessionEventsE2EFixture(t, db, namespace, sessionID) })

	agent := e2ePolicyDenyAgent()
	stub := e2eToolUseThenEndTurn(e2ePolicyDenyToolCall())
	live := &mockInteractiveStream{ctx: context.Background()}
	if _, _, err := h.runTurnRecorded(sessionID, agentVersionID, agent, stub, live, json.RawMessage(`{"message":"go"}`), q); err != nil {
		t.Fatalf("runTurnRecorded: %v", err)
	}

	history := []provider.Message{
		{Role: provider.RoleUser, Content: "go"},
		{Role: provider.RoleAssistant, Content: "Hi there"},
	}
	historyJSON, err := encodeHistory(history)
	if err != nil {
		t.Fatalf("encodeHistory: %v", err)
	}
	output, err := json.Marshal(map[string]any{
		"message":     "Hi there",
		"stop_reason": provider.StopReasonEndTurn,
	})
	if err != nil {
		t.Fatalf("Marshal output: %v", err)
	}
	now := time.Now()
	if _, err := db.Exec(`
		UPDATE sessions
		SET status = $1, history = $2::jsonb, output = $3::jsonb, updated_at = $4
		WHERE id = $5
	`, model.SessionStatusCompleted, historyJSON, output, now, sessionID); err != nil {
		t.Fatalf("update session completed: %v", err)
	}

	attachStream := &mockInteractiveStream{
		ctx: context.Background(),
		recv: []*runtimev1.RunSessionInteractiveClientMsg{
			{Body: &runtimev1.RunSessionInteractiveClientMsg_Start{
				Start: &runtimev1.RunSessionInteractiveStart{SessionId: sessionID},
			}},
		},
	}
	srv := &runtimeServer{
		db: db,
		loadSessionVersionFn: func(context.Context, *store.Queries, string, string) (*executor.Version, error) {
			return executor.NewVersionWithProvider(agentVersionID, agent, providertest.DeltaCompleted()), nil
		},
	}
	if err := srv.RunSessionInteractive(attachStream); err != nil {
		t.Fatalf("RunSessionInteractive attach: %v", err)
	}

	kinds := interactiveStreamStepKinds(attachStream.sent)
	if indexOfKind(kinds, "session_started") < 0 {
		t.Fatalf("attach stream missing session_started: %v", kinds)
	}
	toolCallIdx := indexOfKind(kinds, "tool_call")
	toolResultIdx := indexOfKind(kinds, "tool_result")
	if toolCallIdx < 0 || toolResultIdx < 0 {
		t.Fatalf("attach stream missing tool timeline: %v", kinds)
	}
	if toolCallIdx >= toolResultIdx {
		t.Fatalf("tool_call at %d should precede tool_result at %d in %v", toolCallIdx, toolResultIdx, kinds)
	}
	if indexOfKind(kinds, "completed") < 0 {
		t.Fatalf("attach stream missing completed: %v", kinds)
	}

	started := attachStream.sent[indexOfKind(kinds, "session_started")].GetSessionStarted()
	hist := started.GetHistory()
	if len(hist) != 2 || hist[0].GetContent() != "go" || hist[1].GetContent() != "Hi there" {
		t.Fatalf("session_started history = %+v", hist)
	}
}

func TestSessionEventsE2E_successfulToolCallRecordsOrderedAuditLog(t *testing.T) {
	db := openToolTestPostgres(t)
	q := store.New(db.DB)
	h := newToolE2EHarness(t, toolE2EConfig{DB: db})
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

	namespace := "sevt-" + uuid.NewString()[:8]
	sessionID := uuid.NewString()
	_, agentVersionID, _ := insertToolE2EAgentFixture(t, db, namespace)
	insertSessionEventsE2ESession(t, db, sessionID, agentVersionID, model.SessionStatusRunning, nil)
	t.Cleanup(func() { cleanupSessionEventsE2EFixture(t, db, namespace, sessionID) })

	stub := e2eToolUseThenEndTurn(e2eWeatherToolCall())
	stream := &mockInteractiveStream{ctx: context.Background()}
	if _, _, err := h.runTurnRecorded(sessionID, agentVersionID, e2eWeatherAgent(nil), stub, stream, json.RawMessage(`{"message":"weather?"}`), q); err != nil {
		t.Fatalf("runTurnRecorded: %v", err)
	}

	events, err := q.ListSessionEventsBySessionID(context.Background(), sessionID)
	if err != nil {
		t.Fatalf("ListSessionEventsBySessionID: %v", err)
	}
	got := sessionEventTypes(events)
	want := []string{
		string(model.SessionEventUserMessage),
		string(model.SessionEventToolCall),
		string(model.SessionEventToolResult),
		string(model.SessionEventAssistantMessage),
	}
	if len(got) != len(want) {
		t.Fatalf("event types = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("event types = %v, want %v", got, want)
		}
	}
}

func TestSessionEventsE2E_assistantSegmentsAtToolBoundary(t *testing.T) {
	db := openToolTestPostgres(t)
	q := store.New(db.DB)
	h := newToolE2EHarness(t, toolE2EConfig{DB: db})
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

	namespace := "sevt-" + uuid.NewString()[:8]
	sessionID := uuid.NewString()
	_, agentVersionID, _ := insertToolE2EAgentFixture(t, db, namespace)
	insertSessionEventsE2ESession(t, db, sessionID, agentVersionID, model.SessionStatusRunning, nil)
	t.Cleanup(func() { cleanupSessionEventsE2EFixture(t, db, namespace, sessionID) })

	call := e2eWeatherToolCall()
	stub := providertest.Sequence(
		[]provider.CompletionEvent{
			{Type: provider.EventTextDelta, TextDelta: "Checking forecast. "},
			{Type: provider.EventToolCall, ToolCall: &call},
			{Type: provider.EventCompleted, StopReason: provider.StopReasonToolUse},
		},
		[]provider.CompletionEvent{
			{Type: provider.EventTextDelta, TextDelta: "Done. "},
			{Type: provider.EventTextDelta, TextDelta: "Warm today."},
			{Type: provider.EventCompleted, StopReason: provider.StopReasonEndTurn},
		},
	)
	stream := &mockInteractiveStream{ctx: context.Background()}
	if _, text, err := h.runTurnRecorded(sessionID, agentVersionID, e2eWeatherAgent(nil), stub, stream, json.RawMessage(`{"message":"weather?"}`), q); err != nil {
		t.Fatalf("runTurnRecorded: %v", err)
	} else if text != "Checking forecast. Done. Warm today." {
		t.Fatalf("assistant text = %q", text)
	}

	segments := assistantMessageContents(mustListSessionEvents(t, q, sessionID))
	if len(segments) != 2 {
		t.Fatalf("assistant segments = %v, want 2", segments)
	}
	if segments[0] != "Checking forecast. " {
		t.Fatalf("pre-tool segment = %q", segments[0])
	}
	if segments[1] != "Done. Warm today." {
		t.Fatalf("post-tool segment = %q", segments[1])
	}
}

func TestSessionEventsE2E_rejectedApprovalRecordsDecided(t *testing.T) {
	db := openToolTestPostgres(t)
	q := store.New(db.DB)
	h := newToolE2EHarness(t, toolE2EConfig{DB: db})
	h.startWorker(testworker.Options{
		Handlers: []tooldispatch.HandlerAdvertisement{
			{Tool: "routing.assign-queue", Version: "default", MaxConcurrency: 2},
		},
	})
	defer h.stopWorker()

	namespace := "sevt-" + uuid.NewString()[:8]
	sessionID := uuid.NewString()
	_, agentVersionID, _ := insertToolE2EAgentFixture(t, db, namespace)
	insertSessionEventsE2ESession(t, db, sessionID, agentVersionID, model.SessionStatusRunning, nil)
	t.Cleanup(func() { cleanupSessionEventsE2EFixture(t, db, namespace, sessionID) })

	agent := e2eRequireApprovalAgent()
	stub := e2eToolUseThenEndTurn(e2eRequireApprovalToolCall())
	stream := &mockInteractiveStream{ctx: context.Background()}
	gate := newSessionApprovalGate(h.srv.approvalCoord(), sessionID, stream, q, agentVersionID)

	go func() {
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			req := gate.pendingApproval()
			if req != nil {
				if _, err := q.GetApproval(context.Background(), req.ApprovalID); err != nil {
					time.Sleep(5 * time.Millisecond)
					continue
				}
				if err := gate.deliverApproval(&runtimev1.RunSessionInteractiveToolApproval{
					ApprovalId: req.ApprovalID,
					Approved:   false,
				}); err != nil {
					t.Errorf("deliverApproval: %v", err)
				}
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
		t.Error("timed out waiting for pending approval")
	}()

	if _, _, err := h.runTurnRecordedWithGate(sessionID, agentVersionID, agent, stub, stream, json.RawMessage(`{"message":"go"}`), q, gate); err != nil {
		t.Fatalf("runTurnRecordedWithGate: %v", err)
	}

	events := mustListSessionEvents(t, q, sessionID)
	if countSessionEventType(events, model.SessionEventApprovalRequired) != 1 {
		t.Fatalf("approval_required count = %d, want 1", countSessionEventType(events, model.SessionEventApprovalRequired))
	}
	if countSessionEventType(events, model.SessionEventApprovalDecided) != 1 {
		t.Fatalf("approval_decided count = %d, want 1", countSessionEventType(events, model.SessionEventApprovalDecided))
	}
	if countSessionEventType(events, model.SessionEventToolResult) != 1 {
		t.Fatalf("tool_result count = %d, want 1 denied result", countSessionEventType(events, model.SessionEventToolResult))
	}
	if countSessionEventType(events, model.SessionEventToolCall) != 0 {
		t.Fatalf("tool_call count = %d, want 0 when approval is rejected before dispatch", countSessionEventType(events, model.SessionEventToolCall))
	}
}

func TestSessionEventsE2E_lifecycleEventsRecorded(t *testing.T) {
	db := openToolTestPostgres(t)
	q := store.New(db.DB)

	namespace := "sevt-" + uuid.NewString()[:8]
	sessionID := uuid.NewString()
	_, agentVersionID, _ := insertToolE2EAgentFixture(t, db, namespace)
	insertSessionEventsE2ESession(t, db, sessionID, agentVersionID, model.SessionStatusRunning, nil)
	t.Cleanup(func() { cleanupSessionEventsE2EFixture(t, db, namespace, sessionID) })

	srv := &runtimeServer{db: db}
	ctx := context.Background()
	output := json.RawMessage(`{"message":"ok","stop_reason":"end_turn"}`)
	stream := &mockInteractiveStream{ctx: ctx}
	if err := srv.completeInteractiveSession(ctx, q, sessionEventsFromStream(stream), sessionID, provider.StopReasonEndTurn, output, 1, provider.TokenUsage{}, provider.TokenUsage{}); err != nil {
		t.Fatalf("completeInteractiveSession: %v", err)
	}
	if countSessionEventType(mustListSessionEvents(t, q, sessionID), model.SessionEventSessionCompleted) != 1 {
		t.Fatal("expected session_completed audit event")
	}

	failStream := &mockInteractiveStream{ctx: ctx}
	if err := srv.failInteractiveSession(ctx, q, sessionEventsFromStream(failStream), sessionID, fmt.Errorf("model unavailable")); err != nil {
		t.Fatalf("failInteractiveSession: %v", err)
	}
	if countSessionEventType(mustListSessionEvents(t, q, sessionID), model.SessionEventSessionFailed) != 1 {
		t.Fatal("expected session_failed audit event")
	}
}

func TestSessionEventsE2E_activeAttachReplaysPersistedToolTimeline(t *testing.T) {
	db := openToolTestPostgres(t)
	q := store.New(db.DB)
	h := newToolE2EHarness(t, toolE2EConfig{DB: db})

	namespace := "sevt-" + uuid.NewString()[:8]
	sessionID := uuid.NewString()
	_, agentVersionID, _ := insertToolE2EAgentFixture(t, db, namespace)
	history := []provider.Message{
		{Role: provider.RoleUser, Content: "weather?"},
	}
	insertSessionEventsE2ESession(t, db, sessionID, agentVersionID, model.SessionStatusRunning, history)
	t.Cleanup(func() { cleanupSessionEventsE2EFixture(t, db, namespace, sessionID) })

	callMsg := toolCallServerMsg(executor.ToolCallEvent{
		CallID: "call-1", Tool: "weather.get-forecast", Version: "1.0.0", Args: json.RawMessage(`{"city":"NYC"}`),
	}, nil)
	resultMsg := toolResultServerMsg(executor.ToolResultEvent{
		CallID: "call-1", Payload: json.RawMessage(`{"temp":72}`),
	})
	for _, spec := range []struct {
		typ     model.SessionEventType
		payload json.RawMessage
	}{
		{model.SessionEventToolCall, marshalSessionEventProto(callMsg)},
		{model.SessionEventToolResult, marshalSessionEventProto(resultMsg)},
	} {
		if _, err := q.InsertSessionEvent(context.Background(), store.InsertSessionEventParams{
			SessionID: sessionID, Type: string(spec.typ), Payload: spec.payload,
		}); err != nil {
			t.Fatalf("InsertSessionEvent(%s): %v", spec.typ, err)
		}
	}

	agent := e2eWeatherAgent(nil)
	h.srv.loadSessionVersionFn = func(context.Context, *store.Queries, string, string) (*executor.Version, error) {
		return executor.NewVersionWithProvider(agentVersionID, agent, providertest.DeltaCompleted()), nil
	}

	hub := newSessionEventHub()
	inputMux := newSessionInputMux(context.Background())
	if err := h.srv.registerActiveSession(sessionID, activeSessionEntry{
		cancel:        func() {},
		eventHub:      hub,
		inputMux:      inputMux,
		liveAssistant: "partial ",
	}); err != nil {
		t.Fatalf("registerActiveSession: %v", err)
	}
	defer h.srv.unregisterActiveSession(sessionID)

	attachCtx, detach := context.WithCancel(context.Background())
	defer detach()
	attachStream := &blockingAfterStartStream{mockInteractiveStream: &mockInteractiveStream{
		ctx: attachCtx,
		recv: []*runtimev1.RunSessionInteractiveClientMsg{
			{Body: &runtimev1.RunSessionInteractiveClientMsg_Start{
				Start: &runtimev1.RunSessionInteractiveStart{SessionId: sessionID},
			}},
		},
	}}
	done := make(chan error, 1)
	go func() { done <- h.srv.RunSessionInteractive(attachStream) }()

	deadline := time.After(2 * time.Second)
	for {
		kinds := interactiveStreamStepKinds(attachStream.sent)
		if indexOfKind(kinds, "session_started") >= 0 &&
			indexOfKind(kinds, "tool_call") >= 0 &&
			indexOfKind(kinds, "tool_result") >= 0 {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("attach stream = %v, want session_started + tool timeline", interactiveStreamStepKinds(attachStream.sent))
		case err := <-done:
			t.Fatalf("attach ended early: %v", err)
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}
	if countStreamToolCalls(attachStream.sent) != 1 {
		t.Fatalf("tool_call replay count = %d, want 1", countStreamToolCalls(attachStream.sent))
	}
	if countStreamToolResults(attachStream.sent) != 1 {
		t.Fatalf("tool_result replay count = %d, want 1", countStreamToolResults(attachStream.sent))
	}
	detach()
	<-done
}

func mustListSessionEvents(t *testing.T, q *store.Queries, sessionID string) []store.SessionEvent {
	t.Helper()
	events, err := q.ListSessionEventsBySessionID(context.Background(), sessionID)
	if err != nil {
		t.Fatalf("ListSessionEventsBySessionID: %v", err)
	}
	return events
}
