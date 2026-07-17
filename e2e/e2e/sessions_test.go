//go:build integration

package e2e_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/phrony-platform/runtime/e2e/e2e/harness"
)

const sessionAwaitingApproval = "awaiting_approval"

func TestSessions_AutoDispatchLowAmount(t *testing.T) {
	harness.BeginTest(t, "D1", "Payment ≤1000 auto-dispatches without approval (stub calls process_payment@500)", "detached run parks at awaiting_input; worker receipt; no pending approval")
	harness.SkipIfNoRuntime(t)
	requireWorker(t)
	meta := harness.PublishDeploy(t, "01-auto-dispatch-low")
	rt := harness.RuntimeClient(t)

	sessionID := harness.RunDetached(t, meta, `{"message":"Pay 500 USD to Acme"}`)
	terminal := harness.WaitDetachedRunDone(t, rt, sessionID, 2*time.Minute)

	if !Worker().ContainsReceipt() {
		t.Fatalf("worker missing receipt; output=%q", Worker().Output())
	}
	harness.Result(t, "worker printed payment receipt")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := harness.PendingApprovalForSession(ctx, rt, sessionID); err == nil {
		t.Fatal("unexpected pending approval for auto-dispatch payment")
	}
	harness.Result(t, "no pending approval for session %s", sessionID)

	res := harness.RunPhronyCLI(t, 30*time.Second, nil, "sessions", "ls", meta.AgentRef(), "--status", terminal)
	if res.ExitCode != 0 {
		t.Fatalf("sessions ls: %s", res.Stderr)
	}
	if !strings.Contains(res.Stdout, sessionID) {
		t.Fatalf("session %s not listed with status %q: %q", sessionID, terminal, res.Stdout)
	}
	harness.Result(t, "session %s listed as %s", sessionID, terminal)
}

func TestSessions_HITLApprove(t *testing.T) {
	harness.BeginTest(t, "D2/D3", "Payment >1000 triggers HITL; CLI approve resumes dispatch", "awaiting_approval → approve → completed + worker receipt")
	harness.SkipIfNoRuntime(t)
	requireWorker(t)
	meta := harness.PublishDeploy(t, "00-baseline-hitl")
	rt := harness.RuntimeClient(t)

	sessionID := harness.RunDetached(t, meta, `{"message":"Pay 1500 USD to Acme Corp"}`)
	harness.WaitSessionStatus(t, rt, sessionID, sessionAwaitingApproval, 2*time.Minute)

	approvalID := harness.WaitForPendingApproval(t, rt, sessionID, 2*time.Minute)
	res := harness.ApproveCLI(t, approvalID, "e2e ok")
	if res.ExitCode != 0 {
		t.Fatalf("approve: %s", res.Stderr)
	}

	harness.WaitDetachedRunDone(t, rt, sessionID, 2*time.Minute)
	if !Worker().ContainsReceipt() {
		t.Fatalf("worker missing receipt after approve")
	}
	harness.Result(t, "HITL approve path completed with worker receipt")
}

func TestSessions_HITLReject(t *testing.T) {
	harness.BeginTest(t, "D4", "Operator reject on HITL payment does not dispatch to worker", "awaiting_approval → reject → completed without new receipt")
	harness.SkipIfNoRuntime(t)
	w := requireWorker(t)
	before := w.Output()

	meta := harness.PublishDeploy(t, "00-baseline-hitl")
	rt := harness.RuntimeClient(t)

	sessionID := harness.RunDetached(t, meta, `{"message":"Pay 1500 USD to Acme Corp"}`)
	harness.WaitSessionStatus(t, rt, sessionID, sessionAwaitingApproval, 2*time.Minute)
	approvalID := harness.WaitForPendingApproval(t, rt, sessionID, 2*time.Minute)

	res := harness.RejectCLI(t, approvalID, "e2e deny")
	if res.ExitCode != 0 {
		t.Fatalf("reject: %s", res.Stderr)
	}

	harness.WaitDetachedRunDone(t, rt, sessionID, 2*time.Minute)
	after := w.Output()
	if strings.Count(after, "payment processed:") > strings.Count(before, "payment processed:") {
		t.Fatal("worker received payment receipt after rejection")
	}
	harness.Result(t, "reject completed session without worker receipt")
}

func TestSessions_InspectShowsMetadata(t *testing.T) {
	harness.BeginTest(t, "D6", "sessions inspect shows session id and status after completed run", "phrony sessions inspect prints session metadata")
	harness.SkipIfNoRuntime(t)
	requireWorker(t)
	meta := harness.PublishDeploy(t, "01-auto-dispatch-low")
	rt := harness.RuntimeClient(t)
	sessionID := harness.RunDetached(t, meta, `{"message":"Pay 500 USD to Acme"}`)
	harness.WaitDetachedRunDone(t, rt, sessionID, 2*time.Minute)

	res := harness.RunPhronyCLI(t, 30*time.Second, nil, "sessions", "inspect", sessionID)
	if res.ExitCode != 0 {
		t.Fatalf("sessions inspect: %s", res.Stderr)
	}
	if !strings.Contains(res.Stdout, sessionID) {
		t.Fatalf("inspect missing session id: %q", res.Stdout)
	}
	harness.Result(t, "sessions inspect ok for %s", sessionID)
}
