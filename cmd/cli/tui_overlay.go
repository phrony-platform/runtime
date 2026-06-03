package main

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// overlayCenter composites the foreground block centered over the background,
// keeping the background's overall dimensions. Both inputs are ANSI strings;
// widths are measured in terminal cells so styling and wide runes stay intact.
func overlayCenter(bg, fg string) string {
	if strings.TrimSpace(fg) == "" {
		return bg
	}
	bgLines := strings.Split(bg, "\n")
	fgLines := strings.Split(fg, "\n")

	fgWidth := 0
	for _, line := range fgLines {
		if w := ansi.StringWidth(line); w > fgWidth {
			fgWidth = w
		}
	}

	x := (maxLineWidthAnsi(bgLines) - fgWidth) / 2
	if x < 0 {
		x = 0
	}
	y := (len(bgLines) - len(fgLines)) / 2
	if y < 0 {
		y = 0
	}

	for i, fgLine := range fgLines {
		row := y + i
		if row < 0 || row >= len(bgLines) {
			continue
		}
		bgLines[row] = overlayLine(bgLines[row], fgLine, x, fgWidth)
	}
	return strings.Join(bgLines, "\n")
}

// overlayLine splices fgLine into bgLine starting at cell column x, where
// fgLine occupies fgWidth cells.
func overlayLine(bgLine, fgLine string, x, fgWidth int) string {
	left := ansi.Truncate(bgLine, x, "")
	if pad := x - ansi.StringWidth(left); pad > 0 {
		left += strings.Repeat(" ", pad)
	}
	right := ansi.TruncateLeft(bgLine, x+fgWidth, "")
	return left + fgLine + right
}

func maxLineWidthAnsi(lines []string) int {
	width := 0
	for _, line := range lines {
		if w := ansi.StringWidth(line); w > width {
			width = w
		}
	}
	return width
}
