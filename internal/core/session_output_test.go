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
	raw, err := marshalSessionOutput("reply", "stop", turn, session, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, got := usageFromSessionOutputJSON(raw)
	if got.InputTokens != 150 || got.OutputTokens != 50 {
		t.Fatalf("persisted session_usage = %+v, want 150 in / 50 out", got)
	}

	// Buggy order: marshal before Add omits the latest turn from persisted output.
	stale := prior
	staleRaw, err := marshalSessionOutput("reply", "stop", turn, stale, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, staleGot := usageFromSessionOutputJSON(staleRaw)
	if staleGot.InputTokens != 100 {
		t.Fatalf("stale marshal session_usage = %+v, want 100 in (demonstrates bug)", staleGot)
	}
}

func TestMarshalSessionOutput_roundTripUsage(t *testing.T) {
	raw, err := marshalSessionOutput("hi", "stop", provider.TokenUsage{InputTokens: 10, OutputTokens: 5}, provider.TokenUsage{InputTokens: 30, OutputTokens: 12}, nil)
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

func TestEnrichHistoryFromSessionOutput_turnsArray(t *testing.T) {
	history := []provider.Message{
		{Role: provider.RoleUser, Content: "hi"},
		{Role: provider.RoleAssistant, Content: "one"},
		{Role: provider.RoleUser, Content: "again"},
		{Role: provider.RoleAssistant, Content: "two"},
	}
	output, err := json.Marshal(sessionOutput{
		Turns: []sessionTurnRecord{
			{StopReason: "end_turn", TurnUsage: &sessionOutputUsage{InputTokens: 10, OutputTokens: 5}},
			{StopReason: "end_turn", TurnUsage: &sessionOutputUsage{InputTokens: 20, OutputTokens: 8}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	enriched := enrichHistoryFromSessionOutput(history, output)
	if enriched[1].TurnUsage.InputTokens != 10 || enriched[3].TurnUsage.InputTokens != 20 {
		t.Fatalf("enriched = %+v", enriched)
	}
}

func TestMarshalSessionOutput_includesTurnsFromHistory(t *testing.T) {
	history := []provider.Message{
		{Role: provider.RoleUser, Content: "hi"},
		{
			Role: provider.RoleAssistant, Content: "bye", StopReason: "end_turn",
			TurnUsage: provider.TokenUsage{InputTokens: 3, OutputTokens: 2},
		},
	}
	raw, err := marshalSessionOutput("bye", "end_turn", provider.TokenUsage{InputTokens: 3, OutputTokens: 2}, provider.TokenUsage{InputTokens: 3, OutputTokens: 2}, history)
	if err != nil {
		t.Fatal(err)
	}
	var obj sessionOutput
	if err := json.Unmarshal(raw, &obj); err != nil {
		t.Fatal(err)
	}
	if len(obj.Turns) != 1 || obj.Turns[0].TurnUsage == nil || obj.Turns[0].TurnUsage.InputTokens != 3 {
		t.Fatalf("turns = %+v", obj.Turns)
	}
}
