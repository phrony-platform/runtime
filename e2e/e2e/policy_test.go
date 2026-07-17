//go:build integration

package e2e_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/phrony-platform/runtime/e2e/e2e/harness"
)

func TestPolicy_DenyBlocksDispatch(t *testing.T) {
	harness.BeginTest(t, "E1", "deny policy blocks tool dispatch when amount > 500", "session completes; worker never receives payment receipt")
	harness.SkipIfNoRuntime(t)
	w := requireWorker(t)
	before := w.Output()

	meta := harness.PublishDeploy(t, "02-deny-block")
	rt := harness.RuntimeClient(t)
	sessionID := harness.RunDetached(t, meta, `{"message":"Pay 800 USD to Acme"}`)
	harness.WaitDetachedRunDone(t, rt, sessionID, 2*time.Minute)

	if strings.Count(w.Output(), "payment processed:") > strings.Count(before, "payment processed:") {
		t.Fatal("worker should not process denied payment")
	}
	harness.Result(t, "deny policy blocked worker dispatch")
}

func TestPolicy_AllowlistCurrencyDeny(t *testing.T) {
	harness.BeginTest(t, "E2", "allow [USD] policy denies EUR payment before dispatch", "session completes; no worker receipt")
	harness.SkipIfNoRuntime(t)
	w := requireWorker(t)
	before := w.Output()

	meta := harness.PublishDeploy(t, "03-allowlist-currency")
	rt := harness.RuntimeClient(t)
	sessionID := harness.RunDetached(t, meta, `{"message":"Pay 100 EUR to Acme"}`)
	harness.WaitDetachedRunDone(t, rt, sessionID, 2*time.Minute)

	if strings.Count(w.Output(), "payment processed:") > strings.Count(before, "payment processed:") {
		t.Fatal("worker should not process non-USD payment under allow policy")
	}
	harness.Result(t, "non-USD payment denied before dispatch")
}

func TestPolicy_QuorumRequiresTwoApprovals(t *testing.T) {
	harness.BeginTest(t, "E3", "approvals_required: 2 needs two CLI approves before dispatch", "first approve insufficient; second completes + receipt")
	harness.SkipIfNoRuntime(t)
	requireWorker(t)
	meta := harness.PublishDeploy(t, "04-quorum-approval")
	rt := harness.RuntimeClient(t)

	sessionID := harness.RunDetached(t, meta, `{"message":"Pay 2000 USD to Acme Corp"}`)
	harness.WaitSessionStatus(t, rt, sessionID, sessionAwaitingApproval, 2*time.Minute)
	approvalID := harness.WaitForPendingApproval(t, rt, sessionID, 2*time.Minute)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if res := harness.ApproveCLIAs(t, approvalID, "e2e-approver-1", "first"); res.ExitCode != 0 {
		t.Fatalf("first approve: %s", res.Stderr)
	}
	received, required, status, err := harness.ApprovalReceived(ctx, rt, approvalID)
	if err != nil {
		t.Fatal(err)
	}
	harness.Note(t, "after first approve: received=%d required=%d status=%s", received, required, status)
	if status == "approved" && received >= required {
		t.Fatalf("first approve should not complete quorum: received=%d required=%d status=%s", received, required, status)
	}

	if res := harness.ApproveCLIAs(t, approvalID, "e2e-approver-2", "second"); res.ExitCode != 0 {
		t.Fatalf("second approve: %s", res.Stderr)
	}
	harness.WaitDetachedRunDone(t, rt, sessionID, 2*time.Minute)
	if !Worker().ContainsReceipt() {
		t.Fatal("worker missing receipt after quorum")
	}
	harness.Result(t, "quorum satisfied after two approves")
}

func TestPolicy_OnModifyRevalidate(t *testing.T) {
	harness.BeginTest(t, "E4", "on_modify: revalidate re-evaluates policy when approver edits args below threshold", "approve with lowered amount → session completed")
	harness.SkipIfNoRuntime(t)
	requireWorker(t)
	meta := harness.PublishDeploy(t, "05-on-modify-revalidate")
	rt := harness.RuntimeClient(t)

	sessionID := harness.RunDetached(t, meta, `{"message":"Pay 1500 USD to Acme Corp"}`)
	harness.WaitSessionStatus(t, rt, sessionID, sessionAwaitingApproval, 2*time.Minute)
	approvalID := harness.WaitForPendingApproval(t, rt, sessionID, 2*time.Minute)

	argsPath := filepath.Join(t.TempDir(), "args.json")
	if err := os.WriteFile(argsPath, []byte(`{"amount":500,"currency":"USD","payee":"Acme Corp"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	res := harness.RunPhronyCLI(t, 2*time.Minute, nil,
		"approvals", "approve", approvalID,
		"--comment", "lower amount",
		"--args", argsPath,
	)
	if res.ExitCode != 0 {
		t.Fatalf("approve with modified args: %s", res.Stderr)
	}
	harness.WaitDetachedRunDone(t, rt, sessionID, 3*time.Minute)
	harness.Result(t, "revalidate path completed session")
}
