package main

import (
	"fmt"
	"os"

	"github.com/phrony-platform/runtime/internal/clierr"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if err := runRoot(args); err != nil {
		fmt.Fprintln(os.Stderr, clierr.Format(err))
		return 1
	}
	return 0
}
