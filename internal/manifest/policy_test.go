package manifest

import (
	"testing"
)

func TestPolicy_resolvedPolicySpec_requireApprovalPortableFields(t *testing.T) {
	p := &Policy{
		Metadata: PolicyMetadata{Name: "large-payment"},
		Spec: PolicyDocSpec{
			Scope: "tool:payments.charge",
			Decision: &PolicyDecision{
				Type:              "require_approval",
				AuthorityRef:      "claims.payment-authority",
				ApprovalsRequired: 2,
				Reason:            "over limit",
				OnReject:          "fail",
				OnModify:          "revalidate",
				ComprehensionRequired: true,
				Timeout: &PolicyTimeout{
					AfterMinutes: 240,
					Default:      "deny",
				},
				Runtime: map[string]any{
					"phrony.com/approver_role": "senior",
				},
			},
		},
	}
	spec, ok := p.resolvedPolicySpec()
	if !ok {
		t.Fatal("resolvedPolicySpec() = false, want true")
	}
	if spec.Action != "require_approval" {
		t.Fatalf("action = %q", spec.Action)
	}
	if spec.ApprovalsRequired != 2 {
		t.Fatalf("approvals_required = %d, want 2", spec.ApprovalsRequired)
	}
	if spec.AuthorityRef != "claims.payment-authority" {
		t.Fatalf("authority_ref = %q", spec.AuthorityRef)
	}
	if spec.OnReject != "fail" || spec.OnModify != "revalidate" {
		t.Fatalf("on_reject/on_modify = %q / %q", spec.OnReject, spec.OnModify)
	}
	if !spec.ComprehensionRequired {
		t.Fatal("comprehension_required = false, want true")
	}
	if spec.Timeout == nil || spec.Timeout.AfterMinutes != 240 || spec.Timeout.Default != "deny" {
		t.Fatalf("timeout = %#v", spec.Timeout)
	}
	if spec.Runtime["phrony.com/approver_role"] != "senior" {
		t.Fatalf("runtime = %#v", spec.Runtime)
	}
}

func TestPolicy_resolvedPolicySpec_defaultApprovalsRequired(t *testing.T) {
	p := &Policy{
		Metadata: PolicyMetadata{Name: "approve-tool"},
		Spec: PolicyDocSpec{
			Scope: "tool:demo.echo",
			Decision: &PolicyDecision{
				Type: "require_approval",
			},
		},
	}
	spec, ok := p.resolvedPolicySpec()
	if !ok {
		t.Fatal("resolvedPolicySpec() = false, want true")
	}
	if spec.ApprovalsRequired != 1 {
		t.Fatalf("approvals_required = %d, want default 1", spec.ApprovalsRequired)
	}
}
