package main

import (
	"io"
	"os"

	"golang.org/x/term"
)

func interactiveUseTUI(stdin io.Reader, stdout io.Writer) bool {
	if os.Getenv("PHRONY_NO_TUI") != "" {
		return false
	}
	in, okIn := stdin.(interface{ Fd() uintptr })
	out, okOut := stdout.(interface{ Fd() uintptr })
	if !okIn || !okOut {
		return false
	}
	return term.IsTerminal(int(in.Fd())) && term.IsTerminal(int(out.Fd()))
}
