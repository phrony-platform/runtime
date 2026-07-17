package harness

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

// E2E logs go to stderr (visible with go test -v; without -v, passing tests discard output).
// Set E2E_LOG=0 to disable; make test-e2e uses E2E_LOG=1 and -v.
func e2eLogEnabled() bool {
	v := strings.TrimSpace(os.Getenv("E2E_LOG"))
	if v == "0" || strings.EqualFold(v, "false") {
		return false
	}
	return true
}

func emitLog(t *testing.T, format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	if e2eLogEnabled() {
		_, _ = fmt.Fprintf(os.Stderr, "e2e: %s\n", msg)
		return
	}
	if t != nil {
		t.Helper()
		t.Log(msg)
	}
}

// Action logs a step the harness is about to perform.
func Action(t *testing.T, format string, args ...any) {
	t.Helper()
	emitLog(t, "  → action: %s", fmt.Sprintf(format, args...))
}

// Result logs an observed outcome (pass path).
func Result(t *testing.T, format string, args ...any) {
	t.Helper()
	emitLog(t, "  ✓ result: %s", fmt.Sprintf(format, args...))
}

// Note logs extra context without implying pass/fail.
func Note(t *testing.T, format string, args ...any) {
	t.Helper()
	emitLog(t, "  · note:   %s", fmt.Sprintf(format, args...))
}

// Logf is a formatted line on stderr (and t.Log when t is non-nil).
func Logf(t *testing.T, format string, args ...any) {
	t.Helper()
	emitLog(t, format, args...)
}

// LogPhronyResult logs CLI exit code and trimmed stdout/stderr.
func LogPhronyResult(t *testing.T, res PhronyResult) {
	t.Helper()
	emitLog(t, "  · phrony: exit=%d", res.ExitCode)
	if s := strings.TrimSpace(res.Stdout); s != "" {
		emitLog(t, "    stdout: %s", truncateLog(s, 500))
	}
	if s := strings.TrimSpace(res.Stderr); s != "" {
		emitLog(t, "    stderr: %s", truncateLog(s, 500))
	}
}

func truncateLog(s string, max int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}
