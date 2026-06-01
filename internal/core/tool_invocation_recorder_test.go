package core

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/phrony-platform/runtime/internal/model"
	"github.com/phrony-platform/runtime/internal/store"
	"github.com/phrony-platform/runtime/internal/tooldispatch"
)

func TestToolInvocationRecorder_lifecyclePostgres(t *testing.T) {
	db := openToolTestPostgres(t)
	q := store.New(db.DB)
	rec := NewToolInvocationRecorder(q)

	namespace := "rec-" + uuid.NewString()[:8]
	sessionID := uuid.NewString()
	_, agentVersionID, _ := insertToolE2EAgentFixture(t, db, namespace)
	insertToolE2ESessionAwaitingTool(t, db, sessionID, agentVersionID, nil)
	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM tool_invocations WHERE session_id = $1`, sessionID)
		_, _ = db.Exec(`DELETE FROM sessions WHERE id = $1`, sessionID)
		_, _ = db.Exec(`DELETE FROM agent_versions WHERE agent_id IN (SELECT id FROM agents WHERE namespace = $1)`, namespace)
		_, _ = db.Exec(`DELETE FROM agents WHERE namespace = $1`, namespace)
	})

	call := tooldispatch.ToolCall{
		CallID:         tooldispatch.DeriveCallID(sessionID, agentVersionID, 1, 0),
		SessionID:      sessionID,
		AgentVersionID: agentVersionID,
		Turn:           1,
		Tool:           "weather.get-forecast",
		Version:        "1.0.0",
		Args:           json.RawMessage(`{"city":"NYC"}`),
	}
	ctx := context.Background()

	if err := rec.RecordPending(ctx, call, model.ToolInvocationPending); err != nil {
		t.Fatalf("RecordPending: %v", err)
	}
	inv, err := q.GetToolInvocation(ctx, call.CallID)
	if err != nil {
		t.Fatalf("GetToolInvocation pending: %v", err)
	}
	if inv.Status != model.ToolInvocationPending {
		t.Fatalf("status = %q, want pending", inv.Status)
	}

	if err := rec.RecordQueued(ctx, call); err != nil {
		t.Fatalf("RecordQueued: %v", err)
	}
	inv, err = q.GetToolInvocation(ctx, call.CallID)
	if err != nil {
		t.Fatalf("GetToolInvocation queued: %v", err)
	}
	if inv.Status != model.ToolInvocationQueued {
		t.Fatalf("status = %q, want queued", inv.Status)
	}

	if err := rec.RecordDispatched(ctx, tooldispatch.DispatchProvenance{
		Call: call,
		Worker: tooldispatch.WorkerInfo{
			WorkerID:         "w1",
			WorkloadIdentity: "spiffe://worker",
			ImageDigest:      "sha256:abc",
		},
		DescriptorHash: "desc-hash",
	}); err != nil {
		t.Fatalf("RecordDispatched: %v", err)
	}
	inv, err = q.GetToolInvocation(ctx, call.CallID)
	if err != nil {
		t.Fatalf("GetToolInvocation dispatched: %v", err)
	}
	if inv.Status != model.ToolInvocationDispatched {
		t.Fatalf("status = %q, want dispatched", inv.Status)
	}
	if inv.WorkerIdentity != "spiffe://worker" {
		t.Fatalf("worker_identity = %q", inv.WorkerIdentity)
	}

	payload := json.RawMessage(`{"temp":70}`)
	if err := rec.RecordCompleted(ctx, call, tooldispatch.ToolResult{
		CallID:  call.CallID,
		Payload: payload,
	}, nil); err != nil {
		t.Fatalf("RecordCompleted: %v", err)
	}

	res, ok, err := rec.LookupCompleted(ctx, call.CallID)
	if err != nil {
		t.Fatalf("LookupCompleted: %v", err)
	}
	if !ok {
		t.Fatal("LookupCompleted: not found")
	}
	var want, got map[string]any
	if err := json.Unmarshal(payload, &want); err != nil {
		t.Fatalf("want json: %v", err)
	}
	if err := json.Unmarshal(res.Payload, &got); err != nil {
		t.Fatalf("got json: %v", err)
	}
	if got["temp"] != want["temp"] {
		t.Fatalf("payload = %s", res.Payload)
	}

	unfinished, err := q.ListUnfinishedInvocationsBySession(ctx, sessionID)
	if err != nil {
		t.Fatalf("ListUnfinishedInvocationsBySession: %v", err)
	}
	if len(unfinished) != 0 {
		t.Fatalf("unfinished invocations = %d, want 0", len(unfinished))
	}
}

func TestToolInvocationRecorder_recordIndeterminatePostgres(t *testing.T) {
	db := openToolTestPostgres(t)
	q := store.New(db.DB)
	rec := NewToolInvocationRecorder(q)

	namespace := "rec-ind-" + uuid.NewString()[:8]
	sessionID := uuid.NewString()
	_, agentVersionID, _ := insertToolE2EAgentFixture(t, db, namespace)
	insertToolE2ESessionAwaitingTool(t, db, sessionID, agentVersionID, nil)
	callID := tooldispatch.DeriveCallID(sessionID, agentVersionID, 1, 0)
	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM tool_invocations WHERE session_id = $1`, sessionID)
		_, _ = db.Exec(`DELETE FROM agent_versions WHERE agent_id IN (SELECT id FROM agents WHERE namespace = $1)`, namespace)
		_, _ = db.Exec(`DELETE FROM agents WHERE namespace = $1`, namespace)
	})

	call := tooldispatch.ToolCall{
		CallID: callID, SessionID: sessionID, AgentVersionID: agentVersionID,
		Turn: 1, Tool: "weather.get-forecast", Version: "1.0.0",
		Args: json.RawMessage(`{}`),
	}
	ctx := context.Background()
	if err := rec.RecordPending(ctx, call, model.ToolInvocationPending); err != nil {
		t.Fatalf("RecordPending: %v", err)
	}
	if err := rec.RecordDispatched(ctx, tooldispatch.DispatchProvenance{Call: call}); err != nil {
		t.Fatalf("RecordDispatched: %v", err)
	}
	if err := rec.RecordIndeterminate(ctx, call, "worker lost"); err != nil {
		t.Fatalf("RecordIndeterminate: %v", err)
	}
	inv, err := q.GetToolInvocation(ctx, callID)
	if err != nil {
		t.Fatalf("GetToolInvocation: %v", err)
	}
	if inv.Status != model.ToolInvocationIndeterminate {
		t.Fatalf("status = %q, want indeterminate", inv.Status)
	}
}
