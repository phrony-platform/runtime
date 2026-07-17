//go:build integration

package e2e_test

import (
	"strings"
	"testing"
	"time"

	"github.com/phrony-platform/runtime/e2e/e2e/harness"
)

func TestApprovals_ListFilterByAgent(t *testing.T) {
	harness.BeginTest(t, "G1", "approvals list --agent filters pending rows for the scenario agent", "list shows pending approval for HITL session")
	harness.SkipIfNoRuntime(t)
	requireWorker(t)
	meta := harness.PublishDeploy(t, "00-baseline-hitl")
	rt := harness.RuntimeClient(t)

	sessionID := harness.RunDetached(t, meta, `{"message":"Pay 1500 USD to Acme Corp"}`)
	harness.WaitSessionStatus(t, rt, sessionID, sessionAwaitingApproval, 2*time.Minute)
	_ = harness.WaitForPendingApproval(t, rt, sessionID, 2*time.Minute)

	res := harness.RunPhronyCLI(t, 30*time.Second, nil,
		"approvals", "list",
		"--agent", meta.AgentRef(),
		"--status", "pending",
	)
	if res.ExitCode != 0 {
		t.Fatalf("approvals list: %s", res.Stderr)
	}
	if !strings.Contains(res.Stdout, sessionID) && !strings.Contains(res.Stdout, "pending") {
		t.Fatalf("approvals list output unexpected: %q", res.Stdout)
	}
	harness.Result(t, "filtered approvals list contains pending row")
}

func TestApprovals_ApproveWrongID(t *testing.T) {
	harness.BeginTest(t, "G2", "approvals approve with unknown id returns error", "phrony exits non-zero")
	harness.SkipIfNoRuntime(t)
	res := harness.RunPhronyCLI(t, 30*time.Second, nil, "approvals", "approve", "appr-nonexistent-e2e")
	if res.ExitCode == 0 {
		t.Fatal("expected error approving unknown approval id")
	}
	harness.Result(t, "approve unknown id failed as expected (exit=%d)", res.ExitCode)
}

func TestApprovals_DoubleApproveRejected(t *testing.T) {
	harness.BeginTest(t, "G3", "Second approve on same approval id is rejected", "same actor cannot vote twice while approval is pending")
	harness.SkipIfNoRuntime(t)
	requireWorker(t)
	meta := harness.PublishDeploy(t, "04-quorum-approval")
	rt := harness.RuntimeClient(t)

	sessionID := harness.RunDetached(t, meta, `{"message":"Pay 2000 USD to Acme Corp"}`)
	harness.WaitSessionStatus(t, rt, sessionID, sessionAwaitingApproval, 2*time.Minute)
	approvalID := harness.WaitForPendingApproval(t, rt, sessionID, 2*time.Minute)

	const actor = "g3-double-vote-tester"
	first := harness.ApproveCLIAs(t, approvalID, actor, "first")
	if first.ExitCode != 0 {
		t.Fatalf("first approve: %s", first.Stderr)
	}
	if !strings.Contains(first.Stdout, "status: pending") {
		t.Fatalf("first approve should leave approval pending (quorum=2): %q", first.Stdout)
	}
	harness.Result(t, "first approve recorded (still pending)")
	second := harness.ApproveCLIAs(t, approvalID, actor, "second")
	if second.ExitCode == 0 {
		t.Fatal("expected second approve from same actor to fail")
	}
	if !strings.Contains(harness.CombinedOutput(second), "already decided") {
		t.Fatalf("second approve error = %q, want already decided", harness.CombinedOutput(second))
	}
	harness.Result(t, "duplicate vote rejected (exit=%d)", second.ExitCode)
}
