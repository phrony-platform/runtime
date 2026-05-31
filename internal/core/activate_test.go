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

func TestRuntime_Deploy_activateSuccess(t *testing.T) {
	db, mock := testSQLxDB(t)
	agentID := "agent-1"
	expectDeployActivateMocks(mock, agentID, "ver-2", "2.0.0", sql.ErrNoRows)

	srv := &runtimeServer{db: db}
	resp, err := srv.Deploy(context.Background(), &runtimev1.DeployRequest{
		AgentRef: &runtimev1.AgentRef{Namespace: "demo", Name: "echo", Version: "2.0.0"},
		Actor:    "alice",
	})
	if err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	if resp.GetVersion() != "2.0.0" || resp.GetPreviousVersion() != "" {
		t.Fatalf("resp = %+v, want version 2.0.0 and empty previous", resp)
	}
	if resp.GetNamespace() != "demo" || resp.GetName() != "echo" {
		t.Fatalf("identity = %s/%s", resp.GetNamespace(), resp.GetName())
	}
	if resp.GetDeployedAt() == "" {
		t.Fatal("deployed_at is empty")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestRuntime_Deploy_withPreviousActive(t *testing.T) {
	db, mock := testSQLxDB(t)
	agentID := "agent-1"
	mock.ExpectQuery(`FROM agents`).
		WithArgs("demo", "echo").
		WillReturnRows(sqlmock.NewRows([]string{"id", "namespace", "name", "archived_at"}).
			AddRow(agentID, "demo", "echo", nil))
	mock.ExpectQuery(`SELECT av.id, av.deprecated_at`).
		WithArgs("demo", "echo", "2.0.0").
		WillReturnRows(sqlmock.NewRows([]string{"id", "deprecated_at", "retired_at", "archived_at"}).
			AddRow("ver-2", nil, nil, nil))
	mock.ExpectQuery(`FROM deployments d`).
		WithArgs("demo", "echo").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "version", "deprecated_at", "retired_at", "archived_at",
		}).AddRow("ver-1", "1.0.0", nil, nil, nil))
	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO deployments`).
		WithArgs(sqlmock.AnyArg(), agentID, "ver-2", "deploy", "bob").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("dep-2"))
	mock.ExpectCommit()

	srv := &runtimeServer{db: db}
	resp, err := srv.Deploy(context.Background(), &runtimev1.DeployRequest{
		AgentRef: &runtimev1.AgentRef{Namespace: "demo", Name: "echo", Version: "2.0.0"},
		Actor:    "bob",
	})
	if err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	if resp.GetPreviousVersion() != "1.0.0" {
		t.Fatalf("previous_version = %q, want 1.0.0", resp.GetPreviousVersion())
	}
}

func TestRuntime_Deploy_missingVersion(t *testing.T) {
	srv := &runtimeServer{db: testServeDB(t)}
	_, err := srv.Deploy(context.Background(), &runtimev1.DeployRequest{
		AgentRef: &runtimev1.AgentRef{Namespace: "demo", Name: "echo"},
	})
	assertGRPCCode(t, err, codes.InvalidArgument)
}

func TestRuntime_Deploy_retiredVersionRejected(t *testing.T) {
	db, mock := testSQLxDB(t)
	mock.ExpectQuery(`FROM agents`).
		WithArgs("demo", "echo").
		WillReturnRows(sqlmock.NewRows([]string{"id", "namespace", "name", "archived_at"}).
			AddRow("agent-1", "demo", "echo", nil))
	mock.ExpectQuery(`SELECT av.id, av.deprecated_at`).
		WithArgs("demo", "echo", "1.0.0").
		WillReturnRows(sqlmock.NewRows([]string{"id", "deprecated_at", "retired_at", "archived_at"}).
			AddRow("ver-1", nil, time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC), nil))

	srv := &runtimeServer{db: db}
	_, err := srv.Deploy(context.Background(), &runtimev1.DeployRequest{
		AgentRef: &runtimev1.AgentRef{Namespace: "demo", Name: "echo", Version: "1.0.0"},
	})
	assertGRPCCode(t, err, codes.FailedPrecondition)
	if !strings.Contains(statusMessage(t, err), "retired") {
		t.Fatalf("error = %v, want retired", err)
	}
}

func TestRuntime_Deploy_versionNotPublished(t *testing.T) {
	db, mock := testSQLxDB(t)
	mock.ExpectQuery(`FROM agents`).
		WithArgs("demo", "echo").
		WillReturnRows(sqlmock.NewRows([]string{"id", "namespace", "name", "archived_at"}).
			AddRow("agent-1", "demo", "echo", nil))
	mock.ExpectQuery(`SELECT av.id, av.deprecated_at`).
		WithArgs("demo", "echo", "9.9.9").
		WillReturnError(sql.ErrNoRows)

	srv := &runtimeServer{db: db}
	_, err := srv.Deploy(context.Background(), &runtimev1.DeployRequest{
		AgentRef: &runtimev1.AgentRef{Namespace: "demo", Name: "echo", Version: "9.9.9"},
	})
	assertGRPCCode(t, err, codes.NotFound)
}

func expectDeployActivateMocks(mock sqlmock.Sqlmock, agentID, versionID, version string, activeErr error) {
	mock.ExpectQuery(`FROM agents`).
		WithArgs("demo", "echo").
		WillReturnRows(sqlmock.NewRows([]string{"id", "namespace", "name", "archived_at"}).
			AddRow(agentID, "demo", "echo", nil))
	mock.ExpectQuery(`SELECT av.id, av.deprecated_at`).
		WithArgs("demo", "echo", version).
		WillReturnRows(sqlmock.NewRows([]string{"id", "deprecated_at", "retired_at", "archived_at"}).
			AddRow(versionID, nil, nil, nil))
	mock.ExpectQuery(`FROM deployments d`).
		WithArgs("demo", "echo").
		WillReturnError(activeErr)
	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO deployments`).
		WithArgs(sqlmock.AnyArg(), agentID, versionID, "deploy", sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("dep-1"))
	mock.ExpectCommit()
}
