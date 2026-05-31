package core

import (
	"encoding/json"
	"strings"
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

func TestEncodeDecodeHistory_withTurnUsage(t *testing.T) {
	in := []provider.Message{
		{Role: provider.RoleUser, Content: "hi"},
		{
			Role:        provider.RoleAssistant,
			Content:     "hello",
			StopReason:  "end_turn",
			TurnUsage:   provider.TokenUsage{InputTokens: 10, OutputTokens: 5},
		},
	}
	encoded, err := encodeHistory(in)
	if err != nil {
		t.Fatalf("encodeHistory: %v", err)
	}
	if !strings.Contains(string(encoded), `"turn_usage"`) {
		t.Fatalf("encoded = %s, want turn_usage", encoded)
	}
	decoded, err := decodeHistory(encoded)
	if err != nil {
		t.Fatalf("decodeHistory: %v", err)
	}
	if decoded[1].StopReason != "end_turn" || decoded[1].TurnUsage.InputTokens != 10 {
		t.Fatalf("decoded assistant = %+v", decoded[1])
	}
}

func TestEncodeDecodeHistory_withTurnDurationMs(t *testing.T) {
	in := []provider.Message{
		{Role: provider.RoleUser, Content: "hi"},
		{
			Role:           provider.RoleAssistant,
			Content:        "hello",
			StopReason:     "end_turn",
			TurnUsage:      provider.TokenUsage{InputTokens: 10, OutputTokens: 5},
			TurnDurationMs: 2500,
		},
	}
	encoded, err := encodeHistory(in)
	if err != nil {
		t.Fatalf("encodeHistory: %v", err)
	}
	if !strings.Contains(string(encoded), `"turn_duration_ms":2500`) {
		t.Fatalf("encoded = %s, want turn_duration_ms", encoded)
	}
	decoded, err := decodeHistory(encoded)
	if err != nil {
		t.Fatalf("decodeHistory: %v", err)
	}
	if decoded[1].TurnDurationMs != 2500 {
		t.Fatalf("decoded assistant = %+v", decoded[1])
	}
	proto := historyToProto(decoded)
	if proto[1].GetTurnDurationMs() != 2500 {
		t.Fatalf("proto turn_duration_ms = %d, want 2500", proto[1].GetTurnDurationMs())
	}
}

func TestDecodeHistory_invalidJSON(t *testing.T) {
	_, err := decodeHistory(json.RawMessage(`not-json`))
	if err == nil {
		t.Fatal("decodeHistory() = nil, want error")
	}
}
