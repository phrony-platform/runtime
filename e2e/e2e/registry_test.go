//go:build integration

package e2e_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/phrony-platform/runtime/e2e/e2e/harness"
)

func TestRegistry_StatusHealthy(t *testing.T) {
	harness.BeginTest(t, "B1", "phrony status reports healthy runtime gRPC", "stdout indicates serving/ok")
	harness.SkipIfNoRuntime(t)
	res := harness.RunPhronyCLI(t, 30*time.Second, nil, "status")
	if res.ExitCode != 0 {
		t.Fatalf("status: exit=%d stderr=%s", res.ExitCode, res.Stderr)
	}
	out := strings.ToLower(res.Stdout)
	if !strings.Contains(out, "serving") && !strings.Contains(out, "ok") {
		t.Fatalf("unexpected status output: %q", res.Stdout)
	}
	harness.Result(t, "runtime status healthy")
}

func TestRegistry_AgentAppearsAfterPublish(t *testing.T) {
	harness.BeginTest(t, "B2", "After publish, agent appears in registry (gRPC ListAgents)", "demo/payment-agent-baseline listed")
	harness.SkipIfNoRuntime(t)
	meta := harness.PublishDeploy(t, "00-baseline-hitl")
	rt := harness.RuntimeClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	ok, err := harness.AgentListed(ctx, rt, meta.Namespace, meta.Name)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatalf("agent %s not listed", meta.AgentRef())
	}
	harness.Result(t, "agent %s found via gRPC", meta.AgentRef())
}

func TestRegistry_VersionsListed(t *testing.T) {
	harness.BeginTest(t, "B3", "phrony versions lists published semver for agent", "stdout contains agent version")
	harness.SkipIfNoRuntime(t)
	meta := harness.PublishDeploy(t, "01-auto-dispatch-low")
	res := harness.RunPhronyCLI(t, 30*time.Second, nil, "agents", "versions", meta.AgentRef())
	if res.ExitCode != 0 {
		t.Fatalf("versions: exit=%d stderr=%s", res.ExitCode, res.Stderr)
	}
	if !strings.Contains(res.Stdout, meta.Version) {
		t.Fatalf("versions output missing %q: %q", meta.Version, res.Stdout)
	}
	harness.Result(t, "versions lists %s", meta.Version)
}
