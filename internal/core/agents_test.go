package core

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
	"google.golang.org/grpc/codes"
)

func TestRuntime_ListAgents_success(t *testing.T) {
	db, mock := testSQLxDB(t)
	mock.ExpectQuery(`FROM agents`).
		WithArgs("").
		WillReturnRows(sqlmock.NewRows([]string{"id", "namespace", "name", "owner", "archived_at", "created_at"}).
			AddRow("agent-1", "demo", "echo", "owner", nil, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)))

	srv := &runtimeServer{db: db}
	resp, err := srv.ListAgents(context.Background(), &runtimev1.ListAgentsRequest{})
	if err != nil {
		t.Fatalf("ListAgents: %v", err)
	}
	if len(resp.GetAgents()) != 1 {
		t.Fatalf("agents = %d, want 1", len(resp.GetAgents()))
	}
	if resp.GetAgents()[0].GetId() != "agent-1" {
		t.Fatalf("id = %q, want agent-1", resp.GetAgents()[0].GetId())
	}
}

func TestRuntime_ListAgentVersions_agentNotFound(t *testing.T) {
	db, mock := testSQLxDB(t)
	mock.ExpectQuery(`FROM agents`).
		WithArgs("demo", "missing").
		WillReturnError(sql.ErrNoRows)

	srv := &runtimeServer{db: db}
	_, err := srv.ListAgentVersions(context.Background(), &runtimev1.ListAgentVersionsRequest{
		AgentRef: &runtimev1.AgentRef{Namespace: "demo", Name: "missing"},
	})
	assertGRPCCode(t, err, codes.NotFound)
}

func TestRuntime_DeprecateAgentVersion_success(t *testing.T) {
	db, mock := testSQLxDB(t)
	mock.ExpectQuery(`FROM agents`).
		WithArgs("demo", "echo").
		WillReturnRows(sqlmock.NewRows([]string{"id", "namespace", "name", "archived_at"}).
			AddRow("agent-1", "demo", "echo", nil))
	mock.ExpectQuery(`UPDATE agent_versions`).
		WithArgs("agent-1", "1.0.0").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("ver-1"))

	srv := &runtimeServer{db: db}
	resp, err := srv.DeprecateAgentVersion(context.Background(), &runtimev1.DeprecateAgentVersionRequest{
		AgentRef: &runtimev1.AgentRef{Namespace: "demo", Name: "echo", Version: "1.0.0"},
	})
	if err != nil {
		t.Fatalf("DeprecateAgentVersion: %v", err)
	}
	if resp.GetVersionId() != "ver-1" {
		t.Fatalf("version_id = %q, want ver-1", resp.GetVersionId())
	}
}

func TestRuntime_DeprecateAgentVersion_archivedAgent(t *testing.T) {
	db, mock := testSQLxDB(t)
	mock.ExpectQuery(`FROM agents`).
		WithArgs("demo", "echo").
		WillReturnRows(sqlmock.NewRows([]string{"id", "namespace", "name", "archived_at"}).
			AddRow("agent-1", "demo", "echo", time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)))

	srv := &runtimeServer{db: db}
	_, err := srv.DeprecateAgentVersion(context.Background(), &runtimev1.DeprecateAgentVersionRequest{
		AgentRef: &runtimev1.AgentRef{Namespace: "demo", Name: "echo", Version: "1.0.0"},
	})
	assertGRPCCode(t, err, codes.FailedPrecondition)
	if !strings.Contains(statusMessage(t, err), "archived") {
		t.Fatalf("error = %v, want archived", err)
	}
}

func TestRuntime_ArchiveAgent_success(t *testing.T) {
	db, mock := testSQLxDB(t)
	mock.ExpectQuery(`FROM agents`).
		WithArgs("demo", "echo").
		WillReturnRows(sqlmock.NewRows([]string{"id", "namespace", "name", "archived_at"}).
			AddRow("agent-1", "demo", "echo", nil))
	mock.ExpectBegin()
	mock.ExpectQuery(`UPDATE agents`).
		WithArgs("agent-1").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("agent-1"))
	mock.ExpectExec(`UPDATE agent_versions`).
		WithArgs("agent-1").
		WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectCommit()

	srv := &runtimeServer{db: db}
	_, err := srv.ArchiveAgent(context.Background(), &runtimev1.ArchiveAgentRequest{
		AgentRef: &runtimev1.AgentRef{Namespace: "demo", Name: "echo"},
	})
	if err != nil {
		t.Fatalf("ArchiveAgent: %v", err)
	}
}

func TestRuntime_ArchiveAgent_alreadyArchived(t *testing.T) {
	db, mock := testSQLxDB(t)
	mock.ExpectQuery(`FROM agents`).
		WithArgs("demo", "echo").
		WillReturnRows(sqlmock.NewRows([]string{"id", "namespace", "name", "archived_at"}).
			AddRow("agent-1", "demo", "echo", nil))
	mock.ExpectBegin()
	mock.ExpectQuery(`UPDATE agents`).
		WithArgs("agent-1").
		WillReturnError(sql.ErrNoRows)
	mock.ExpectRollback()

	srv := &runtimeServer{db: db}
	_, err := srv.ArchiveAgent(context.Background(), &runtimev1.ArchiveAgentRequest{
		AgentRef: &runtimev1.AgentRef{Namespace: "demo", Name: "echo"},
	})
	assertGRPCCode(t, err, codes.FailedPrecondition)
	if !strings.Contains(statusMessage(t, err), "already archived") {
		t.Fatalf("error = %v, want already archived", err)
	}
}

func TestRuntime_RunSession_deprecatedVersion(t *testing.T) {
	db, mock := testSQLxDB(t)
	mock.ExpectQuery(`FROM deployments d`).
		WithArgs("demo", "echo-agent").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "version", "deprecated_at", "retired_at", "archived_at",
		}).AddRow("version-uuid", "1.0.0", time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC), nil, nil))

	srv := &runtimeServer{db: db}
	_, err := srv.RunSession(context.Background(), &runtimev1.RunSessionRequest{
		AgentRef: &runtimev1.AgentRef{Namespace: "demo", Name: "echo-agent", Version: "1.0.0"},
	})
	assertGRPCCode(t, err, codes.FailedPrecondition)
	if !strings.Contains(statusMessage(t, err), "deprecated") {
		t.Fatalf("error = %v, want deprecated", err)
	}
}

func TestRuntime_Publish_archivedAgentRejected(t *testing.T) {
	manifestJSON := resolvedDeployManifestJSON(t)

	db, mock := testSQLxDB(t)
	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO agents`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("agent-uuid"))
	mock.ExpectQuery(`FROM agents`).
		WithArgs("agent-uuid").
		WillReturnRows(sqlmock.NewRows([]string{"id", "namespace", "name", "archived_at"}).
			AddRow("agent-uuid", "demo", "echo-agent", time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)))
	mock.ExpectRollback()

	srv := &runtimeServer{db: db}
	_, err := srv.Publish(context.Background(), &runtimev1.PublishRequest{Manifest: manifestJSON})
	assertGRPCCode(t, err, codes.FailedPrecondition)
	if !strings.Contains(statusMessage(t, err), "archived") {
		t.Fatalf("error = %v, want archived", err)
	}
}

func TestRuntime_ArchiveAgent_beginFailed(t *testing.T) {
	db, mock := testSQLxDB(t)
	mock.ExpectQuery(`FROM agents`).
		WithArgs("demo", "echo").
		WillReturnRows(sqlmock.NewRows([]string{"id", "namespace", "name", "archived_at"}).
			AddRow("agent-1", "demo", "echo", nil))
	mock.ExpectBegin().WillReturnError(errors.New("begin failed"))

	srv := &runtimeServer{db: db}
	_, err := srv.ArchiveAgent(context.Background(), &runtimev1.ArchiveAgentRequest{
		AgentRef: &runtimev1.AgentRef{Namespace: "demo", Name: "echo"},
	})
	assertGRPCCode(t, err, codes.Internal)
}
