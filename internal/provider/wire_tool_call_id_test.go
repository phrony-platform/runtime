package provider

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/phrony-platform/runtime/internal/tooldispatch"
)

func TestWireToolCallID_shortPassthrough(t *testing.T) {
	if got := WireToolCallID("call_abc"); got != "call_abc" {
		t.Fatalf("WireToolCallID() = %q, want passthrough", got)
	}
}

func TestWireToolCallID_longDeterministic(t *testing.T) {
	long := strings.Repeat("a", 80)
	got := WireToolCallID(long)
	if len(got) > MaxWireToolCallIDLen {
		t.Fatalf("WireToolCallID() len = %d, want <= %d", len(got), MaxWireToolCallIDLen)
	}
	if got != WireToolCallID(long) {
		t.Fatal("WireToolCallID should be deterministic")
	}
	if !strings.HasPrefix(got, "call_") {
		t.Fatalf("WireToolCallID() = %q, want call_ prefix", got)
	}
}

func TestWireToolCallID_deriveCallID(t *testing.T) {
	callID := tooldispatch.DeriveCallID(uuid.NewString(), uuid.NewString(), 1, 0)
	if len(callID) <= MaxWireToolCallIDLen {
		t.Fatalf("DeriveCallID len = %d, want > %d for this test", len(callID), MaxWireToolCallIDLen)
	}
	got := WireToolCallID(callID)
	if len(got) > MaxWireToolCallIDLen {
		t.Fatalf("WireToolCallID() len = %d, want <= %d", len(got), MaxWireToolCallIDLen)
	}
}
