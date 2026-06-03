package core

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
	"github.com/phrony-platform/runtime/internal/executor"
	"github.com/phrony-platform/runtime/internal/model"
	"github.com/phrony-platform/runtime/internal/provider"
	"github.com/phrony-platform/runtime/internal/providertest"
	"github.com/phrony-platform/runtime/internal/store"
	"github.com/phrony-platform/runtime/internal/tooldispatch"
	"github.com/phrony-platform/runtime/internal/tooldispatch/testworker"
)

func TestToolDispatchE2E_fiveQueuedCallsRecoveryOnStartup(t *testing.T) {
	db := openToolTestPostgres(t)

	namespace := "tool-rec-" + uuid.NewString()[:8]
	sessionID := uuid.NewString()
	_, agentVersionID, _ := insertToolE2EAgentFixture(t, db, namespace)
	t.Cleanup(func() { cleanupToolE2EFixture(t, db, namespace, sessionID) })

	const turn = 1
	callIDs := make([]string, 5)
	for i := range callIDs {
		callIDs[i] = tooldispatch.DeriveCallID(sessionID, agentVersionID, turn, i)
	}

	history, err := encodeHistory([]provider.Message{
		{Role: provider.RoleUser, Content: "weather?"},
		{
			Role: provider.RoleAssistant,
			Blocks: []provider.ContentBlock{
				provider.ToolUseBlock(callIDs[0], "weather_get_forecast", json.RawMessage(`{"city":"NYC"}`)),
			},
			StopReason: provider.StopReasonToolUse,
		},
	})
	if err != nil {
		t.Fatalf("encodeHistory: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO sessions (id, agent_version_id, input, status, history)
		VALUES ($1, $2, '{"message":"weather?"}'::jsonb, $3, $4::jsonb)
	`, sessionID, agentVersionID, model.SessionStatusAwaitingTool, history); err != nil {
		t.Fatalf("insert session: %v", err)
	}
	for i, callID := range callIDs {
		insertQueuedToolInvocation(t, db, callID, sessionID, agentVersionID, turn, i)
	}

	var dispatchCount atomic.Int32
	h := newToolE2EHarness(t, toolE2EConfig{DB: db, MaxQueuePerTool: 16})
	h.srv.db = db
	h.srv.loadSessionVersionFn = func(context.Context, *store.Queries, string, string) (*executor.Version, error) {
		return executor.NewVersionWithProvider(agentVersionID, e2eWeatherAgent(nil), providertest.DeltaCompleted()), nil
	}
	h.startWorker(testworker.Options{
		Handlers: []tooldispatch.HandlerAdvertisement{
			{Tool: "weather.get-forecast", Version: "1.0.0", MaxConcurrency: 5},
		},
		Handler: func(_ context.Context, inv *runtimev1.WorkInvoke) (json.RawMessage, *tooldispatch.ToolError) {
			dispatchCount.Add(1)
			return json.RawMessage(`{"temp":72}`), nil
		},
	})
	defer h.stopWorker()

	h.srv.reconcileSessionsOnStartup(context.Background())

	q := store.New(db.DB)
	for _, callID := range callIDs {
		if _, err := waitForToolInvocationStatus(t, q, callID, model.ToolInvocationSucceeded, 45*time.Second); err != nil {
			t.Fatalf("wait %s: %v", callID, err)
		}
	}
	if n := int(dispatchCount.Load()); n != 5 {
		t.Fatalf("worker dispatches = %d, want 5", n)
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
