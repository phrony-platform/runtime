package main

import (
	"strings"
	"testing"
)

func TestWrapTUIText_longLine(t *testing.T) {
	const width = 20
	in := strings.Repeat("a", 50)
	out := wrapTUIText(width, in)
	if !strings.Contains(out, "\n") {
		t.Fatalf("expected wrapped newlines, got %q", out)
	}
	for _, line := range strings.Split(out, "\n") {
		if len(line) > width+2 {
			t.Fatalf("line longer than width: len=%d %q", len(line), line)
		}
	}
}

func TestWrapTUILines_preservesBlankLines(t *testing.T) {
	out := wrapTUILines(30, "hello\n\nworld")
	if !strings.Contains(out, "\n\n") {
		t.Fatalf("expected blank line preserved, got %q", out)
	}
}
