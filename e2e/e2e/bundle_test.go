//go:build integration

package e2e_test

import (
	"context"
	"testing"
	"time"

	"github.com/phrony-platform/runtime/e2e/e2e/harness"
)

func TestBundle_PaymentAutoDispatch(t *testing.T) {
	harness.BeginTest(t, "J3", "Bundle orchestrator delegates payment ≤1000; worker receipt", "detached bundle run completes with payment processed line")
	harness.SkipIfNoRuntime(t)
	w := requireWorker(t)
	before := w.Output()

	meta, _ := harness.PublishDeployBundle(t, "22-bundle-payment-auto")
	rt := harness.RuntimeClient(t)
	sessionID := harness.RunBundleDetached(t, meta, "22-bundle-payment-auto", `{"message":"Pay 500 USD to Acme Corp"}`)
	harness.WaitDetachedRunDone(t, rt, sessionID, 2*time.Minute)

	if !w.ContainsReceipt() || w.Output() == before {
		t.Fatal("worker missing payment receipt after bundle delegation")
	}
	harness.Result(t, "bundle payment auto-dispatch completed session %s", sessionID)
}

func TestBundle_PaymentHITL(t *testing.T) {
	harness.BeginTest(t, "J4", "Bundle delegation triggers HITL when specialist payment > 1000", "delegated specialist pending approval then receipt after CLI approve")
	harness.SkipIfNoRuntime(t)
	requireWorker(t)
	meta, _ := harness.PublishDeployBundle(t, "23-bundle-payment-hitl")
	specialist, err := harness.ReadAgentMeta(harness.ScenarioManifest("23-bundle-payment-hitl", "specialists/payment.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	rt := harness.RuntimeClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	excludeBefore, err := harness.SnapshotPendingApprovals(ctx, rt, specialist)
	cancel()
	if err != nil {
		t.Fatal(err)
	}
	sessionID := harness.RunBundleDetached(t, meta, "23-bundle-payment-hitl", `{"message":"Pay 1500 USD to Acme Corp"}`)
	approvalID := harness.WaitForDelegatedPendingApproval(t, rt, sessionID, specialist, excludeBefore, 2*time.Minute)
	approvalCtx, approvalCancel := context.WithTimeout(context.Background(), 30*time.Second)
	childSessionID, err := harness.ApprovalSessionID(approvalCtx, rt, approvalID)
	approvalCancel()
	if err != nil {
		t.Fatal(err)
	}
	if res := harness.ApproveCLI(t, approvalID, "bundle e2e approve"); res.ExitCode != 0 {
		t.Fatalf("approve: %s", res.Stderr)
	}
	harness.WaitDetachedRunDone(t, rt, childSessionID, 2*time.Minute)
	harness.WaitSessionStatusNoToolTimeout(t, rt, sessionID, "awaiting_input", 2*time.Minute)
	if !Worker().ContainsReceipt() {
		t.Fatal("worker missing receipt after bundle HITL approval")
	}
	harness.Result(t, "bundle HITL path completed session %s", sessionID)
}

func TestBundle_SecretsUnion(t *testing.T) {
	harness.BeginTest(t, "J6", "Bundle run resolves union secrets from deployed closure", "orchestrator openai + specialist anthropic without --from")
	harness.SkipIfNoRuntime(t)
	meta, _ := harness.PublishDeployBundle(t, "26-bundle-secrets-union")
	t.Setenv("OPENAI_API_KEY", "sk-e2e-openai")
	t.Setenv("ANTHROPIC_API_KEY", "sk-e2e-anthropic")
	rt := harness.RuntimeClient(t)
	sessionID := harness.RunBundleDetached(t, meta, "26-bundle-secrets-union", `{"message":"Explain bundle secret unions."}`)
	harness.WaitDetachedRunDone(t, rt, sessionID, 2*time.Minute)
	harness.Result(t, "bundle secrets union completed session %s", sessionID)
}

func TestBundle_DelegationWithoutTools(t *testing.T) {
	harness.BeginTest(t, "J5", "Bundle orchestrator delegates to text-only specialist", "detached bundle run completes without worker receipt")
	harness.SkipIfNoRuntime(t)
	w := requireWorker(t)
	before := w.Output()

	meta, _ := harness.PublishDeployBundle(t, "24-bundle-delegation")
	rt := harness.RuntimeClient(t)
	sessionID := harness.RunBundleDetached(t, meta, "24-bundle-delegation", `{"message":"What is the status of payment to Acme Corp?"}`)
	harness.WaitDetachedRunDone(t, rt, sessionID, 2*time.Minute)

	if w.Output() != before && w.ContainsReceipt() {
		t.Fatal("text-only bundle delegation should not invoke payment worker")
	}
	harness.Result(t, "bundle delegation completed session %s without tool dispatch", sessionID)
}
