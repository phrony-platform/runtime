package manifest

import "testing"

func TestCanRedispatchAfterIndeterminate(t *testing.T) {
	if !CanRedispatchAfterIndeterminate(SideEffectReadOnly) {
		t.Fatal("read_only should allow redispatch")
	}
	if !CanRedispatchAfterIndeterminate(SideEffectIdempotentWrite) {
		t.Fatal("idempotent_write should allow redispatch")
	}
	if CanRedispatchAfterIndeterminate(SideEffectNonIdempotentWrite) {
		t.Fatal("non_idempotent_write should not allow redispatch")
	}
}
