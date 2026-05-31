package core

import (
	"encoding/json"
	"testing"

	"github.com/phrony-platform/runtime/internal/provider"
)

func TestPersistSessionOutput_includesCurrentTurnUsage(t *testing.T) {
	prior := provider.TokenUsage{InputTokens: 100, OutputTokens: 40}
	turn := provider.TokenUsage{InputTokens: 50, OutputTokens: 10}

	session := prior
	session.Add(turn)
	raw, err := marshalSessionOutput("reply", "stop", turn, session)
	if err != nil {
		t.Fatal(err)
	}
	_, got := usageFromSessionOutputJSON(raw)
	if got.InputTokens != 150 || got.OutputTokens != 50 {
		t.Fatalf("persisted session_usage = %+v, want 150 in / 50 out", got)
	}

	// Buggy order: marshal before Add omits the latest turn from persisted output.
	stale := prior
	staleRaw, err := marshalSessionOutput("reply", "stop", turn, stale)
	if err != nil {
		t.Fatal(err)
	}
	_, staleGot := usageFromSessionOutputJSON(staleRaw)
	if staleGot.InputTokens != 100 {
		t.Fatalf("stale marshal session_usage = %+v, want 100 in (demonstrates bug)", staleGot)
	}
}

func TestMarshalSessionOutput_roundTripUsage(t *testing.T) {
	raw, err := marshalSessionOutput("hi", "stop", provider.TokenUsage{InputTokens: 10, OutputTokens: 5}, provider.TokenUsage{InputTokens: 30, OutputTokens: 12})
	if err != nil {
		t.Fatal(err)
	}
	turn, session := usageFromSessionOutputJSON(raw)
	if turn.InputTokens != 10 || turn.OutputTokens != 5 {
		t.Fatalf("turn usage = %+v", turn)
	}
	if session.InputTokens != 30 || session.OutputTokens != 12 {
		t.Fatalf("session usage = %+v", session)
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		t.Fatal(err)
	}
	if _, ok := obj["session_usage"]; !ok {
		t.Fatal("missing session_usage in output")
	}
}
