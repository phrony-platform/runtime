package main

import (
	"log/slog"
	"os"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, nil)))
	if err := runRoot(args); err != nil {
		slog.Error("command failed", "error", err)
		return 1
	}
	return 0
}
