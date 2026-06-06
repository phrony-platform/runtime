package sessionids

import (
	"strings"
	"testing"
)

func TestChildFromCallID_stableAndDistinct(t *testing.T) {
	first := ChildFromCallID("call-abc")
	if first != ChildFromCallID("call-abc") {
		t.Fatal("ChildFromCallID must be stable for a given call id")
	}
	if first == ChildFromCallID("call-xyz") {
		t.Fatal("distinct call ids must map to distinct child sessions")
	}
	if !strings.HasPrefix(first, "run_") {
		t.Fatalf("child session id = %q, want run_ prefix", first)
	}
}
