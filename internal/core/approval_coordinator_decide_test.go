package core

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/phrony-platform/runtime/internal/model"
	"github.com/phrony-platform/runtime/internal/policy"
)

func TestApprovalCoordinator_comprehensionRequired(t *testing.T) {
	coord, mock := testApprovalCoordinator(t)

	expectDecideGetApproval(mock, "appr-1", approvalRow("appr-1", "sess-1", "call-1", model.ApprovalStatusPending, true, 1, 0, nil))

	_, err := coord.Decide(context.Background(), approvalDecideParams{
		ApprovalID: "appr-1",
		Approved:   true,
		DecidedBy:  "alice",
	})
	if err == nil || err.Error() != "comprehension acknowledgement is required" {
		t.Fatalf("Decide() err = %v, want comprehension error", err)
	}
}

func TestApprovalCoordinator_multiApproverPartial(t *testing.T) {
	coord, mock := testApprovalCoordinator(t)

	expectDecideGetApproval(mock, "appr-1", approvalRow("appr-1", "sess-1", "call-1", model.ApprovalStatusPending, false, 2, 0, nil))
	expectDecideVote(mock)
	expectDecideIncrement(mock, "appr-1", 1, 2)

	result, err := coord.Decide(context.Background(), approvalDecideParams{
		ApprovalID: "appr-1",
		Approved:   true,
		DecidedBy:  "alice",
	})
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if result.Status != model.ApprovalStatusPending || result.ApprovalsReceived != 1 {
		t.Fatalf("result = %+v, want pending with 1 received", result)
	}
}

func TestApprovalCoordinator_approveFinalUnblocksGate(t *testing.T) {
	coord, mock := testApprovalCoordinator(t)
	gate := newSessionApprovalGate(coord, "sess-1", nil, nil, "av-1")
	coord.registerGate("sess-1", gate)

	req := policy.ApprovalRequest{ApprovalID: "appr-1", SessionID: "sess-1", Args: json.RawMessage(`{}`)}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	resultCh := make(chan policy.ApprovalResult, 1)
	errCh := make(chan error, 1)
	go func() {
		r, err := gate.WaitApproval(ctx, req)
		if err != nil {
			errCh <- err
			return
		}
		resultCh <- r
	}()

	waitForPendingApproval(t, gate)

	expectDecideGetApproval(mock, "appr-1", approvalRow("appr-1", "sess-1", "", model.ApprovalStatusPending, false, 1, 0, nil))
	expectDecideVote(mock)
	expectDecideIncrement(mock, "appr-1", 1, 1)
	expectDecideFinalize(mock, "appr-1", model.ApprovalStatusApproved, "alice", 1)

	got, err := coord.Decide(context.Background(), approvalDecideParams{
		ApprovalID: "appr-1",
		Approved:   true,
		DecidedBy:  "alice",
	})
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if got.Status != model.ApprovalStatusApproved || !got.Terminal {
		t.Fatalf("result = %+v", got)
	}

	select {
	case r := <-resultCh:
		if !r.Approved {
			t.Fatal("gate was not approved")
		}
	case err := <-errCh:
		t.Fatalf("WaitApproval: %v", err)
	case <-ctx.Done():
		t.Fatal("timed out waiting for gate")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql: %v", err)
	}
}

func TestApprovalCoordinator_rejectUnblocksGate(t *testing.T) {
	coord, mock := testApprovalCoordinator(t)
	gate := newSessionApprovalGate(coord, "sess-1", nil, nil, "av-1")
	coord.registerGate("sess-1", gate)

	req := policy.ApprovalRequest{ApprovalID: "appr-1", SessionID: "sess-1", CallID: "call-1", Args: json.RawMessage(`{}`)}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	resultCh := make(chan policy.ApprovalResult, 1)
	errCh := make(chan error, 1)
	go func() {
		r, err := gate.WaitApproval(ctx, req)
		if err != nil {
			errCh <- err
			return
		}
		resultCh <- r
	}()

	waitForPendingApproval(t, gate)

	expectDecideGetApproval(mock, "appr-1", approvalRow("appr-1", "sess-1", "call-1", model.ApprovalStatusPending, false, 1, 0, nil))
	expectDecideVote(mock)
	expectDecideFinalize(mock, "appr-1", model.ApprovalStatusDenied, "alice", 0)
	expectUpdateToolInvocationStatus(mock, "call-1", model.ToolInvocationFailed)

	_, err := coord.Decide(context.Background(), approvalDecideParams{
		ApprovalID: "appr-1",
		Approved:   false,
		DecidedBy:  "alice",
		Comment:    "no",
	})
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}

	select {
	case r := <-resultCh:
		if r.Approved {
			t.Fatal("expected denial")
		}
	case err := <-errCh:
		t.Fatalf("WaitApproval: %v", err)
	case <-ctx.Done():
		t.Fatal("timed out")
	}
}

func TestApprovalCoordinator_rejectOnFailDeliversError(t *testing.T) {
	coord, mock := testApprovalCoordinator(t)
	gate := newSessionApprovalGate(coord, "sess-1", nil, nil, "av-1")
	coord.registerGate("sess-1", gate)

	req := policy.ApprovalRequest{ApprovalID: "appr-1", SessionID: "sess-1", Args: json.RawMessage(`{}`)}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		_, err := gate.WaitApproval(ctx, req)
		errCh <- err
	}()

	waitForPendingApproval(t, gate)

	expectDecideGetApproval(mock, "appr-1", approvalRowFrom(approvalRowOpts{
		ID: "appr-1", SessionID: "sess-1", CallID: "call-1",
		Status: model.ApprovalStatusPending, OnReject: "fail",
	}))
	expectDecideVote(mock)
	expectDecideFinalize(mock, "appr-1", model.ApprovalStatusDenied, "alice", 0)
	expectUpdateToolInvocationStatus(mock, "call-1", model.ToolInvocationFailed)

	_, err := coord.Decide(context.Background(), approvalDecideParams{
		ApprovalID: "appr-1",
		Approved:   false,
		DecidedBy:  "alice",
		Comment:    "policy violation",
	})
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}

	select {
	case err := <-errCh:
		if err == nil || err.Error() != "policy violation" {
			t.Fatalf("WaitApproval err = %v, want policy violation", err)
		}
	case <-ctx.Done():
		t.Fatal("timed out")
	}
}

func TestApprovalCoordinator_duplicateVote(t *testing.T) {
	coord, mock := testApprovalCoordinator(t)

	expectDecideGetApproval(mock, "appr-1", approvalRow("appr-1", "sess-1", "call-1", model.ApprovalStatusPending, false, 1, 0, nil))
	expectDecideVoteDuplicate(mock)

	_, err := coord.Decide(context.Background(), approvalDecideParams{
		ApprovalID: "appr-1",
		Approved:   true,
		DecidedBy:  "alice",
	})
	if err == nil {
		t.Fatal("Decide() = nil, want duplicate vote error")
	}
	want := `actor "alice" already decided on this approval`
	if err.Error() != want {
		t.Fatalf("Decide() err = %q, want %q", err.Error(), want)
	}
}

func TestApprovalCoordinator_alreadyDecidedIdempotent(t *testing.T) {
	coord, mock := testApprovalCoordinator(t)

	expectDecideGetApproval(mock, "appr-1", approvalRow("appr-1", "sess-1", "call-1", model.ApprovalStatusApproved, false, 1, 1, nil))

	got, err := coord.Decide(context.Background(), approvalDecideParams{
		ApprovalID: "appr-1",
		Approved:   true,
		DecidedBy:  "alice",
	})
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if got.Status != model.ApprovalStatusApproved || got.Terminal {
		t.Fatalf("result = %+v, want idempotent status without terminal", got)
	}
}

func waitForPendingApproval(t *testing.T, gate *sessionApprovalGate) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if gate.pendingApproval() != nil {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("pending approval was not registered")
}
