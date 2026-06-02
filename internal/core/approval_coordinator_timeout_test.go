package core

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/phrony-platform/runtime/internal/model"
	"github.com/phrony-platform/runtime/internal/policy"
)

func TestApprovalRuntimeHelpers(t *testing.T) {
	runtime := json.RawMessage(`{
		"phrony.com/timeout_default": "escalate",
		"phrony.com/timeout_after_minutes": 10,
		"phrony.com/escalate_after_minutes": 5,
		"decision.runtime.phrony.com/escalate_to_role": "senior-ops",
		"phrony.com/escalation_depth": 2
	}`)
	if got := approvalTimeoutDefault(runtime); got != "escalate" {
		t.Fatalf("timeout_default = %q", got)
	}
	if got := approvalEscalateAfterMinutes(runtime, 0); got != 5 {
		t.Fatalf("escalate_after = %d, want 5", got)
	}
	if got := approvalEscalateRoute(runtime, "ops"); got != "senior-ops" {
		t.Fatalf("escalate_route = %q", got)
	}
	if got := approvalEscalationDepth(runtime); got != 2 {
		t.Fatalf("depth = %d", got)
	}
}

func TestHandleApprovalTimeout_denyDefault(t *testing.T) {
	coord, mock := testApprovalCoordinator(t)
	mock.MatchExpectationsInOrder(false)
	runtime, _ := json.Marshal(map[string]any{
		approvalRuntimeTimeoutDefault: "deny",
	})
	expectDecideGetApproval(mock, "appr-1", approvalRowFrom(approvalRowOpts{
		ID: "appr-1", SessionID: "sess-1", CallID: "",
		Status: model.ApprovalStatusPending, PolicyRuntime: runtime,
	}))
	expectDecideVote(mock)
	expectDecideFinalize(mock, "appr-1", model.ApprovalStatusDenied, "system:timeout", 0)

	coord.handleApprovalTimeout("appr-1")
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql: %v", err)
	}
}

func TestHandleApprovalTimeout_allow(t *testing.T) {
	coord, mock := testApprovalCoordinator(t)
	mock.MatchExpectationsInOrder(false)
	runtime, _ := json.Marshal(map[string]any{
		approvalRuntimeTimeoutDefault: "allow",
	})
	expectDecideGetApproval(mock, "appr-1", approvalRowFrom(approvalRowOpts{
		ID: "appr-1", SessionID: "sess-1", CallID: "",
		Status: model.ApprovalStatusPending, PolicyRuntime: runtime,
	}))
	expectDecideVote(mock)
	expectDecideIncrement(mock, "appr-1", 1, 1)
	expectDecideFinalize(mock, "appr-1", model.ApprovalStatusApproved, "system:timeout", 1)

	coord.handleApprovalTimeout("appr-1")
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql: %v", err)
	}
}

func TestHandleApprovalTimeout_escalate(t *testing.T) {
	coord, mock := testApprovalCoordinator(t)
	runtime, _ := json.Marshal(map[string]any{
		approvalRuntimeTimeoutDefault: "escalate",
		approvalRuntimeEscalateToRole: "senior",
		approvalRuntimeEscalateAfter:  15,
	})
	expectDecideGetApproval(mock, "appr-1", approvalRowFrom(approvalRowOpts{
		ID: "appr-1", SessionID: "sess-1", CallID: "call-1",
		Status: model.ApprovalStatusPending, PolicyRuntime: runtime,
	}))
	mock.ExpectQuery(`UPDATE approvals`).
		WithArgs("appr-1", model.ApprovalStatusEscalated, "system:timeout").
		WillReturnRows(sqlmock.NewRows([]string{"decided_at"}).AddRow(time.Now()))
	mock.ExpectQuery(`INSERT INTO approvals`).
		WillReturnRows(sqlmock.NewRows([]string{"created_at"}).AddRow(time.Now()))

	coord.handleApprovalTimeout("appr-1")
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql: %v", err)
	}
}

func TestHandleApprovalTimeout_skipsNonPending(t *testing.T) {
	coord, mock := testApprovalCoordinator(t)
	expectDecideGetApproval(mock, "appr-1", approvalRowFrom(approvalRowOpts{
		ID: "appr-1", SessionID: "sess-1", CallID: "call-1",
		Status: model.ApprovalStatusApproved,
	}))

	coord.handleApprovalTimeout("appr-1")
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql: %v", err)
	}
}

func TestMergeApprovalRuntime_timeoutDefault(t *testing.T) {
	raw := mergeApprovalRuntime(policy.ApprovalRequest{
		TimeoutDefault:      "deny",
		TimeoutAfterMinutes: 5,
	})
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if m[approvalRuntimeTimeoutDefault] != "deny" {
		t.Fatalf("timeout_default = %v", m[approvalRuntimeTimeoutDefault])
	}
}

func TestArmApprovalTimeout_fires(t *testing.T) {
	coord, mock := testApprovalCoordinator(t)
	mock.MatchExpectationsInOrder(false)
	runtime, _ := json.Marshal(map[string]any{approvalRuntimeTimeoutDefault: "deny"})
	expectDecideGetApproval(mock, "appr-timer", approvalRowFrom(approvalRowOpts{
		ID: "appr-timer", SessionID: "sess-1", CallID: "",
		Status: model.ApprovalStatusPending, PolicyRuntime: runtime,
	}))
	expectDecideVote(mock)
	expectDecideFinalize(mock, "appr-timer", model.ApprovalStatusDenied, "system:timeout", 0)

	coord.armApprovalTimeout("appr-timer", time.Now().Add(-time.Millisecond))
	time.Sleep(50 * time.Millisecond)
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql: %v", err)
	}
}
