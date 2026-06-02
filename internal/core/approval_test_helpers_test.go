package core

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/jmoiron/sqlx"
	"github.com/phrony-platform/runtime/internal/model"
)

type approvalRowOpts struct {
	ID, SessionID, CallID, Status string
	Comprehension                 bool
	Required, Received            int
	OnReject, OnModify            string
	PolicyRuntime                 json.RawMessage
	ExpiresAt                     *time.Time
}

func approvalRowFrom(opts approvalRowOpts) *sqlmock.Rows {
	if opts.Status == "" {
		opts.Status = model.ApprovalStatusPending
	}
	if opts.Required <= 0 {
		opts.Required = 1
	}
	var expires any
	if opts.ExpiresAt != nil {
		expires = *opts.ExpiresAt
	}
	var runtime any
	if len(opts.PolicyRuntime) > 0 {
		runtime = opts.PolicyRuntime
	}
	return sqlmock.NewRows([]string{
		"id", "session_id", "call_id", "status", "route", "reason", "decided_by", "comment",
		"created_at", "decided_at",
		"tool", "version", "args", "authority_ref", "policy_name",
		"approvals_required", "approvals_received", "comprehension_required",
		"on_reject", "on_modify", "expires_at", "policy_runtime",
	}).AddRow(
		opts.ID, opts.SessionID, opts.CallID, opts.Status, "ops", "reason", "", "",
		time.Now(), nil,
		"tool.ref", "1.0.0", []byte(`{}`), "", "policy",
		opts.Required, opts.Received, opts.Comprehension,
		opts.OnReject, opts.OnModify, expires, runtime,
	)
}

func approvalRow(id, sessionID, callID, status string, comprehension bool, required, received int, _ *time.Time) *sqlmock.Rows {
	return approvalRowFrom(approvalRowOpts{
		ID: id, SessionID: sessionID, CallID: callID, Status: status,
		Comprehension: comprehension, Required: required, Received: received,
	})
}

func testApprovalCoordinator(t *testing.T) (*approvalCoordinator, sqlmock.Sqlmock) {
	t.Helper()
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	srv := &runtimeServer{db: sqlx.NewDb(sqlDB, "pgx")}
	return newApprovalCoordinator(srv), mock
}

func expectDecideGetApproval(mock sqlmock.Sqlmock, id string, row *sqlmock.Rows) {
	mock.ExpectQuery(`FROM approvals`).WithArgs(id).WillReturnRows(row)
}

// expectDecideFlowGetApproval accounts for GetApproval in handleApprovalTimeout and decideLocked.
func expectDecideFlowGetApproval(mock sqlmock.Sqlmock, id string, row *sqlmock.Rows) {
	expectDecideGetApproval(mock, id, row)
	expectDecideGetApproval(mock, id, row)
}

func expectDecideVote(mock sqlmock.Sqlmock) {
	mock.ExpectQuery(`INSERT INTO approval_votes`).
		WillReturnRows(sqlmock.NewRows([]string{"created_at"}).AddRow(time.Now()))
}

func expectDecideIncrement(mock sqlmock.Sqlmock, id string, received, required int) {
	mock.ExpectQuery(`UPDATE approvals`).
		WithArgs(id).
		WillReturnRows(sqlmock.NewRows([]string{"approvals_received", "approvals_required"}).AddRow(received, required))
}

func expectDecideFinalize(mock sqlmock.Sqlmock, id, status, decidedBy string, received int) {
	mock.ExpectQuery(`UPDATE approvals`).
		WithArgs(id, status, decidedBy, sqlmock.AnyArg(), received).
		WillReturnRows(sqlmock.NewRows([]string{"decided_at"}).AddRow(time.Now()))
}

func expectUpdateToolInvocationStatus(mock sqlmock.Sqlmock, callID, status string) {
	mock.ExpectExec(`UPDATE tool_invocations`).
		WithArgs(callID, status).
		WillReturnResult(sqlmock.NewResult(0, 1))
}
