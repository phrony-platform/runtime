package main

import (
	"strings"
	"testing"
	"time"

	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
)

func TestRenderMessageMetaBar(t *testing.T) {
	bar := renderMessageMetaBar(60, turnDisplayMeta{
		stats: &runtimev1.InteractiveSessionStats{
			Turn: 2,
			TurnUsage: &runtimev1.TokenUsage{
				InputTokens: 50, OutputTokens: 20, TotalTokens: 70,
			},
		},
		stopReason: "end_turn",
		duration:   800 * time.Millisecond,
	})
	for _, want := range []string{"⏱", "800ms", "▲", "▼", "Σ", "end_turn"} {
		if !strings.Contains(bar, want) {
			t.Fatalf("bar = %q, want %q", bar, want)
		}
	}
}

func TestRenderAgentBlock_includesMetaInsidePanel(t *testing.T) {
	block := renderAgentBlock(40, "AGENT", "hello", turnMeta(
		&runtimev1.InteractiveSessionStats{
			Turn: 1,
			TurnUsage: &runtimev1.TokenUsage{InputTokens: 10, OutputTokens: 5, TotalTokens: 15},
		},
		"end_turn",
		0,
	))
	if !strings.Contains(block, "#1") {
		t.Fatalf("block = %q, want turn badge", block)
	}
	if i, j := strings.Index(block, "#1"), strings.Index(block, "AGENT"); i < 0 || j < 0 || i > j {
		t.Fatalf("block = %q, want #N before AGENT", block)
	}
	if !strings.Contains(block, "▲") || !strings.Contains(block, "hello") {
		t.Fatalf("block = %q, want tokens and body inside panel", block)
	}
}
