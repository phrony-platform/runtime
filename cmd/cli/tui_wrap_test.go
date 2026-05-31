package main

import (
	"strings"
	"testing"

	"github.com/mattn/go-runewidth"
)

func maxLineWidth(s string) int {
	max := 0
	for _, line := range strings.Split(s, "\n") {
		if w := runewidth.StringWidth(stripANSI(line)); w > max {
			max = w
		}
	}
	return max
}

func stripANSI(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	esc := false
	for _, r := range s {
		if esc {
			if r == 'm' {
				esc = false
			}
			continue
		}
		if r == '\x1b' {
			esc = true
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func TestWrapTUIText_longLine(t *testing.T) {
	const width = 20
	in := strings.Repeat("a", 50)
	out := wrapTUIText(width, in)
	if !strings.Contains(out, "\n") {
		t.Fatalf("expected wrapped newlines, got %q", out)
	}
	if maxLineWidth(out) > width {
		t.Fatalf("max line width = %d, want <= %d", maxLineWidth(out), width)
	}
}

func TestWrapTUIText_longJSON(t *testing.T) {
	const width = 40
	in := `{"refused":false,"reply":"` + strings.Repeat("x", 80) + `","topics":["a"]}`
	out := wrapTUILines(width, in)
	if maxLineWidth(out) > width {
		t.Fatalf("max line width = %d, want <= %d; out=%q", maxLineWidth(out), width, out)
	}
}

func TestWrapTUILines_preservesBlankLines(t *testing.T) {
	out := wrapTUILines(30, "hello\n\nworld")
	if !strings.Contains(out, "\n\n") {
		t.Fatalf("expected blank line preserved, got %q", out)
	}
}

func TestWrapTUIText_emptyAndZeroWidth(t *testing.T) {
	if wrapTUIText(0, "hello") != "hello" {
		t.Fatal("zero width should return input unchanged")
	}
	if wrapTUIText(40, "") != "" {
		t.Fatal("empty string should stay empty")
	}
}

func TestRunTUIBodyContentWidth(t *testing.T) {
	m := newRunTUI(nil, nil, nil)
	m.width = 80
	if got := m.bodyContentWidth(); got != 72 {
		t.Fatalf("bodyContentWidth() = %d, want 72", got)
	}
	m.width = 12
	if got := m.bodyContentWidth(); got != 10 {
		t.Fatalf("bodyContentWidth() = %d, want minimum 10", got)
	}
}
