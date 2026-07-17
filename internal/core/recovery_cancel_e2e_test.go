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
	"github.com/phrony-platform/runtime/internal/providertest"
	"github.com/phrony-platform/runtime/internal/store"
	"github.com/phrony-platform/runtime/internal/tooldispatch"
	"github.com/phrony-platform/runtime/internal/tooldispatch/testworker"
)

// CancelSession during recoverDetachedSession must not send a new WorkInvoke.
// A test barrier holds recovery after activeSessions registration so cancel can
// commit in the former race window before Dispatch.
func TestToolDispatchE2E_cancelDuringRecovery_noWorkInvoke(t *testing.T) {
	db := openToolTestPostgres(t)

	namespace := "tool-cancel-rec-" + uuid.NewString()[:8]
	sessionID := uuid.NewString()
	_, agentVersionID, _ := insertToolE2EAgentFixture(t, db, namespace)
	t.Cleanup(func() { cleanupToolE2EFixture(t, db, namespace, sessionID) })

	const turn = 1
	callID := tooldispatch.DeriveCallID(sessionID, agentVersionID, turn, 0)
	insertToolE2ESessionAwaitingTool(t, db, sessionID, agentVersionID, nil)
	insertQueuedToolInvocation(t, db, callID, sessionID, agentVersionID, turn, 0)

	entered := make(chan struct{})
	release := make(chan struct{})
	var invokeCount atomic.Int32

	h := newToolE2EHarness(t, toolE2EConfig{DB: db})
	h.srv.db = db
	h.srv.loadSessionVersionFn = func(context.Context, *store.Queries, string, string) (*executor.Version, error) {
		return executor.NewVersionWithProvider(agentVersionID, e2eWeatherAgent(nil), providertest.DeltaCompleted()), nil
	}
	h.srv.recoverDetachedAfterRegisterFn = func(id string) {
		if id != sessionID {
			t.Errorf("barrier session_id = %q, want %q", id, sessionID)
		}
		close(entered)
		<-release
	}

	h.startWorker(testworker.Options{
		Handlers: []tooldispatch.HandlerAdvertisement{
			{Tool: "weather.get-forecast", Version: "1.0.0", MaxConcurrency: 2},
		},
		Handler: func(_ context.Context, _ *runtimev1.WorkInvoke) (json.RawMessage, *tooldispatch.ToolError) {
			invokeCount.Add(1)
			return json.RawMessage(`{"temp":72}`), nil
		},
	})
	defer h.stopWorker()

	done := make(chan struct{})
	go func() {
		defer close(done)
		h.srv.recoverDetachedSession(sessionID)
	}()

	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for recovery barrier")
	}

	if !h.srv.sessionIsActive(sessionID) {
		t.Fatal("expected recovery to hold an activeSessions slot at the barrier")
	}

	_, err := h.srv.CancelSession(context.Background(), &runtimev1.CancelSessionRequest{
		SessionId: sessionID,
	})
	if err != nil {
		t.Fatalf("CancelSession: %v", err)
	}

	close(release)

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for recoverDetachedSession to exit")
	}

	if n := invokeCount.Load(); n != 0 {
		t.Fatalf("worker WorkInvoke count = %d, want 0 after cancel during recovery", n)
	}

	q := store.New(db.DB)
	session, err := q.GetSession(context.Background(), sessionID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if session.Status != model.SessionStatusCancelled {
		t.Fatalf("session status = %q, want cancelled", session.Status)
	}

	inv, err := q.GetToolInvocation(context.Background(), callID)
	if err != nil {
		t.Fatalf("GetToolInvocation: %v", err)
	}
	if inv.Status == model.ToolInvocationSucceeded || inv.Status == model.ToolInvocationDispatched {
		t.Fatalf("invocation status = %q, want unfinished (no post-cancel WorkInvoke)", inv.Status)
	}
}
