package manifest

import (
	"testing"
)

func TestParseLogicalRef(t *testing.T) {
	t.Parallel()
	parsed, err := ParseLogicalRef("claims.approve-claim-payment@^1.3")
	if err != nil {
		t.Fatalf("ParseLogicalRef() error = %v", err)
	}
	if parsed.Namespace != "claims" || parsed.Name != "approve-claim-payment" || parsed.Constraint != "^1.3" {
		t.Fatalf("parsed = %+v", parsed)
	}
	if parsed.Raw != "claims.approve-claim-payment" {
		t.Fatalf("Raw = %q", parsed.Raw)
	}
}

func TestMatchesVersion_caretConstraint(t *testing.T) {
	t.Parallel()
	ref, err := ParseLogicalRef("claims.tool@^1.3")
	if err != nil {
		t.Fatalf("ParseLogicalRef() error = %v", err)
	}
	if err := ref.MatchesVersion("1.3.0"); err != nil {
		t.Fatalf("1.3.0: %v", err)
	}
	if err := ref.MatchesVersion("1.4.9"); err != nil {
		t.Fatalf("1.4.9: %v", err)
	}
	if err := ref.MatchesVersion("2.0.0"); err == nil {
		t.Fatal("2.0.0: want error")
	}
	if err := ref.MatchesVersion("1.2.9"); err == nil {
		t.Fatal("1.2.9: want error")
	}
}
