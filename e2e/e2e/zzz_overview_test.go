package e2e_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/phrony-platform/runtime/e2e/e2e/harness"
)

// overviewReportPath is written after the full suite (go test hides passing-test stderr).
const overviewReportPath = "scenario-overview.txt"

// TestScenarioOverview_EndOfRun is in the last *_test.go file so -shuffle=off runs it
// after every other test (including zzz_archive_test.go) has recorded results.
func TestScenarioOverview_EndOfRun(t *testing.T) {
	t.Helper()
	report := filepath.Join(t.TempDir(), "overview.txt")
	f, err := os.Create(report)
	if err != nil {
		t.Fatalf("create overview report: %v", err)
	}
	harness.PrintScenarioOverview(f)
	if err := f.Close(); err != nil {
		t.Fatalf("close overview report: %v", err)
	}
	data, err := os.ReadFile(report)
	if err != nil {
		t.Fatalf("read overview report: %v", err)
	}
	// Stable path for Makefile / local runs (package dir is e2e/).
	out := overviewReportPath
	if err := os.WriteFile(out, data, 0o644); err != nil {
		t.Fatalf("write %s: %v", out, err)
	}
	_, _ = os.Stderr.Write(data)
}
