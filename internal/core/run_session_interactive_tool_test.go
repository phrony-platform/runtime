package core

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
	"github.com/phrony-platform/runtime/internal/executor"
	"github.com/phrony-platform/runtime/internal/manifest"
	"github.com/phrony-platform/runtime/internal/policy"
	"github.com/phrony-platform/runtime/internal/provider"
	"github.com/phrony-platform/runtime/internal/tooldispatch"
	"github.com/phrony-platform/runtime/internal/tooldispatch/testworker"
)

func TestInteractiveSessionState_runTurn_toolWorkerRoundTrip(t *testing.T) {
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
	st := &interactiveSessionState{
		sessionID:      "sess-e2e",
		agentVersionID: "av-e2e",
		version:        executor.NewVersionWithProvider("av-e2e", e2eWeatherAgent(nil), stub),
		toolDispatch:   h.srv.toolDispatch,
		policies:       policy.NewEvaluator(e2eWeatherAgent(nil)),
	}

	stopReason, text, _, err := st.runTurn(context.Background(), nil, stream, json.RawMessage(`{"message":"weather?"}`))
	if err != nil {
		t.Fatalf("runTurn: %v", err)
	}
	if stopReason != provider.StopReasonEndTurn {
		t.Fatalf("stop_reason = %q, want end_turn", stopReason)
	}
	if text != "Hi there" {
		t.Fatalf("assistant text = %q", text)
	}
	if countStreamToolCalls(stream.sent) != 1 {
		t.Fatalf("tool_call events = %d, want 1", countStreamToolCalls(stream.sent))
	}
	if countStreamToolResults(stream.sent) != 1 {
		t.Fatalf("tool_result events = %d, want 1", countStreamToolResults(stream.sent))
	}
}

func TestInteractiveSessionState_runTurn_requireApprovalViaToolApproval(t *testing.T) {
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
	gate := newSessionApprovalGate(nil, "sess-e2e", sessionEventsFromStream(stream), nil, "av-e2e")
	st := &interactiveSessionState{
		sessionID:      "sess-e2e",
		agentVersionID: "av-e2e",
		version:        executor.NewVersionWithProvider("av-e2e", agent, stub),
		toolDispatch:   h.srv.toolDispatch,
		policies:       policy.NewEvaluator(agent),
		approvalGate:   gate,
	}
	gate.hitl = st

	approved := make(chan struct{})
	go func() {
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			if req := gate.pendingApproval(); req != nil {
				if err := gate.deliverApproval(&runtimev1.RunSessionInteractiveToolApproval{
					ApprovalId: req.ApprovalID,
					Approved:   true,
				}); err != nil {
					t.Errorf("deliverApproval: %v", err)
				}
				close(approved)
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
		t.Error("timed out waiting for pending approval")
	}()

	_, _, _, err := st.runTurn(context.Background(), nil, stream, json.RawMessage(`{"message":"go"}`))
	if err != nil {
		t.Fatalf("runTurn: %v", err)
	}
	select {
	case <-approved:
	case <-time.After(time.Second):
		t.Fatal("approval was not delivered")
	}
	if countStreamToolCalls(stream.sent) != 1 {
		t.Fatalf("tool_call events = %d, want 1", countStreamToolCalls(stream.sent))
	}
}
