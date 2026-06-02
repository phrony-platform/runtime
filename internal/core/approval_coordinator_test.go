package core

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
	"github.com/phrony-platform/runtime/internal/policy"
)

func TestApprovalCoordinator_DecideFromStream_noCoordinatorLegacy(t *testing.T) {
	gate := newSessionApprovalGate(nil, "sess-1", nil, nil, "av-1")
	req := policy.ApprovalRequest{ApprovalID: "appr-1", CallID: "call-1", SessionID: "sess-1"}
	gate.setPending(req)
	if err := gate.deliverApproval(&runtimev1.RunSessionInteractiveToolApproval{
		ApprovalId: "appr-1",
		Approved:   true,
	}); err != nil {
		t.Fatalf("deliverApproval: %v", err)
	}
}

func TestSessionApprovalGate_WaitApproval_withoutDB(t *testing.T) {
	gate := newSessionApprovalGate(nil, "sess-1", nil, nil, "av-1")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		_, _ = gate.WaitApproval(ctx, policy.ApprovalRequest{
			ApprovalID: "appr-1",
			CallID:     "call-1",
			SessionID:  "sess-1",
			Args:       json.RawMessage(`{}`),
		})
	}()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if gate.pendingApproval() != nil {
			if err := gate.deliverApproval(&runtimev1.RunSessionInteractiveToolApproval{
				ApprovalId: "appr-1",
				Approved:   true,
			}); err != nil {
				t.Fatalf("deliverApproval: %v", err)
			}
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("pending approval was not registered")
}
