package core

import (
	"encoding/json"
	"testing"

	"github.com/phrony-platform/runtime/internal/provider"
)

func TestEncodeDecodeHistory_roundTrip(t *testing.T) {
	in := []provider.Message{
		{Role: provider.RoleUser, Content: "hi"},
		{Role: provider.RoleAssistant, Content: "hello"},
	}
	encoded, err := encodeHistory(in)
	if err != nil {
		t.Fatalf("encodeHistory: %v", err)
	}
	want := `[{"role":"user","content":"hi"},{"role":"assistant","content":"hello"}]`
	if string(encoded) != want {
		t.Fatalf("encoded = %s, want %s", encoded, want)
	}
	decoded, err := decodeHistory(encoded)
	if err != nil {
		t.Fatalf("decodeHistory: %v", err)
	}
	if len(decoded) != len(in) {
		t.Fatalf("len(decoded) = %d, want %d", len(decoded), len(in))
	}
	for i := range in {
		if decoded[i] != in[i] {
			t.Fatalf("decoded[%d] = %+v, want %+v", i, decoded[i], in[i])
		}
	}
}

func TestEncodeHistory_empty(t *testing.T) {
	encoded, err := encodeHistory(nil)
	if err != nil {
		t.Fatalf("encodeHistory: %v", err)
	}
	if string(encoded) != "[]" {
		t.Fatalf("encoded = %s, want []", encoded)
	}
}

func TestDecodeHistory_emptyRaw(t *testing.T) {
	decoded, err := decodeHistory(json.RawMessage(nil))
	if err != nil {
		t.Fatalf("decodeHistory: %v", err)
	}
	if decoded != nil {
		t.Fatalf("decoded = %+v, want nil", decoded)
	}
}
