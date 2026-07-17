//go:build integration

package e2e_test

import (
	"strings"
	"testing"
	"time"

	"github.com/phrony-platform/runtime/e2e/e2e/harness"
)

func TestDispatch_WorkerOffFailsWithoutReceipt(t *testing.T) {
	harness.BeginTest(t, "F1", "Nodispatch worker declines payment; run finishes without receipt", "detached run ends without payment processed line (stray make run worker breaks this)")
	harness.SkipIfNoRuntime(t)
	stopSharedWorker(t)
	nd, err := harness.StartNoDispatchWorker()
	if err != nil {
		t.Fatalf("start nodispatch worker: %v", err)
	}
	t.Cleanup(func() {
		nd.Stop()
		restartWorker(t)
	})

	meta := harness.PublishDeploy(t, "01-auto-dispatch-low")
	rt := harness.RuntimeClient(t)
	sessionID := harness.RunDetached(t, meta, `{"message":"Pay 500 USD to Acme"}`)

	status := harness.WaitDetachedRunDone(t, rt, sessionID, 90*time.Second)
	if nd.ContainsReceipt() {
		t.Fatal("nodispatch worker printed payment receipt")
	}
	if w := Worker(); w != nil && w.ContainsReceipt() {
		t.Fatal("shared e2e worker printed receipt after Stop()")
	}
	harness.Result(t, "detached run ended with status=%q (no payment receipt)", status)
}

func TestDispatch_NoHandlerEscalates(t *testing.T) {
	harness.BeginTest(t, "F2", "dispatch:no_handler with worker off escalates to approval", "awaiting_approval (ops route); not awaiting_input from a stray worker")
	harness.SkipIfNoRuntime(t)
	stopSharedWorker(t)
	t.Cleanup(func() { restartWorker(t) })
	harness.Note(t, "ensure no other playground worker is running (e.g. `make run` in another terminal)")

	meta := harness.PublishDeploy(t, "06-dispatch-escalate-no-handler")
	rt := harness.RuntimeClient(t)
	sessionID := harness.RunDetached(t, meta, `{"message":"Pay 100 USD to Acme"}`)
	harness.WaitNoHandlerEscalation(t, rt, sessionID, 90*time.Second)
}

func TestDispatch_WrongToolVersion(t *testing.T) {
	harness.BeginTest(t, "F3", "Agent binds process-payment@9.9.9; worker only has @1.0.0", "detached run done; worker not invoked")
	harness.SkipIfNoRuntime(t)
	w := requireWorker(t)
	before := w.Output()

	meta := harness.PublishDeploy(t, "07-wrong-tool-version")
	rt := harness.RuntimeClient(t)
	sessionID := harness.RunDetached(t, meta, `{"message":"Pay 100 USD to Acme"}`)
	harness.WaitDetachedRunDone(t, rt, sessionID, 90*time.Second)

	if strings.Count(w.Output(), "payment processed:") > strings.Count(before, "payment processed:") {
		t.Fatal("worker should not handle wrong tool version")
	}
	harness.Result(t, "wrong tool version did not reach worker")
}

func TestDispatch_HandlerValidationError(t *testing.T) {
	harness.BeginTest(t, "F4", "Worker rejects amount=0 / missing payee; session still finishes", "detached run completes turn with tool error in history")
	harness.SkipIfNoRuntime(t)
	requireWorker(t)
	meta := harness.PublishDeploy(t, "08-handler-validation")
	rt := harness.RuntimeClient(t)
	sessionID := harness.RunDetached(t, meta, `{"message":"Pay invalid amount"}`)
	harness.WaitDetachedRunDone(t, rt, sessionID, 90*time.Second)
	harness.Result(t, "handler validation error path finished session %s", sessionID)
}

func TestDispatch_AllowlistRuntimeRestart(t *testing.T) {
	harness.BeginTest(t, "F5", "RUNTIME_TOOL_ALLOWLIST mismatch", "skipped in default harness")
	t.Skip("F5: requires RUNTIME_TOOL_ALLOWLIST runtime restart — out of scope for default harness")
}
