package cliout

import (
	"fmt"
	"io"
	"strings"
)

// WriteTable prints a borderless aligned table.
func WriteTable(w io.Writer, headers []string, rows [][]string) error {
	if len(headers) == 0 {
		return nil
	}
	widths := make([]int, len(headers))
	for i, h := range headers {
		widths[i] = len(h)
	}
	for _, row := range rows {
		for i, cell := range row {
			if i >= len(widths) {
				break
			}
			if n := len(cell); n > widths[i] {
				widths[i] = n
			}
		}
	}

	var b strings.Builder
	writeRow(&b, headers, widths)
	for _, row := range rows {
		writeRow(&b, row, widths)
	}
	_, err := io.WriteString(w, b.String())
	return err
}

func writeRow(b *strings.Builder, cells []string, widths []int) {
	for i, cell := range cells {
		if i > 0 {
			b.WriteString("  ")
		}
		if i < len(widths) {
			b.WriteString(fmt.Sprintf("%-*s", widths[i], cell))
		} else {
			b.WriteString(cell)
		}
	}
	b.WriteByte('\n')
}
