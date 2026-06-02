package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/phrony-platform/runtime/internal/model"
)

func TestQueries_InsertAndDecideApproval(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	created := time.Now()
	decided := created.Add(time.Minute)
	mock.ExpectQuery(`INSERT INTO approvals`).
		WithArgs(
			"appr-1", "sess-1", "call-1", model.ApprovalStatusPending, "supervisor", "severity",
			"", "", json.RawMessage("{}"), "", "",
			1, 0, false,
			"", "", nil, nil,
		).
		WillReturnRows(sqlmock.NewRows([]string{"created_at"}).AddRow(created))
	mock.ExpectQuery(`UPDATE approvals`).
		WithArgs("appr-1", model.ApprovalStatusApproved, "alice", "ok", 1).
		WillReturnRows(sqlmock.NewRows([]string{"decided_at"}).AddRow(decided))

	q := New(sqlDB)
	if _, err := q.InsertApproval(context.Background(), InsertApprovalParams{
		ID:        "appr-1",
		SessionID: "sess-1",
		CallID:    "call-1",
		Status:    model.ApprovalStatusPending,
		Route:     "supervisor",
		Reason:    "severity",
	}); err != nil {
		t.Fatalf("InsertApproval: %v", err)
	}
	got, err := q.DecideApproval(context.Background(), DecideApprovalParams{
		ID:                "appr-1",
		Status:            model.ApprovalStatusApproved,
		DecidedBy:         "alice",
		Comment:           "ok",
		ApprovalsReceived: 1,
	})
	if err != nil {
		t.Fatalf("DecideApproval: %v", err)
	}
	if !got.Equal(decided) {
		t.Fatalf("decided_at = %v, want %v", got, decided)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestQueries_DecideApproval_notPending(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	mock.ExpectQuery(`UPDATE approvals`).
		WithArgs("appr-1", model.ApprovalStatusApproved, "alice", "", 1).
		WillReturnError(sql.ErrNoRows)

	q := New(sqlDB)
	_, err = q.DecideApproval(context.Background(), DecideApprovalParams{
		ID:                "appr-1",
		Status:            model.ApprovalStatusApproved,
		DecidedBy:         "alice",
		ApprovalsReceived: 1,
	})
	if err != sql.ErrNoRows {
		t.Fatalf("err = %v, want ErrNoRows", err)
	}
}

func TestQueries_InsertApprovalVote_andDuplicate(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	now := time.Now()
	mock.ExpectQuery(`INSERT INTO approval_votes`).
		WithArgs("appr-1", "alice", model.ApprovalVoteApproved, "ok", true).
		WillReturnRows(sqlmock.NewRows([]string{"created_at"}).AddRow(now))
	mock.ExpectQuery(`INSERT INTO approval_votes`).
		WithArgs("appr-1", "alice", model.ApprovalVoteApproved, "ok", true).
		WillReturnError(sql.ErrNoRows)

	q := New(sqlDB)
	got, err := q.InsertApprovalVote(context.Background(), InsertApprovalVoteParams{
		ApprovalID:                "appr-1",
		DecidedBy:                 "alice",
		Decision:                  model.ApprovalVoteApproved,
		Comment:                   "ok",
		ComprehensionAcknowledged: true,
	})
	if err != nil || !got.Equal(now) {
		t.Fatalf("first vote: got=%v err=%v", got, err)
	}
	_, err = q.InsertApprovalVote(context.Background(), InsertApprovalVoteParams{
		ApprovalID:                "appr-1",
		DecidedBy:                 "alice",
		Decision:                  model.ApprovalVoteApproved,
		Comment:                   "ok",
		ComprehensionAcknowledged: true,
	})
	if err != ErrDuplicateApprovalVote {
		t.Fatalf("duplicate vote err = %v", err)
	}
}

func TestQueries_IncrementApprovalsReceived(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	mock.ExpectQuery(`UPDATE approvals`).
		WithArgs("appr-1").
		WillReturnRows(sqlmock.NewRows([]string{"approvals_received", "approvals_required"}).AddRow(2, 2))

	q := New(sqlDB)
	received, required, err := q.IncrementApprovalsReceived(context.Background(), "appr-1")
	if err != nil {
		t.Fatalf("IncrementApprovalsReceived: %v", err)
	}
	if received != 2 || required != 2 {
		t.Fatalf("received/required = %d/%d", received, required)
	}
}

func TestQueries_ListApprovals(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	now := time.Now()
	mock.ExpectQuery(`FROM approvals a`).
		WithArgs("pending", "ops", "sess-1", "demo", "agent").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "session_id", "call_id", "status", "route", "reason", "decided_by", "comment",
			"created_at", "decided_at",
			"tool", "version", "args", "authority_ref", "policy_name",
			"approvals_required", "approvals_received", "comprehension_required",
			"on_reject", "on_modify", "expires_at", "policy_runtime",
		}).AddRow(
			"appr-1", "sess-1", "call-1", model.ApprovalStatusPending, "ops", "reason", "", "",
			now, nil,
			"tool.ref", "1.0.0", []byte(`{}`), "", "policy",
			1, 0, false,
			"", "", nil, nil,
		))

	q := New(sqlDB)
	rows, err := q.ListApprovals(context.Background(), ListApprovalsParams{
		Status: "pending", Route: "ops", SessionID: "sess-1",
		AgentNamespace: "demo", AgentName: "agent",
	})
	if err != nil {
		t.Fatalf("ListApprovals: %v", err)
	}
	if len(rows) != 1 || rows[0].ID != "appr-1" {
		t.Fatalf("rows = %+v", rows)
	}
}

func TestQueries_GetApproval_notFound(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	mock.ExpectQuery(`FROM approvals`).WithArgs("missing").WillReturnError(sql.ErrNoRows)

	q := New(sqlDB)
	_, err = q.GetApproval(context.Background(), "missing")
	if err != sql.ErrNoRows {
		t.Fatalf("err = %v", err)
	}
}

func TestQueries_MarkApprovalEscalated(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	decided := time.Now()
	mock.ExpectQuery(`UPDATE approvals`).
		WithArgs("appr-1", model.ApprovalStatusEscalated, "system:timeout").
		WillReturnRows(sqlmock.NewRows([]string{"decided_at"}).AddRow(decided))

	q := New(sqlDB)
	got, err := q.MarkApprovalEscalated(context.Background(), "appr-1", "system:timeout")
	if err != nil {
		t.Fatalf("MarkApprovalEscalated: %v", err)
	}
	if !got.Equal(decided) {
		t.Fatalf("decided_at = %v", got)
	}
}

func TestQueries_GetPendingApprovalBySession(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	mock.ExpectQuery(`FROM approvals`).
		WithArgs("sess-1").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "session_id", "call_id", "status", "route", "reason", "decided_by", "comment",
			"created_at", "decided_at",
			"tool", "version", "args", "authority_ref", "policy_name",
			"approvals_required", "approvals_received", "comprehension_required",
			"on_reject", "on_modify", "expires_at", "policy_runtime",
		}).AddRow(
			"appr-1", "sess-1", "call-1", model.ApprovalStatusPending, "ops", "reason", "", "",
			time.Now(), nil,
			"tool.ref", "1.0.0", []byte(`{}`), "", "policy",
			1, 0, false,
			"", "", nil, nil,
		))

	q := New(sqlDB)
	row, err := q.GetPendingApprovalBySession(context.Background(), "sess-1")
	if err != nil {
		t.Fatalf("GetPendingApprovalBySession: %v", err)
	}
	if row.ID != "appr-1" {
		t.Fatalf("id = %q", row.ID)
	}
}
