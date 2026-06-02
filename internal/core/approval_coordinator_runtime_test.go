package core

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/phrony-platform/runtime/internal/model"
	"github.com/phrony-platform/runtime/internal/policy"
	"github.com/phrony-platform/runtime/internal/store"
)

func TestApprovalRuntimeHelpers_emptyRuntime(t *testing.T) {
	if got := approvalTimeoutDefault(nil); got != "deny" {
		t.Fatalf("default = %q", got)
	}
	if got := approvalEscalateRoute(nil, "ops"); got != "ops" {
		t.Fatalf("route = %q", got)
	}
}

func TestApprovalRequestFromStore_roundTrip(t *testing.T) {
	expires := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	runtime, _ := json.Marshal(map[string]any{
		approvalRuntimeTimeoutDefault: "deny",
	})
	row := store.Approval{
		ID: "appr-1", SessionID: "sess-1", CallID: "call-1",
		Status: model.ApprovalStatusPending, Route: "ops",
		ApprovalsRequired: 2, ComprehensionRequired: true,
		OnReject: "fail", OnModify: "revalidate",
		ExpiresAt: &expires, PolicyRuntime: runtime,
		Args: json.RawMessage(`{}`),
	}
	req, err := approvalRequestFromStore(nil, nil, row, "sess-1")
	if err != nil {
		t.Fatalf("approvalRequestFromStore: %v", err)
	}
	if req.ApprovalsRequired != 2 || !req.ComprehensionRequired {
		t.Fatalf("req = %+v", req)
	}
	if req.OnReject != "fail" || req.OnModify != "revalidate" {
		t.Fatalf("on_reject/on_modify = %q / %q", req.OnReject, req.OnModify)
	}
	if req.ExpiresAt == nil {
		t.Fatal("missing expires_at")
	}
}

func TestApprovalRequiredToProto_portableFields(t *testing.T) {
	expires := time.Date(2030, 6, 1, 12, 0, 0, 0, time.UTC)
	proto := approvalRequiredToProto(policy.ApprovalRequest{
		ApprovalID:            "appr-1",
		CallID:                "call-1",
		Tool:                  "payments.charge",
		Version:               "1.0.0",
		Route:                 "ops",
		Reason:                "high amount",
		AuthorityRef:          "claims.supervisor",
		PolicyName:            "payment-policy",
		ApprovalsRequired:     2,
		ApprovalsReceived:     1,
		ComprehensionRequired: true,
		ExpiresAt:             &expires,
		Runtime: map[string]any{
			"phrony.com/approver_role": "senior",
		},
	})
	if proto.GetApprovalsRequired() != 2 || proto.GetApprovalsReceived() != 1 {
		t.Fatalf("approvals = %d/%d", proto.GetApprovalsRequired(), proto.GetApprovalsReceived())
	}
	if !proto.GetComprehensionRequired() || proto.GetExpiresAt() == "" {
		t.Fatalf("proto = %+v", proto)
	}
	if len(proto.GetPolicyRuntime()) == 0 {
		t.Fatal("missing policy_runtime")
	}
}
