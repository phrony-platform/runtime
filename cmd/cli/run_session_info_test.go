package main

import (
	"strings"
	"testing"

	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
)

func TestFormatTokenUsage(t *testing.T) {
	if got := formatTokenUsage(nil); got != "—" {
		t.Fatalf("nil = %q, want em dash", got)
	}
	got := formatTokenUsage(&runtimev1.TokenUsage{
		InputTokens:  10,
		OutputTokens: 5,
		TotalTokens:  15,
	})
	if got != "10 in / 5 out (15 total)" {
		t.Fatalf("got %q", got)
	}
	est := formatTokenUsage(&runtimev1.TokenUsage{
		InputTokens:  1,
		OutputTokens: 2,
		TotalTokens:  3,
		Estimated:    true,
	})
	if est != "~1 in / 2 out (3 total)" {
		t.Fatalf("estimated = %q", est)
	}
}

func TestFormatSessionStatsLine(t *testing.T) {
	line := formatSessionStatsLine(&runtimev1.InteractiveSessionStats{
		Turn: 2,
		TurnUsage: &runtimev1.TokenUsage{
			InputTokens:  100,
			OutputTokens: 40,
			TotalTokens:  140,
		},
		SessionUsage: &runtimev1.TokenUsage{
			InputTokens:  200,
			OutputTokens: 80,
			TotalTokens:  280,
		},
	}, "end_turn")
	if line == "" {
		t.Fatal("expected non-empty stats line")
	}
	for _, want := range []string{"turn 2", "end_turn", "turn tokens", "session tokens"} {
		if !strings.Contains(line, want) {
			t.Fatalf("line = %q, want substring %q", line, want)
		}
	}
}
