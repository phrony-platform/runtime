package core

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/jmoiron/sqlx"
	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
	"github.com/phrony-platform/runtime/internal/model"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestRuntime_GetApproval_validation(t *testing.T) {
	srv := &runtimeServer{db: mustTestDB(t)}
	_, err := srv.GetApproval(context.Background(), &runtimev1.GetApprovalRequest{})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("code = %v, want InvalidArgument", status.Code(err))
	}
}

func TestRuntime_GetApproval_notFound(t *testing.T) {
	db, mock := testSQLxDB(t)
	srv := &runtimeServer{db: db}
	mock.ExpectQuery(`FROM approvals`).WithArgs("missing").WillReturnError(sql.ErrNoRows)

	_, err := srv.GetApproval(context.Background(), &runtimev1.GetApprovalRequest{ApprovalId: "missing"})
	if status.Code(err) != codes.NotFound {
		t.Fatalf("code = %v, err = %v", status.Code(err), err)
	}
}

func TestRuntime_GetApproval_success(t *testing.T) {
	db, mock := testSQLxDB(t)
	srv := &runtimeServer{db: db}
	now := time.Now()
	mock.ExpectQuery(`FROM approvals`).WithArgs("appr-1").
		WillReturnRows(approvalRow("appr-1", "sess-1", "call-1", model.ApprovalStatusPending, false, 1, 0, nil))
	mock.ExpectQuery(`FROM approval_votes`).WithArgs("appr-1").
		WillReturnRows(sqlmock.NewRows([]string{
			"decided_by", "decision", "comment", "comprehension_acknowledged", "created_at",
		}).AddRow("alice", model.ApprovalVoteApproved, "ok", true, now))

	got, err := srv.GetApproval(context.Background(), &runtimev1.GetApprovalRequest{ApprovalId: "appr-1"})
	if err != nil {
		t.Fatalf("GetApproval: %v", err)
	}
	if got.GetId() != "appr-1" || got.GetTool() != "tool.ref" {
		t.Fatalf("approval = %+v", got)
	}
	if len(got.GetVotes()) != 1 || got.GetVotes()[0].GetDecidedBy() != "alice" {
		t.Fatalf("votes = %+v", got.GetVotes())
	}
}

func TestRuntime_GetApproval_enrichesFromInvocation(t *testing.T) {
	db, mock := testSQLxDB(t)
	srv := &runtimeServer{db: db}
	mock.ExpectQuery(`FROM approvals`).WithArgs("appr-1").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "session_id", "call_id", "status", "route", "reason", "decided_by", "comment",
			"created_at", "decided_at",
			"tool", "version", "args", "authority_ref", "policy_name",
			"approvals_required", "approvals_received", "comprehension_required",
			"on_reject", "on_modify", "expires_at", "policy_runtime",
		}).AddRow(
			"appr-1", "sess-1", "call-1", model.ApprovalStatusPending, "ops", "reason", "", "",
			time.Now(), nil,
			"", "", nil, "", "policy",
			1, 0, false,
			"", "", nil, nil,
		))
	mock.ExpectQuery(`FROM tool_invocations`).WithArgs("call-1").
		WillReturnRows(sqlmock.NewRows([]string{
			"call_id", "session_id", "agent_version_id", "turn", "tool", "version", "args",
			"result", "status", "worker_identity", "image_digest", "descriptor_hash",
			"manifest_content_hash", "attempt", "error_code", "error_message",
			"usage_input_tokens", "usage_output_tokens", "usage_estimated",
			"created_at", "updated_at", "dispatched_at", "completed_at",
		}).AddRow(
			"call-1", "sess-1", "av-1", 1, "payments.charge", "2.0.0", []byte(`{"amount":100}`),
			nil, model.ToolInvocationAwaitingApproval,
			"", "", "", "", 1, nil, nil,
			0, 0, false,
			time.Now(), time.Now(), nil, nil,
		))
	mock.ExpectQuery(`FROM approval_votes`).WithArgs("appr-1").
		WillReturnRows(sqlmock.NewRows([]string{
			"decided_by", "decision", "comment", "comprehension_acknowledged", "created_at",
		}))

	got, err := srv.GetApproval(context.Background(), &runtimev1.GetApprovalRequest{ApprovalId: "appr-1"})
	if err != nil {
		t.Fatalf("GetApproval: %v", err)
	}
	if got.GetTool() != "payments.charge" {
		t.Fatalf("tool = %q", got.GetTool())
	}
	if string(got.GetArgs()) != `{"amount":100}` {
		t.Fatalf("args = %s", got.GetArgs())
	}
}

func TestRuntime_ListApprovals_success(t *testing.T) {
	db, mock := testSQLxDB(t)
	srv := &runtimeServer{db: db}
	mock.ExpectQuery(`FROM approvals a`).
		WithArgs("pending", "", "", "", "").
		WillReturnRows(approvalRow("appr-1", "sess-1", "call-1", model.ApprovalStatusPending, false, 1, 0, nil))

	resp, err := srv.ListApprovals(context.Background(), &runtimev1.ListApprovalsRequest{Status: "pending"})
	if err != nil {
		t.Fatalf("ListApprovals: %v", err)
	}
	if len(resp.GetApprovals()) != 1 {
		t.Fatalf("len = %d", len(resp.GetApprovals()))
	}
}

func TestRuntime_DecideApproval_validation(t *testing.T) {
	srv := &runtimeServer{db: mustTestDB(t)}
	_, err := srv.DecideApproval(context.Background(), &runtimev1.DecideApprovalRequest{})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("empty id code = %v", status.Code(err))
	}
	_, err = srv.DecideApproval(context.Background(), &runtimev1.DecideApprovalRequest{
		ApprovalId: "appr-1",
		Decision:   runtimev1.ApprovalDecision_APPROVAL_DECISION_UNSPECIFIED,
	})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("decision code = %v", status.Code(err))
	}
}

func TestRuntime_DecideApproval_comprehensionRequired(t *testing.T) {
	db, mock := testSQLxDB(t)
	srv := &runtimeServer{db: db}
	expectDecideGetApproval(mock, "appr-1", approvalRow("appr-1", "sess-1", "call-1", model.ApprovalStatusPending, true, 1, 0, nil))

	_, err := srv.DecideApproval(context.Background(), &runtimev1.DecideApprovalRequest{
		ApprovalId: "appr-1",
		Decision:   runtimev1.ApprovalDecision_APPROVAL_DECISION_APPROVE,
	})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("code = %v, err = %v", status.Code(err), err)
	}
}

func TestRuntime_DecideApproval_approveSuccess(t *testing.T) {
	db, mock := testSQLxDB(t)
	srv := &runtimeServer{db: db}
	expectDecideGetApproval(mock, "appr-1", approvalRow("appr-1", "sess-1", "call-1", model.ApprovalStatusPending, false, 1, 0, nil))
	expectDecideVote(mock)
	expectDecideIncrement(mock, "appr-1", 1, 1)
	expectDecideFinalize(mock, "appr-1", model.ApprovalStatusApproved, "bob", 1)
	expectUpdateToolInvocationStatus(mock, "call-1", model.ToolInvocationPending)
	expectDecideGetApproval(mock, "appr-1", approvalRow("appr-1", "sess-1", "call-1", model.ApprovalStatusApproved, false, 1, 1, nil))
	now := time.Now()
	mock.ExpectQuery(`FROM sessions`).WithArgs("sess-1").
		WillReturnRows(sessionMockRows("sess-1", "av-1", model.SessionStatusAwaitingApproval, []byte(`{}`), nil, "sess-1", 0, now, now))

	resp, err := srv.DecideApproval(context.Background(), &runtimev1.DecideApprovalRequest{
		ApprovalId: "appr-1",
		Decision:   runtimev1.ApprovalDecision_APPROVAL_DECISION_APPROVE,
		Actor:      "bob",
	})
	if err != nil {
		t.Fatalf("DecideApproval: %v", err)
	}
	if resp.GetStatus() != model.ApprovalStatusApproved {
		t.Fatalf("status = %q", resp.GetStatus())
	}
}

func mustTestDB(t *testing.T) *sqlx.DB {
	t.Helper()
	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return sqlx.NewDb(db, "pgx")
}
