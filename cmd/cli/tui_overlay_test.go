package main

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestOverlayCenter_preservesDimensionsAndCenters(t *testing.T) {
	bg := strings.Join([]string{
		"..........",
		"..........",
		"..........",
		"..........",
		"..........",
	}, "\n")
	fg := strings.Join([]string{
		"AAAA",
		"AAAA",
	}, "\n")

	got := overlayCenter(bg, fg)
	lines := strings.Split(got, "\n")

	if len(lines) != 5 {
		t.Fatalf("got %d lines, want 5 (background height preserved)", len(lines))
	}
	for i, line := range lines {
		if w := ansi.StringWidth(line); w != 10 {
			t.Fatalf("line %d width = %d, want 10", i, w)
		}
	}
	// 2-row foreground centered in 5 rows starts at row 1.
	if !strings.Contains(lines[1], "AAAA") || !strings.Contains(lines[2], "AAAA") {
		t.Fatalf("foreground not centered vertically: %q", got)
	}
	if strings.Contains(lines[0], "A") || strings.Contains(lines[4], "A") {
		t.Fatalf("foreground bled outside its rows: %q", got)
	}
	// 4-wide foreground centered in 10 cols starts at col 3.
	if !strings.HasPrefix(lines[1], "...AAAA...") {
		t.Fatalf("foreground not centered horizontally: %q", lines[1])
	}
}

func TestOverlayCenter_emptyForegroundReturnsBackground(t *testing.T) {
	bg := "hello\nworld"
	if got := overlayCenter(bg, "   "); got != bg {
		t.Fatalf("got %q, want unchanged background", got)
	}
}
