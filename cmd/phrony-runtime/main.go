package main

import (
	"os"

	"github.com/phrony-platform/runtime/internal/runtimecmd"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	return runtimecmd.Run(args)
}
