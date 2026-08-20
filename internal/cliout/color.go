package cliout

import (
	"io"
	"os"

	"golang.org/x/term"
)

// UseColor reports whether ANSI styling should be emitted to w, honoring the
// NO_COLOR convention and only coloring when writing to a terminal.
func UseColor(w io.Writer) bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	f, ok := w.(interface{ Fd() uintptr })
	if !ok {
		return false
	}
	return term.IsTerminal(int(f.Fd()))
}
