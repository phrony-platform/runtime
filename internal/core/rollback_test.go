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

func TestRuntime_Rollback_toPreviousVersion(t *testing.T) {
	db, mock := testSQLxDB(t)
	agentID := "agent-1"
	mock.ExpectQuery(`FROM agents`).
		WithArgs("demo", "echo").
		WillReturnRows(sqlmock.NewRows([]string{"id", "namespace", "name", "archived_at"}).
			AddRow(agentID, "demo", "echo", nil))
	mock.ExpectQuery(`FROM deployments d`).
		WithArgs("demo", "echo").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "version", "deprecated_at", "retired_at", "archived_at",
		}).AddRow("ver-2", "2.0.0", nil, nil, nil))
	mock.ExpectQuery(`WITH active AS`).
		WithArgs(agentID).
		WillReturnRows(sqlmock.NewRows([]string{"agent_version_id"}).AddRow("ver-1"))
	mock.ExpectQuery(`SELECT version FROM agent_versions`).
		WithArgs("ver-1").
		WillReturnRows(sqlmock.NewRows([]string{"version"}).AddRow("1.0.0"))
	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO deployments`).
		WithArgs(sqlmock.AnyArg(), agentID, "ver-1", "rollback", sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("dep-3"))
	mock.ExpectCommit()

	srv := &runtimeServer{db: db}
	resp, err := srv.Rollback(context.Background(), &runtimev1.RollbackRequest{
		AgentRef: &runtimev1.AgentRef{Namespace: "demo", Name: "echo"},
		Actor:    "alice",
	})
	if err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if resp.GetVersion() != "1.0.0" || resp.GetPreviousVersion() != "2.0.0" {
		t.Fatalf("resp = %+v, want rollback to 1.0.0 from 2.0.0", resp)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestRuntime_Rollback_explicitToVersion(t *testing.T) {
	db, mock := testSQLxDB(t)
	agentID := "agent-1"
	mock.ExpectQuery(`FROM agents`).
		WithArgs("demo", "echo").
		WillReturnRows(sqlmock.NewRows([]string{"id", "namespace", "name", "archived_at"}).
			AddRow(agentID, "demo", "echo", nil))
	mock.ExpectQuery(`FROM deployments d`).
		WithArgs("demo", "echo").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "version", "deprecated_at", "retired_at", "archived_at",
		}).AddRow("ver-2", "2.0.0", nil, nil, nil))
	mock.ExpectQuery(`SELECT av.id, av.deprecated_at`).
		WithArgs("demo", "echo", "1.5.0").
		WillReturnRows(sqlmock.NewRows([]string{"id", "deprecated_at", "retired_at", "archived_at"}).
			AddRow("ver-15", nil, nil, nil))
	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO deployments`).
		WithArgs(sqlmock.AnyArg(), agentID, "ver-15", "rollback", sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("dep-4"))
	mock.ExpectCommit()

	srv := &runtimeServer{db: db}
	resp, err := srv.Rollback(context.Background(), &runtimev1.RollbackRequest{
		AgentRef:  &runtimev1.AgentRef{Namespace: "demo", Name: "echo"},
		ToVersion: "1.5.0",
	})
	if err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if resp.GetVersion() != "1.5.0" {
		t.Fatalf("version = %q, want 1.5.0", resp.GetVersion())
	}
}

func TestRuntime_Rollback_noActiveDeployment(t *testing.T) {
	db, mock := testSQLxDB(t)
	mock.ExpectQuery(`FROM agents`).
		WithArgs("demo", "echo").
		WillReturnRows(sqlmock.NewRows([]string{"id", "namespace", "name", "archived_at"}).
			AddRow("agent-1", "demo", "echo", nil))
	mock.ExpectQuery(`FROM deployments d`).
		WithArgs("demo", "echo").
		WillReturnError(sql.ErrNoRows)

	srv := &runtimeServer{db: db}
	_, err := srv.Rollback(context.Background(), &runtimev1.RollbackRequest{
		AgentRef: &runtimev1.AgentRef{Namespace: "demo", Name: "echo"},
	})
	assertGRPCCode(t, err, codes.FailedPrecondition)
	if !strings.Contains(statusMessage(t, err), "no active deployment") {
		t.Fatalf("error = %v, want no active deployment", err)
	}
}

func TestRuntime_Rollback_noPreviousDeployment(t *testing.T) {
	db, mock := testSQLxDB(t)
	agentID := "agent-1"
	mock.ExpectQuery(`FROM agents`).
		WithArgs("demo", "echo").
		WillReturnRows(sqlmock.NewRows([]string{"id", "namespace", "name", "archived_at"}).
			AddRow(agentID, "demo", "echo", nil))
	mock.ExpectQuery(`FROM deployments d`).
		WithArgs("demo", "echo").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "version", "deprecated_at", "retired_at", "archived_at",
		}).AddRow("ver-1", "1.0.0", nil, nil, nil))
	mock.ExpectQuery(`WITH active AS`).
		WithArgs(agentID).
		WillReturnError(sql.ErrNoRows)

	srv := &runtimeServer{db: db}
	_, err := srv.Rollback(context.Background(), &runtimev1.RollbackRequest{
		AgentRef: &runtimev1.AgentRef{Namespace: "demo", Name: "echo"},
	})
	assertGRPCCode(t, err, codes.FailedPrecondition)
	if !strings.Contains(statusMessage(t, err), "no previous deployment") {
		t.Fatalf("error = %v, want no previous deployment", err)
	}
}

func TestRuntime_Rollback_retiredTargetRejected(t *testing.T) {
	db, mock := testSQLxDB(t)
	mock.ExpectQuery(`FROM agents`).
		WithArgs("demo", "echo").
		WillReturnRows(sqlmock.NewRows([]string{"id", "namespace", "name", "archived_at"}).
			AddRow("agent-1", "demo", "echo", nil))
	mock.ExpectQuery(`FROM deployments d`).
		WithArgs("demo", "echo").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "version", "deprecated_at", "retired_at", "archived_at",
		}).AddRow("ver-2", "2.0.0", nil, nil, nil))
	mock.ExpectQuery(`SELECT av.id, av.deprecated_at`).
		WithArgs("demo", "echo", "1.0.0").
		WillReturnRows(sqlmock.NewRows([]string{"id", "deprecated_at", "retired_at", "archived_at"}).
			AddRow("ver-1", nil, time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC), nil))

	srv := &runtimeServer{db: db}
	_, err := srv.Rollback(context.Background(), &runtimev1.RollbackRequest{
		AgentRef:  &runtimev1.AgentRef{Namespace: "demo", Name: "echo"},
		ToVersion: "1.0.0",
	})
	assertGRPCCode(t, err, codes.FailedPrecondition)
	if !strings.Contains(statusMessage(t, err), "retired") {
		t.Fatalf("error = %v, want retired", err)
	}
}
