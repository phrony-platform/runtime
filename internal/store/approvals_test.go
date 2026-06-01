package store

import (
	"context"
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
		WithArgs("appr-1", "sess-1", "call-1", model.ApprovalStatusPending, "supervisor", "severity").
		WillReturnRows(sqlmock.NewRows([]string{"created_at"}).AddRow(created))
	mock.ExpectQuery(`UPDATE approvals`).
		WithArgs("appr-1", model.ApprovalStatusApproved, "alice", "ok").
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
		ID:        "appr-1",
		Status:    model.ApprovalStatusApproved,
		DecidedBy: "alice",
		Comment:   "ok",
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
