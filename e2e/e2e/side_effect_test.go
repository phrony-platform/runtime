//go:build integration

package e2e_test

import (
	"testing"
	"time"

	"github.com/phrony-platform/runtime/e2e/e2e/harness"
)

func TestSideEffect_IndeterminateReadOnly(t *testing.T) {
	harness.BeginTest(t, "F6a", "read_only + worker drop → tool error, no approval", "detached run ends without awaiting_approval")
	runIndeterminateWithoutApproval(t, "18-indeterminate-read-only")
}

func TestSideEffect_IndeterminateIdempotentWrite(t *testing.T) {
	harness.BeginTest(t, "F6b", "idempotent_write + worker drop → tool error, no approval", "detached run ends without awaiting_approval")
	runIndeterminateWithoutApproval(t, "20-indeterminate-idempotent")
}

func TestSideEffect_IndeterminateNonIdempotentWrite(t *testing.T) {
	harness.BeginTest(t, "F7a", "non_idempotent_write + worker drop → HITL", "awaiting_approval")
	runIndeterminateWithApproval(t, "19-indeterminate-non-idempotent")
}

func TestSideEffect_IndeterminateIrreversibleAction(t *testing.T) {
	harness.BeginTest(t, "F7b", "irreversible_action + worker drop → HITL", "awaiting_approval")
	runIndeterminateWithApproval(t, "21-indeterminate-irreversible")
}

func runIndeterminateWithoutApproval(t *testing.T, scenario string) {
	t.Helper()
	harness.SkipIfNoRuntime(t)
	stopSharedWorker(t)
	ind, err := harness.StartIndeterminateWorker()
	if err != nil {
		t.Fatalf("start indeterminate worker: %v", err)
	}
	t.Cleanup(func() {
		ind.Stop()
		restartWorker(t)
	})

	meta := harness.PublishDeploy(t, scenario)
	rt := harness.RuntimeClient(t)
	sessionID := harness.RunDetached(t, meta, `{"message":"Pay 500 USD to Acme"}`)
	harness.WaitDetachedRunWithoutApproval(t, rt, sessionID, 90*time.Second)
}

func runIndeterminateWithApproval(t *testing.T, scenario string) {
	t.Helper()
	harness.SkipIfNoRuntime(t)
	stopSharedWorker(t)
	ind, err := harness.StartIndeterminateWorker()
	if err != nil {
		t.Fatalf("start indeterminate worker: %v", err)
	}
	t.Cleanup(func() {
		ind.Stop()
		restartWorker(t)
	})

	meta := harness.PublishDeploy(t, scenario)
	rt := harness.RuntimeClient(t)
	sessionID := harness.RunDetached(t, meta, `{"message":"Pay 500 USD to Acme"}`)
	harness.WaitIndeterminateEscalation(t, rt, sessionID, 90*time.Second)
}
