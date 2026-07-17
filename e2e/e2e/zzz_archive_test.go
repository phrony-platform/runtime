//go:build integration

package e2e_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/phrony-platform/runtime/e2e/e2e/harness"
)

// TestArchive_ArchiveAgent runs in zzz_archive_test.go (before zzz_overview_test.go) and
// uses 16-archive-agent so demo/payment-agent-auto (01-auto-dispatch-low) stays publishable
// across suite runs and in the next make test-e2e.
func TestArchive_ArchiveAgent(t *testing.T) {
	harness.BeginTest(t, "I1", "agents archive deprecates all versions for an agent", "phrony agents archive succeeds; agent still listed")
	harness.SkipIfNoRuntime(t)
	agentPath := harness.ScenarioAgentYAML("16-archive-agent")
	meta, err := harness.ReadAgentMeta(agentPath)
	if err != nil {
		t.Fatal(err)
	}
	if res := harness.RunPhronyCLI(t, 2*time.Minute, nil, "agents", "validate", agentPath); res.ExitCode != 0 {
		t.Fatalf("validate: %s", res.Stderr)
	}
	pub := harness.RunPhronyCLI(t, 2*time.Minute, nil, "agents", "publish", agentPath)
	if pub.ExitCode != 0 && !harness.PhronyAlreadyExists(pub) && !harness.PhronyAgentArchived(pub) {
		t.Fatalf("publish: %s", pub.Stderr)
	}
	if harness.PhronyAgentArchived(pub) {
		harness.Note(t, "agent %s already archived from a prior suite run", meta.AgentRef())
	} else {
		ref := meta.AgentVersionRef()
		if res := harness.RunPhronyCLI(t, 2*time.Minute, nil, "agents", "deploy", ref); res.ExitCode != 0 {
			t.Fatalf("deploy %s: %s", ref, res.Stderr)
		}
	}
	res := harness.RunPhronyCLI(t, 30*time.Second, nil, "agents", "archive", meta.AgentRef())
	if res.ExitCode != 0 {
		out := harness.CombinedOutput(res)
		if strings.Contains(out, "already archived") {
			harness.Result(t, "agent %s already archived", meta.AgentRef())
			return
		}
		t.Fatalf("archive: %s", res.Stderr)
	}
	rt := harness.RuntimeClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	ok, err := harness.AgentListed(ctx, rt, meta.Namespace, meta.Name)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatalf("agent %s missing after archive", meta.AgentRef())
	}
	harness.Result(t, "agent %s archived (still visible in list)", meta.AgentRef())
}
