package core

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
	"github.com/phrony-platform/runtime/internal/model"
	"google.golang.org/grpc/codes"
)

func TestRuntime_ListSessions_success(t *testing.T) {
	db, mock := testSQLxDB(t)
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	mock.ExpectQuery(`FROM agent_versions av`).
		WithArgs("demo", "echo-agent").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("version-uuid"))
	mock.ExpectQuery(`FROM sessions`).
		WithArgs("version-uuid", "").
		WillReturnRows(sqlmock.NewRows([]string{"id", "agent_version_id", "status", "created_at", "updated_at"}).
			AddRow("sess-1", "version-uuid", model.SessionStatusAwaitingInput, now, now))

	srv := &runtimeServer{db: db}
	resp, err := srv.ListSessions(context.Background(), &runtimev1.ListSessionsRequest{
		AgentRef: &runtimev1.AgentRef{Namespace: "demo", Name: "echo-agent"},
	})
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(resp.GetSessions()) != 1 {
		t.Fatalf("sessions = %d, want 1", len(resp.GetSessions()))
	}
	summary := resp.GetSessions()[0]
	if summary.GetId() != "sess-1" {
		t.Fatalf("id = %q, want sess-1", summary.GetId())
	}
	if summary.GetStatus() != model.SessionStatusAwaitingInput {
		t.Fatalf("status = %q", summary.GetStatus())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestRuntime_ListSessions_agentNotFound(t *testing.T) {
	db, mock := testSQLxDB(t)
	mock.ExpectQuery(`FROM agent_versions av`).
		WithArgs("demo", "missing").
		WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery(`FROM agents`).
		WithArgs("demo", "missing").
		WillReturnError(sql.ErrNoRows)

	srv := &runtimeServer{db: db}
	_, err := srv.ListSessions(context.Background(), &runtimev1.ListSessionsRequest{
		AgentRef: &runtimev1.AgentRef{Namespace: "demo", Name: "missing"},
	})
	assertGRPCCode(t, err, codes.NotFound)
}

func TestRuntime_ListSessions_noDatabase(t *testing.T) {
	srv := &runtimeServer{}
	_, err := srv.ListSessions(context.Background(), &runtimev1.ListSessionsRequest{
		AgentRef: &runtimev1.AgentRef{Namespace: "demo", Name: "echo"},
	})
	assertGRPCCode(t, err, codes.FailedPrecondition)
}
