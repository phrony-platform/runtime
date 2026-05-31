package main

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// wrapTUIText wraps s to the given terminal width, breaking long lines (e.g. JSON).
func wrapTUIText(width int, s string) string {
	if s == "" {
		return s
	}
	if width < 1 {
		return s
	}
	return lipgloss.NewStyle().Width(width).Render(s)
}

// wrapTUILines wraps each line independently so explicit newlines in the transcript are preserved.
func wrapTUILines(width int, s string) string {
	if s == "" || width < 1 {
		return s
	}
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		if line == "" {
			continue
		}
		lines[i] = wrapTUIText(width, line)
	}
	return strings.Join(lines, "\n")
}
