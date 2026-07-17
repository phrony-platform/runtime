package harness

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"testing"
)

// ScenarioResult is one sanity-matrix row after a test finishes.
type ScenarioResult struct {
	ID       string
	Purpose  string
	TestName string
	Status   string // PASS, FAIL, SKIP
}

var (
	reportMu      sync.Mutex
	reportResults []ScenarioResult
)

func recordScenarioResult(r ScenarioResult) {
	reportMu.Lock()
	reportResults = append(reportResults, r)
	reportMu.Unlock()
}

// BeginTest logs the case and registers it for the end-of-run scenario overview.
func BeginTest(t *testing.T, id, purpose, expect string) {
	t.Helper()
	id = strings.TrimSpace(id)
	emitLog(t, "━━━ %s ━━━", id)
	emitLog(t, "  test:   %s", purpose)
	emitLog(t, "  expect: %s", expect)

	t.Cleanup(func() {
		status := "PASS"
		switch {
		case t.Skipped():
			status = "SKIP"
		case t.Failed():
			status = "FAIL"
		}
		recordScenarioResult(ScenarioResult{
			ID:       id,
			Purpose:  purpose,
			TestName: t.Name(),
			Status:   status,
		})
	})
}

// PrintScenarioOverview writes one table of all recorded scenarios to w.
func PrintScenarioOverview(w io.Writer) {
	reportMu.Lock()
	rows := append([]ScenarioResult(nil), reportResults...)
	reportMu.Unlock()

	if len(rows) == 0 {
		fmt.Fprintln(w, "\nNo scenario results recorded (tests must call harness.BeginTest).")
		return
	}

	sort.Slice(rows, func(i, j int) bool {
		return scenarioIDLess(rows[i].ID, rows[j].ID)
	})

	var pass, fail, skip int
	for _, r := range rows {
		switch r.Status {
		case "PASS":
			pass++
		case "FAIL":
			fail++
		case "SKIP":
			skip++
		}
	}

	fmt.Fprintln(w)
	fmt.Fprintln(w, "══════════════════════════════════════════════════════════════")
	fmt.Fprintf(w, "  Scenario overview  (%d total: %d passed, %d failed, %d skipped)\n",
		len(rows), pass, fail, skip)
	fmt.Fprintln(w, "══════════════════════════════════════════════════════════════")
	fmt.Fprintf(w, "  %-8s  %-8s  %s\n", "ID", "RESULT", "TEST")
	fmt.Fprintf(w, "  %-8s  %-8s  %s\n", "────", "──────", "────")
	for _, r := range rows {
		fmt.Fprintf(w, "  %-8s  %-8s  %s\n", r.ID, r.Status, r.TestName)
	}
	fmt.Fprintln(w, "──────────────────────────────────────────────────────────────")
	for _, r := range rows {
		fmt.Fprintf(w, "  %-8s  %s\n", r.ID, truncateLog(r.Purpose, 72))
	}
	fmt.Fprintln(w, "══════════════════════════════════════════════════════════════")
}

// scenarioIDLess sorts sanity ids (A1, B2, …, F10) in matrix order.
func scenarioIDLess(a, b string) bool {
	ra, oa := splitScenarioID(a)
	rb, ob := splitScenarioID(b)
	if oa != ob {
		return oa < ob
	}
	return ra < rb
}

func splitScenarioID(id string) (num int, letter int) {
	id = strings.TrimSpace(id)
	if id == "" {
		return 0, 0
	}
	// Use first letter group (A, B, …) before any digit or punctuation.
	letter = int(strings.ToUpper(string(id[0]))[0])
	// Collect digits for sub-order (e.g. D2/D3 → 2).
	for _, r := range id {
		if r >= '0' && r <= '9' {
			num = num*10 + int(r-'0')
		}
	}
	return num, letter
}

// ResetScenarioReport clears recorded results (for tests of the reporter).
func ResetScenarioReport() {
	reportMu.Lock()
	reportResults = nil
	reportMu.Unlock()
}
