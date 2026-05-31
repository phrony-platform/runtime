package main

import (
	"strings"
	"testing"
	"time"

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

func TestFormatTurnFooter(t *testing.T) {
	line := formatTurnFooter(&runtimev1.InteractiveSessionStats{
		Turn: 1,
		TurnUsage: &runtimev1.TokenUsage{
			InputTokens: 10, OutputTokens: 5, TotalTokens: 15,
		},
	}, "end_turn", 250*time.Millisecond)
	for _, want := range []string{"turn 1", "250ms", "end_turn", "10 in / 5 out"} {
		if !strings.Contains(line, want) {
			t.Fatalf("line = %q, want substring %q", line, want)
		}
	}
}

func TestFormatWallClockLimit(t *testing.T) {
	start := time.Now().Add(-18 * time.Second)
	got := formatWallClockLimit(start, 30, time.Now())
	if got != "18s / 30s (60%)" {
		t.Fatalf("got %q", got)
	}
	if formatWallClockLimit(time.Time{}, 30, time.Now()) != "" {
		t.Fatal("zero start should be empty")
	}
}

func TestFormatTokenLimitPercent(t *testing.T) {
	u := &runtimev1.TokenUsage{InputTokens: 35, OutputTokens: 35, TotalTokens: 70}
	if got := formatTokenLimitPercent(u, 0); got != "" {
		t.Fatalf("no limit = %q, want empty", got)
	}
	if got := formatTokenLimitPercent(u, 5000); got != "1% of limit" {
		t.Fatalf("got %q, want 1%% of limit", got)
	}
	if got := formatTokenLimitPercent(u, 70); got != "100% of limit" {
		t.Fatalf("got %q, want 100%% of limit", got)
	}
}

func TestFormatDuration(t *testing.T) {
	if got := formatDuration(500 * time.Millisecond); got != "500ms" {
		t.Fatalf("got %q", got)
	}
	if got := formatDuration(1500 * time.Millisecond); got != "1.5s" {
		t.Fatalf("got %q", got)
	}
}
