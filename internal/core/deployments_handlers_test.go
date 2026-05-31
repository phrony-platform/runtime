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

func TestRuntime_GetActiveVersion_success(t *testing.T) {
	db, mock := testSQLxDB(t)
	deployed := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	mock.ExpectQuery(`FROM deployments d`).
		WithArgs("demo", "echo").
		WillReturnRows(sqlmock.NewRows([]string{"version", "created_at", "actor"}).
			AddRow("2.0.0", deployed, "alice"))

	srv := &runtimeServer{db: db}
	resp, err := srv.GetActiveVersion(context.Background(), &runtimev1.GetActiveVersionRequest{
		AgentRef: &runtimev1.AgentRef{Namespace: "demo", Name: "echo"},
	})
	if err != nil {
		t.Fatalf("GetActiveVersion: %v", err)
	}
	if resp.GetVersion() != "2.0.0" || resp.GetActor() != "alice" {
		t.Fatalf("resp = %+v", resp)
	}
	if resp.GetDeployedAt() == "" {
		t.Fatal("deployed_at is empty")
	}
}

func TestRuntime_GetActiveVersion_noDeployment(t *testing.T) {
	db, mock := testSQLxDB(t)
	mock.ExpectQuery(`FROM deployments d`).
		WithArgs("demo", "echo").
		WillReturnError(sql.ErrNoRows)

	srv := &runtimeServer{db: db}
	_, err := srv.GetActiveVersion(context.Background(), &runtimev1.GetActiveVersionRequest{
		AgentRef: &runtimev1.AgentRef{Namespace: "demo", Name: "echo"},
	})
	assertGRPCCode(t, err, codes.FailedPrecondition)
	if !strings.Contains(statusMessage(t, err), "no active deployment") {
		t.Fatalf("error = %v, want no active deployment", err)
	}
}

func TestRuntime_ListDeployments_success(t *testing.T) {
	db, mock := testSQLxDB(t)
	agentID := "agent-1"
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	earlier := now.Add(-time.Hour)
	mock.ExpectQuery(`FROM agents`).
		WithArgs("demo", "echo").
		WillReturnRows(sqlmock.NewRows([]string{"id", "namespace", "name", "archived_at"}).
			AddRow(agentID, "demo", "echo", nil))
	mock.ExpectQuery(`FROM deployments d`).
		WithArgs(agentID).
		WillReturnRows(sqlmock.NewRows([]string{"version", "action", "actor", "created_at"}).
			AddRow("2.0.0", "deploy", "alice", now).
			AddRow("1.0.0", "rollback", "bob", earlier))

	srv := &runtimeServer{db: db}
	resp, err := srv.ListDeployments(context.Background(), &runtimev1.ListDeploymentsRequest{
		AgentRef: &runtimev1.AgentRef{Namespace: "demo", Name: "echo"},
	})
	if err != nil {
		t.Fatalf("ListDeployments: %v", err)
	}
	if len(resp.GetDeployments()) != 2 {
		t.Fatalf("deployments = %d, want 2", len(resp.GetDeployments()))
	}
	if resp.GetDeployments()[0].GetVersion() != "2.0.0" || resp.GetDeployments()[0].GetAction() != "deploy" {
		t.Fatalf("first = %+v", resp.GetDeployments()[0])
	}
}
