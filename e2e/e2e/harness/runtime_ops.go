//go:build integration

package harness

import (
	"context"
	"fmt"
	"regexp"
	"testing"
	"time"

	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
)

var sessionStartedRE = regexp.MustCompile(`session\s+(\S+)\s+started`)

// PublishAgent publishes a manifest; AlreadyExists is treated as success for re-runs.
func PublishAgent(t *testing.T, agentPath, versionRef string) {
	t.Helper()
	res := RunPhronyCLI(t, 2*time.Minute, nil, "agents", "publish", agentPath)
	if res.ExitCode != 0 {
		if PhronyAlreadyExists(res) {
			Note(t, "publish %s: version already published", versionRef)
			return
		}
		t.Fatalf("publish %s: exit=%d stderr=%s", agentPath, res.ExitCode, res.Stderr)
	}
}

// PublishDeploy validates, publishes, and deploys a scenario bundle.
func PublishDeploy(t *testing.T, scenario string) AgentMeta {
	t.Helper()
	agentPath := ScenarioAgentYAML(scenario)
	meta, err := ReadAgentMeta(agentPath)
	if err != nil {
		t.Fatal(err)
	}
	Action(t, "bundle %s → validate, publish, deploy %s", scenario, meta.AgentVersionRef())
	if res := RunPhronyCLI(t, 2*time.Minute, nil, "agents", "validate", agentPath); res.ExitCode != 0 {
		t.Fatalf("validate %s: exit=%d stderr=%s", scenario, res.ExitCode, res.Stderr)
	}
	PublishAgent(t, agentPath, meta.AgentVersionRef())
	ref := meta.AgentVersionRef()
	if res := RunPhronyCLI(t, 2*time.Minute, nil, "agents", "deploy", ref); res.ExitCode != 0 {
		t.Fatalf("deploy %s: exit=%d stderr=%s", ref, res.ExitCode, res.Stderr)
	}
	Result(t, "deployed %s from scenario %s", ref, scenario)
	return meta
}

// RunDetached starts a detached session and returns the session id.
func RunDetached(t *testing.T, meta AgentMeta, inputJSON string) string {
	t.Helper()
	ref := meta.AgentRef()
	args := []string{"run", ref, "--input", inputJSON}
	Action(t, "detached run %s input=%s", ref, inputJSON)
	res := RunPhronyCLI(t, 3*time.Minute, nil, args...)
	if res.ExitCode != 0 {
		t.Fatalf("run %s: exit=%d stderr=%s stdout=%s", ref, res.ExitCode, res.Stderr, res.Stdout)
	}
	m := sessionStartedRE.FindStringSubmatch(res.Stdout)
	if len(m) < 2 {
		t.Fatalf("parse session id from stdout: %q", res.Stdout)
	}
	Result(t, "session started: %s", m[1])
	return m[1]
}

// ApproveCLI approves a pending approval via the CLI (actor from PHRONY_ACTOR or OS user).
func ApproveCLI(t *testing.T, approvalID, comment string) PhronyResult {
	t.Helper()
	return ApproveCLIAs(t, approvalID, "", comment)
}

// ApproveCLIAs approves as the given audit actor (PHRONY_ACTOR). Use distinct actors for quorum.
func ApproveCLIAs(t *testing.T, approvalID, actor, comment string) PhronyResult {
	t.Helper()
	args := []string{"approvals", "approve", approvalID}
	if comment != "" {
		args = append(args, "--comment", comment)
	}
	var extra []string
	if actor != "" {
		extra = []string{"PHRONY_ACTOR=" + actor}
	}
	return RunPhronyCLI(t, 2*time.Minute, extra, args...)
}

// RejectCLI rejects a pending approval via the CLI.
func RejectCLI(t *testing.T, approvalID, comment string) PhronyResult {
	t.Helper()
	args := []string{"approvals", "reject", approvalID}
	if comment != "" {
		args = append(args, "--comment", comment)
	}
	return RunPhronyCLI(t, 2*time.Minute, nil, args...)
}

// ApprovalReceived returns approvals_received for an approval id.
func ApprovalReceived(ctx context.Context, rt runtimev1.RuntimeClient, approvalID string) (received, required int32, status string, err error) {
	resp, err := rt.ListApprovals(ctx, &runtimev1.ListApprovalsRequest{})
	if err != nil {
		return 0, 0, "", err
	}
	for _, a := range resp.GetApprovals() {
		if a.GetId() != approvalID {
			continue
		}
		return a.GetApprovalsReceived(), a.GetApprovalsRequired(), a.GetStatus(), nil
	}
	return 0, 0, "", fmt.Errorf("approval %s not found", approvalID)
}

// AgentListed reports whether an agent appears in ListAgents for a namespace.
func AgentListed(ctx context.Context, rt runtimev1.RuntimeClient, namespace, name string) (bool, error) {
	resp, err := rt.ListAgents(ctx, &runtimev1.ListAgentsRequest{Namespace: namespace})
	if err != nil {
		return false, err
	}
	for _, a := range resp.GetAgents() {
		if a.GetNamespace() == namespace && a.GetName() == name {
			return true, nil
		}
	}
	return false, nil
}
