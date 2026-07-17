package harness

import (
	"bytes"
	"strings"
	"testing"
)

func TestPrintScenarioOverview_sortAndFormat(t *testing.T) {
	ResetScenarioReport()
	recordScenarioResult(ScenarioResult{ID: "B2", Purpose: "b", TestName: "TestB", Status: "PASS"})
	recordScenarioResult(ScenarioResult{ID: "A1", Purpose: "a", TestName: "TestA", Status: "FAIL"})
	recordScenarioResult(ScenarioResult{ID: "A5", Purpose: "c", TestName: "TestC", Status: "SKIP"})

	var buf bytes.Buffer
	PrintScenarioOverview(&buf)
	out := buf.String()
	if !strings.Contains(out, "3 total: 1 passed, 1 failed, 1 skipped") {
		t.Fatalf("summary missing counts: %s", out)
	}
	if !strings.Contains(out, "A1") || !strings.Contains(out, "FAIL") {
		t.Fatalf("missing A1 FAIL: %s", out)
	}
}
