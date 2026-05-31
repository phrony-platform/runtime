package core

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
	"google.golang.org/grpc/codes"
)

func TestRuntime_RetireAgentVersion_success(t *testing.T) {
	db, mock := testSQLxDB(t)
	mock.ExpectQuery(`FROM agents`).
		WithArgs("demo", "echo").
		WillReturnRows(sqlmock.NewRows([]string{"id", "namespace", "name", "archived_at"}).
			AddRow("agent-1", "demo", "echo", nil))
	mock.ExpectQuery(`UPDATE agent_versions`).
		WithArgs("agent-1", "1.0.0").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("ver-1"))

	srv := &runtimeServer{db: db}
	resp, err := srv.RetireAgentVersion(context.Background(), &runtimev1.RetireAgentVersionRequest{
		AgentRef: &runtimev1.AgentRef{Namespace: "demo", Name: "echo", Version: "1.0.0"},
	})
	if err != nil {
		t.Fatalf("RetireAgentVersion: %v", err)
	}
	if resp.GetVersionId() != "ver-1" {
		t.Fatalf("version_id = %q, want ver-1", resp.GetVersionId())
	}
}

func TestRuntime_RetireAgentVersion_archivedAgent(t *testing.T) {
	db, mock := testSQLxDB(t)
	mock.ExpectQuery(`FROM agents`).
		WithArgs("demo", "echo").
		WillReturnRows(sqlmock.NewRows([]string{"id", "namespace", "name", "archived_at"}).
			AddRow("agent-1", "demo", "echo", time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)))

	srv := &runtimeServer{db: db}
	_, err := srv.RetireAgentVersion(context.Background(), &runtimev1.RetireAgentVersionRequest{
		AgentRef: &runtimev1.AgentRef{Namespace: "demo", Name: "echo", Version: "1.0.0"},
	})
	assertGRPCCode(t, err, codes.FailedPrecondition)
	if !strings.Contains(statusMessage(t, err), "archived") {
		t.Fatalf("error = %v, want archived", err)
	}
}

func TestRuntime_RetireAgentVersion_notFound(t *testing.T) {
	db, mock := testSQLxDB(t)
	mock.ExpectQuery(`FROM agents`).
		WithArgs("demo", "echo").
		WillReturnRows(sqlmock.NewRows([]string{"id", "namespace", "name", "archived_at"}).
			AddRow("agent-1", "demo", "echo", nil))
	mock.ExpectQuery(`UPDATE agent_versions`).
		WithArgs("agent-1", "9.9.9").
		WillReturnError(sql.ErrNoRows)

	srv := &runtimeServer{db: db}
	_, err := srv.RetireAgentVersion(context.Background(), &runtimev1.RetireAgentVersionRequest{
		AgentRef: &runtimev1.AgentRef{Namespace: "demo", Name: "echo", Version: "9.9.9"},
	})
	assertGRPCCode(t, err, codes.NotFound)
}
