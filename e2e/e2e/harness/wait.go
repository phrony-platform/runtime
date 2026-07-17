//go:build integration

package harness

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
)

// dispatchQueueWaitSlack is how long awaiting_tool may last before we assume the runtime
// has no dispatch queue timeout (rebuild/restart with RUNTIME_DISPATCH_QUEUE_WAIT).
const dispatchQueueWaitSlack = 25 * time.Second

const pollInterval = 200 * time.Millisecond

// Detached phrony run (non --attach) parks successful turns at awaiting_input, not completed.
var detachedRunTerminalStatuses = []string{"awaiting_input", "completed", "failed"}

// sessionTerminalStatuses are statuses that will not progress toward a different wait target.
var sessionTerminalStatuses = []string{"awaiting_input", "completed", "failed", "cancelled"}

// pollContext returns a context cancelled on test interrupt (Ctrl+C), test timeout, or poll timeout.
func pollContext(t *testing.T, timeout time.Duration) (context.Context, context.CancelFunc) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	if d, ok := t.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	return context.WithDeadline(t.Context(), deadline)
}

func pollSleep(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(pollInterval):
		return nil
	}
}

func failOnPollInterrupt(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		return
	}
	if errors.Is(err, context.Canceled) {
		t.Fatal("interrupted")
	}
}

func isSessionTerminal(status string) bool {
	for _, s := range sessionTerminalStatuses {
		if status == s {
			return true
		}
	}
	return false
}

// failIfUnexpectedTerminal fails when the session reached a terminal status other than wantStatus.
func failIfUnexpectedTerminal(t *testing.T, sessionID, status, wantStatus string) {
	t.Helper()
	if status == wantStatus || !isSessionTerminal(status) {
		return
	}
	t.Fatalf("session %s reached terminal status=%q while waiting for %q", sessionID, status, wantStatus)
}

// WaitDetachedRunDone polls until a detached session finishes its turn.
func WaitDetachedRunDone(t *testing.T, rt runtimev1.RuntimeClient, sessionID string, timeout time.Duration) string {
	t.Helper()
	Action(t, "poll session %s until detached run done (awaiting_input|completed|failed)", sessionID)
	ctx, cancel := pollContext(t, timeout)
	defer cancel()
	start := time.Now()
	var last string
	for {
		status, err := SessionStatus(ctx, rt, sessionID)
		if err != nil {
			failOnPollInterrupt(t, err)
			t.Fatalf("session status: %v", err)
		}
		if status != last {
			Note(t, "session %s status=%q", sessionID, status)
			last = status
		}
		if status == "awaiting_tool" && time.Since(start) > dispatchQueueWaitSlack {
			t.Fatalf("session %s stuck in awaiting_tool — rebuild and restart runtime (needs RUNTIME_DISPATCH_QUEUE_WAIT); for worker-off cases use StartNoDispatchWorker or no-handler policy", sessionID)
		}
		for _, want := range detachedRunTerminalStatuses {
			if status == want {
				Result(t, "detached session %s finished with status=%q", sessionID, status)
				return status
			}
		}
		if err := pollSleep(ctx); err != nil {
			failOnPollInterrupt(t, err)
			t.Fatalf("session %s status = %q, want one of %v within %s", sessionID, last, detachedRunTerminalStatuses, timeout)
		}
	}
}

// WaitIndeterminateEscalation polls until indeterminate dispatch escalates to awaiting_approval.
// Fails fast if the session finishes without approval (read_only / idempotent_write path).
func WaitIndeterminateEscalation(t *testing.T, rt runtimev1.RuntimeClient, sessionID string, timeout time.Duration) {
	t.Helper()
	Action(t, "poll session %s until awaiting_approval (indeterminate escalate)", sessionID)
	ctx, cancel := pollContext(t, timeout)
	defer cancel()
	start := time.Now()
	var last string
	for {
		status, err := SessionStatus(ctx, rt, sessionID)
		if err != nil {
			failOnPollInterrupt(t, err)
			t.Fatalf("session status: %v", err)
		}
		if status != last {
			Note(t, "session %s status=%q", sessionID, status)
			last = status
		}
		if status == "awaiting_tool" && time.Since(start) > dispatchQueueWaitSlack {
			t.Fatalf("session %s stuck in awaiting_tool — rebuild and restart runtime (needs RUNTIME_DISPATCH_QUEUE_WAIT)", sessionID)
		}
		switch status {
		case "awaiting_approval":
			Result(t, "indeterminate dispatch escalated to awaiting_approval")
			return
		case "awaiting_input", "completed", "failed":
			t.Fatalf("session %s reached %q before awaiting_approval (expected HITL for non-idempotent side_effect_class)", sessionID, status)
		}
		if err := pollSleep(ctx); err != nil {
			failOnPollInterrupt(t, err)
			t.Fatalf("session %s status = %q, want awaiting_approval within %s", sessionID, last, timeout)
		}
	}
}

// WaitDetachedRunWithoutApproval polls until a detached run finishes without ever reaching awaiting_approval.
func WaitDetachedRunWithoutApproval(t *testing.T, rt runtimev1.RuntimeClient, sessionID string, timeout time.Duration) string {
	t.Helper()
	Action(t, "poll session %s until detached done without awaiting_approval", sessionID)
	ctx, cancel := pollContext(t, timeout)
	defer cancel()
	start := time.Now()
	var last string
	for {
		status, err := SessionStatus(ctx, rt, sessionID)
		if err != nil {
			failOnPollInterrupt(t, err)
			t.Fatalf("session status: %v", err)
		}
		if status != last {
			Note(t, "session %s status=%q", sessionID, status)
			last = status
		}
		if status == "awaiting_approval" {
			t.Fatalf("session %s reached awaiting_approval (expected tool error for read_only/idempotent_write)", sessionID)
		}
		if status == "awaiting_tool" && time.Since(start) > dispatchQueueWaitSlack {
			t.Fatalf("session %s stuck in awaiting_tool — use StartIndeterminateWorker for indeterminate side-effect tests", sessionID)
		}
		for _, want := range detachedRunTerminalStatuses {
			if status == want {
				Result(t, "detached session %s finished with status=%q (no approval)", sessionID, status)
				return status
			}
		}
		if err := pollSleep(ctx); err != nil {
			failOnPollInterrupt(t, err)
			t.Fatalf("session %s status = %q, want one of %v within %s", sessionID, last, detachedRunTerminalStatuses, timeout)
		}
	}
}

// WaitNoHandlerEscalation polls until dispatch:no_handler escalates to awaiting_approval.
// Fails fast if a stray worker handled the tool (session reaches awaiting_input first).
func WaitNoHandlerEscalation(t *testing.T, rt runtimev1.RuntimeClient, sessionID string, timeout time.Duration) {
	t.Helper()
	Action(t, "poll session %s until awaiting_approval (no-handler escalate) or fail if tool dispatched", sessionID)
	ctx, cancel := pollContext(t, timeout)
	defer cancel()
	start := time.Now()
	var last string
	for {
		status, err := SessionStatus(ctx, rt, sessionID)
		if err != nil {
			failOnPollInterrupt(t, err)
			t.Fatalf("session status: %v", err)
		}
		if status != last {
			Note(t, "session %s status=%q", sessionID, status)
			last = status
		}
		if status == "awaiting_tool" && time.Since(start) > dispatchQueueWaitSlack {
			t.Fatalf("session %s stuck in awaiting_tool — rebuild and restart runtime (needs RUNTIME_DISPATCH_QUEUE_WAIT / dispatch queue timeout); stop stray `make run` workers", sessionID)
		}
		switch status {
		case "awaiting_approval":
			Result(t, "no-handler escalated to awaiting_approval")
			return
		case "awaiting_input":
			t.Fatal("session reached awaiting_input — a worker processed the tool (stop other `make run` workers; only e2e worker should be stopped)")
		case "completed", "failed":
			t.Fatalf("session %s reached %q before awaiting_approval (expected escalate on no handler)", sessionID, status)
		}
		if err := pollSleep(ctx); err != nil {
			failOnPollInterrupt(t, err)
			t.Fatalf("session %s status = %q, want awaiting_approval within %s (is a worker still connected?)", sessionID, last, timeout)
		}
	}
}

// WaitSessionStatusNoToolTimeout polls like WaitSessionStatus but does not fail fast
// when the session stays in awaiting_tool. Use when a parent orchestrator waits on
// a delegated child that may still be driving nested tool calls after approval.
func WaitSessionStatusNoToolTimeout(t *testing.T, rt runtimev1.RuntimeClient, sessionID, wantStatus string, timeout time.Duration) {
	t.Helper()
	Action(t, "poll gRPC session %s until status=%q without awaiting_tool timeout (timeout %s)", sessionID, wantStatus, timeout)
	ctx, cancel := pollContext(t, timeout)
	defer cancel()
	var last string
	for {
		status, err := SessionStatus(ctx, rt, sessionID)
		if err != nil {
			failOnPollInterrupt(t, err)
			t.Fatalf("session status: %v", err)
		}
		if status != last {
			Note(t, "session %s status=%q (waiting for %q)", sessionID, status, wantStatus)
			last = status
		}
		if status == wantStatus {
			Result(t, "session %s reached status=%q", sessionID, wantStatus)
			return
		}
		failIfUnexpectedTerminal(t, sessionID, status, wantStatus)
		if err := pollSleep(ctx); err != nil {
			failOnPollInterrupt(t, err)
			t.Fatalf("session %s status = %q, want %q within %s", sessionID, last, wantStatus, timeout)
		}
	}
}

// WaitSessionStatus polls until the session reaches wantStatus or timeout.
func WaitSessionStatus(t *testing.T, rt runtimev1.RuntimeClient, sessionID, wantStatus string, timeout time.Duration) {
	t.Helper()
	Action(t, "poll gRPC session %s until status=%q (timeout %s)", sessionID, wantStatus, timeout)
	ctx, cancel := pollContext(t, timeout)
	defer cancel()
	start := time.Now()
	var last string
	for {
		status, err := SessionStatus(ctx, rt, sessionID)
		if err != nil {
			failOnPollInterrupt(t, err)
			t.Fatalf("session status: %v", err)
		}
		if status != last {
			Note(t, "session %s status=%q (waiting for %q)", sessionID, status, wantStatus)
			last = status
		}
		if status == "awaiting_tool" && wantStatus != "awaiting_tool" && time.Since(start) > dispatchQueueWaitSlack {
			t.Fatalf("session %s stuck in awaiting_tool — rebuild and restart runtime (needs RUNTIME_DISPATCH_QUEUE_WAIT)", sessionID)
		}
		if status == wantStatus {
			Result(t, "session %s reached status=%q", sessionID, wantStatus)
			return
		}
		failIfUnexpectedTerminal(t, sessionID, status, wantStatus)
		if err := pollSleep(ctx); err != nil {
			failOnPollInterrupt(t, err)
			t.Fatalf("session %s status = %q, want %q within %s", sessionID, last, wantStatus, timeout)
		}
	}
}

// SessionStatus returns the current status for sessionID (root or delegated child).
func SessionStatus(ctx context.Context, rt runtimev1.RuntimeClient, sessionID string) (string, error) {
	resp, err := rt.InspectSession(ctx, &runtimev1.InspectSessionRequest{SessionId: sessionID})
	if err != nil {
		return "", err
	}
	sess := resp.GetSession()
	if sess == nil || strings.TrimSpace(sess.GetId()) == "" {
		return "", fmt.Errorf("session %s not found", sessionID)
	}
	return sess.GetStatus(), nil
}

// PendingApprovalForAgent returns the first pending approval id for sessions of the agent.
func PendingApprovalForAgent(ctx context.Context, rt runtimev1.RuntimeClient, meta AgentMeta) (string, error) {
	resp, err := rt.ListApprovals(ctx, &runtimev1.ListApprovalsRequest{
		Status:         "pending",
		AgentNamespace: meta.Namespace,
		AgentName:      meta.Name,
	})
	if err != nil {
		return "", err
	}
	for _, a := range resp.GetApprovals() {
		if a.GetStatus() == "pending" {
			return a.GetId(), nil
		}
	}
	return "", fmt.Errorf("no pending approval for agent %s", meta.AgentRef())
}

// SnapshotPendingApprovals records pending approval ids for an agent before a run
// starts so delegated HITL tests can ignore stale rows left by earlier runs.
func SnapshotPendingApprovals(ctx context.Context, rt runtimev1.RuntimeClient, specialist AgentMeta) (map[string]struct{}, error) {
	resp, err := rt.ListApprovals(ctx, &runtimev1.ListApprovalsRequest{
		Status:         "pending",
		AgentNamespace: specialist.Namespace,
		AgentName:      specialist.Name,
	})
	if err != nil {
		return nil, err
	}
	seen := make(map[string]struct{}, len(resp.GetApprovals()))
	for _, a := range resp.GetApprovals() {
		if a.GetStatus() == "pending" {
			seen[a.GetId()] = struct{}{}
		}
	}
	return seen, nil
}

// PendingApprovalForDelegatedSession returns a new pending approval on a delegated
// specialist session. Stale pending rows from earlier runs are skipped when
// excludeBefore is non-nil (snapshot from SnapshotPendingApprovals before the run).
func PendingApprovalForDelegatedSession(
	ctx context.Context,
	rt runtimev1.RuntimeClient,
	parentSessionID string,
	specialist AgentMeta,
	excludeBefore map[string]struct{},
) (string, error) {
	resp, err := rt.ListApprovals(ctx, &runtimev1.ListApprovalsRequest{
		Status:         "pending",
		AgentNamespace: specialist.Namespace,
		AgentName:      specialist.Name,
	})
	if err != nil {
		return "", err
	}
	for _, a := range resp.GetApprovals() {
		if a.GetStatus() != "pending" {
			continue
		}
		if a.GetSessionId() == parentSessionID {
			continue
		}
		if excludeBefore != nil {
			if _, stale := excludeBefore[a.GetId()]; stale {
				continue
			}
		}
		return a.GetId(), nil
	}
	return "", fmt.Errorf("no pending delegated approval for parent session %s", parentSessionID)
}

// WaitForDelegatedPendingApproval polls until a delegated specialist session has a
// pending approval. The parent orchestrator stays in awaiting_tool until the
// child approval completes and delegation returns.
func WaitForDelegatedPendingApproval(
	t *testing.T,
	rt runtimev1.RuntimeClient,
	parentSessionID string,
	specialist AgentMeta,
	excludeBefore map[string]struct{},
	timeout time.Duration,
) string {
	t.Helper()
	Action(t, "poll gRPC approvals for delegated %s until pending (parent %s, timeout %s)", specialist.AgentRef(), parentSessionID, timeout)
	ctx, cancel := pollContext(t, timeout)
	defer cancel()
	start := time.Now()
	var lastParentStatus string
	for {
		id, err := PendingApprovalForDelegatedSession(ctx, rt, parentSessionID, specialist, excludeBefore)
		if err == nil && id != "" {
			Result(t, "pending delegated approval %s for specialist %s", id, specialist.AgentRef())
			return id
		}
		if status, serr := SessionStatus(ctx, rt, parentSessionID); serr == nil {
			if status != lastParentStatus {
				Note(t, "parent session %s status=%q (waiting for specialist pending approval)", parentSessionID, status)
				lastParentStatus = status
			}
			if status == "awaiting_tool" && time.Since(start) > dispatchQueueWaitSlack {
				t.Fatalf("parent session %s stuck in awaiting_tool without delegated approval — rebuild runtime (RUNTIME_DISPATCH_QUEUE_WAIT) or check specialist agent/policies", parentSessionID)
			}
			if isSessionTerminal(status) {
				t.Fatalf("parent session %s reached %q before delegated specialist approval appeared", parentSessionID, status)
			}
		}
		if err := pollSleep(ctx); err != nil {
			failOnPollInterrupt(t, err)
			t.Fatalf("no pending approval for delegated %s within %s (parent status=%q)", specialist.AgentRef(), timeout, lastParentStatus)
		}
	}
}

// ApprovalSessionID returns the session id for an approval.
func ApprovalSessionID(ctx context.Context, rt runtimev1.RuntimeClient, approvalID string) (string, error) {
	resp, err := rt.GetApproval(ctx, &runtimev1.GetApprovalRequest{ApprovalId: approvalID})
	if err != nil {
		return "", err
	}
	sessionID := strings.TrimSpace(resp.GetSessionId())
	if sessionID == "" {
		return "", fmt.Errorf("approval %s has no session_id", approvalID)
	}
	return sessionID, nil
}

// PendingApprovalForSession returns the first pending approval id for a session.
func PendingApprovalForSession(ctx context.Context, rt runtimev1.RuntimeClient, sessionID string) (string, error) {
	resp, err := rt.ListApprovals(ctx, &runtimev1.ListApprovalsRequest{
		Status:    "pending",
		SessionId: sessionID,
	})
	if err != nil {
		return "", err
	}
	for _, a := range resp.GetApprovals() {
		if a.GetSessionId() == sessionID && a.GetStatus() == "pending" {
			return a.GetId(), nil
		}
	}
	return "", fmt.Errorf("no pending approval for session %s", sessionID)
}

// WaitForPendingApproval polls until a pending approval exists for the session.
func WaitForPendingApproval(t *testing.T, rt runtimev1.RuntimeClient, sessionID string, timeout time.Duration) string {
	t.Helper()
	Action(t, "poll gRPC approvals for session %s until pending (timeout %s)", sessionID, timeout)
	ctx, cancel := pollContext(t, timeout)
	defer cancel()
	var lastStatus string
	for {
		id, err := PendingApprovalForSession(ctx, rt, sessionID)
		if err == nil && id != "" {
			Result(t, "pending approval %s for session %s", id, sessionID)
			return id
		}
		if status, serr := SessionStatus(ctx, rt, sessionID); serr == nil {
			if status != lastStatus {
				Note(t, "session %s status=%q (waiting for pending approval)", sessionID, status)
				lastStatus = status
			}
			if isSessionTerminal(status) {
				t.Fatalf("session %s reached %q before a pending approval appeared", sessionID, status)
			}
		}
		if err := pollSleep(ctx); err != nil {
			failOnPollInterrupt(t, err)
			t.Fatalf("no pending approval for session %s within %s (last status=%q)", sessionID, timeout, lastStatus)
		}
	}
}
