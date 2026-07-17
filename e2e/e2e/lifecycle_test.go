//go:build integration

package e2e_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/phrony-platform/runtime/e2e/e2e/harness"
)

func TestLifecycle_PublishDeployActiveInspect(t *testing.T) {
	harness.BeginTest(t, "C1–C3", "Publish/deploy baseline agent; active, inspect, history CLI", "active shows version; inspect/history succeed")
	harness.SkipIfNoRuntime(t)
	meta := harness.PublishDeploy(t, "00-baseline-hitl")

	res := harness.RunPhronyCLI(t, 30*time.Second, nil, "agents", "active", meta.AgentRef())
	if res.ExitCode != 0 {
		t.Fatalf("active: %s", res.Stderr)
	}
	if !strings.Contains(res.Stdout, meta.Version) {
		t.Fatalf("active missing version %q: %q", meta.Version, res.Stdout)
	}
	harness.Result(t, "active deployment is %s", meta.Version)

	res = harness.RunPhronyCLI(t, 30*time.Second, nil, "agents", "inspect", meta.AgentVersionRef())
	if res.ExitCode != 0 {
		t.Fatalf("inspect: %s", res.Stderr)
	}

	res = harness.RunPhronyCLI(t, 30*time.Second, nil, "agents", "history", meta.AgentRef())
	if res.ExitCode != 0 {
		t.Fatalf("history: %s", res.Stderr)
	}
	harness.Result(t, "inspect and history succeeded")
}

func TestLifecycle_VersionBumpRollbackDeprecateRetire(t *testing.T) {
	harness.BeginTest(t, "C4–C9", "Version bump 1.0.2→1.0.3, diff, rollback, deprecate, retire", "lifecycle commands succeed; deprecated run may fail")
	harness.SkipIfNoRuntime(t)
	dir := harness.ScenarioDir("15-version-bump")
	agentPath := filepath.Join(dir, "agent.yaml")

	const baseVer = "1.0.2"
	const nextVer = "1.0.3"
	baseRef := "demo/payment-agent-bump@" + baseVer
	nextRef := "demo/payment-agent-bump@" + nextVer

	harness.PublishAgent(t, agentPath, baseRef)
	if res := harness.RunPhronyCLI(t, 2*time.Minute, nil, "agents", "deploy", baseRef); res.ExitCode != 0 {
		if harness.PhronyVersionRetired(res) {
			harness.Note(t, "%s retired from prior run; publishing %s", baseRef, nextRef)
			harness.PublishAgent(t, agentPath, nextRef)
			if res := harness.RunPhronyCLI(t, 2*time.Minute, nil, "agents", "deploy", nextRef); res.ExitCode != 0 {
				t.Fatalf("deploy %s: %s", nextRef, res.Stderr)
			}
			harness.Result(t, "lifecycle resumed on %s after %s retired", nextRef, baseRef)
			return
		}
		t.Fatalf("deploy %s: %s", baseRef, res.Stderr)
	}

	data, err := os.ReadFile(agentPath)
	if err != nil {
		t.Fatal(err)
	}
	updated := strings.ReplaceAll(string(data), "version: "+baseVer, "version: "+nextVer)
	if err := os.WriteFile(agentPath, []byte(updated), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.WriteFile(agentPath, data, 0o644)
	})

	harness.PublishAgent(t, agentPath, nextRef)
	if res := harness.RunPhronyCLI(t, 2*time.Minute, nil, "agents", "deploy", nextRef); res.ExitCode != 0 {
		t.Fatalf("deploy %s: %s", nextRef, res.Stderr)
	}
	harness.Result(t, "published and deployed %s and %s", baseVer, nextVer)

	res := harness.RunPhronyCLI(t, 30*time.Second, nil, "agents", "diff", agentPath, baseRef)
	if res.ExitCode != 0 {
		t.Fatalf("diff: %s", res.Stderr)
	}

	if res := harness.RunPhronyCLI(t, 30*time.Second, nil, "rollback", "demo/payment-agent-bump"); res.ExitCode != 0 {
		t.Fatalf("rollback: %s", res.Stderr)
	}
	if res := harness.RunPhronyCLI(t, 30*time.Second, nil, "rollback", "demo/payment-agent-bump", "--to", baseVer); res.ExitCode != 0 {
		t.Fatalf("rollback --to: %s", res.Stderr)
	}
	harness.Result(t, "rollback commands succeeded")

	if res := harness.RunPhronyCLI(t, 30*time.Second, nil, "agents", "deprecate", baseRef); res.ExitCode != 0 {
		t.Fatalf("deprecate: %s", res.Stderr)
	}
	runRes := harness.RunPhronyCLI(t, 2*time.Minute, nil, "run", baseRef, "--input", `{"message":"smoke"}`)
	if runRes.ExitCode == 0 {
		harness.Note(t, "run on deprecated %s succeeded (record if unexpected): %q", baseRef, runRes.Stdout)
	} else {
		harness.Result(t, "run on deprecated version rejected (exit=%d)", runRes.ExitCode)
	}

	if res := harness.RunPhronyCLI(t, 30*time.Second, nil, "agents", "retire", baseRef); res.ExitCode != 0 {
		t.Fatalf("retire: %s", res.Stderr)
	}
	harness.Result(t, "retire succeeded for %s", baseRef)
}
