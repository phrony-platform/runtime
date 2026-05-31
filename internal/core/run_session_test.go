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

func TestRuntime_RunSession_latestVersion(t *testing.T) {
	db, mock := testSQLxDB(t)
	expectActiveDeployment(mock, "demo", "echo-agent", "version-uuid", "1.2.0")
	mock.ExpectQuery(`INSERT INTO sessions`).
		WithArgs(sqlmock.AnyArg(), "version-uuid", []byte("{}"), runSessionStatusPending).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("generated-session"))

	srv := &runtimeServer{db: db}
	resp, err := srv.RunSession(context.Background(), &runtimev1.RunSessionRequest{
		AgentRef: &runtimev1.AgentRef{Namespace: "demo", Name: "echo-agent"},
	})
	if err != nil {
		t.Fatalf("RunSession: %v", err)
	}
	if !strings.HasPrefix(resp.GetSessionId(), "run_") {
		t.Fatalf("session_id = %q, want run_ prefix", resp.GetSessionId())
	}
	if resp.GetAgentVersionId() != "version-uuid" {
		t.Fatalf("agent_version_id = %q, want version-uuid", resp.GetAgentVersionId())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestRuntime_RunSession_specificVersion(t *testing.T) {
	db, mock := testSQLxDB(t)
	expectActiveDeployment(mock, "demo", "echo-agent", "version-uuid", "1.2.0")
	mock.ExpectQuery(`INSERT INTO sessions`).
		WithArgs(sqlmock.AnyArg(), "version-uuid", []byte(`{"q":"hi"}`), runSessionStatusPending).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("generated-session"))

	srv := &runtimeServer{db: db}
	resp, err := srv.RunSession(context.Background(), &runtimev1.RunSessionRequest{
		AgentRef: &runtimev1.AgentRef{
			Namespace: "demo",
			Name:      "echo-agent",
			Version:   "1.2.0",
		},
		Input: []byte(`{"q":"hi"}`),
	})
	if err != nil {
		t.Fatalf("RunSession: %v", err)
	}
	if resp.GetAgentVersionId() != "version-uuid" {
		t.Fatalf("agent_version_id = %q, want version-uuid", resp.GetAgentVersionId())
	}
}

func TestRuntime_RunSession_missingAgentRef(t *testing.T) {
	srv := &runtimeServer{db: testServeDB(t)}
	_, err := srv.RunSession(context.Background(), &runtimev1.RunSessionRequest{})
	assertGRPCCode(t, err, codes.InvalidArgument)
}

func TestRuntime_RunSession_noDatabase(t *testing.T) {
	srv := &runtimeServer{}
	_, err := srv.RunSession(context.Background(), &runtimev1.RunSessionRequest{
		AgentRef: &runtimev1.AgentRef{Namespace: "demo", Name: "echo"},
	})
	assertGRPCCode(t, err, codes.FailedPrecondition)
}

func TestRuntime_RunSession_agentNotFound(t *testing.T) {
	db, mock := testSQLxDB(t)
	mock.ExpectQuery(`FROM deployments d`).
		WithArgs("demo", "missing").
		WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery(`FROM agents`).
		WithArgs("demo", "missing").
		WillReturnError(sql.ErrNoRows)

	srv := &runtimeServer{db: db}
	_, err := srv.RunSession(context.Background(), &runtimev1.RunSessionRequest{
		AgentRef: &runtimev1.AgentRef{Namespace: "demo", Name: "missing"},
	})
	assertGRPCCode(t, err, codes.NotFound)
}

func TestRuntime_RunSession_versionNotActive(t *testing.T) {
	db, mock := testSQLxDB(t)
	expectActiveDeployment(mock, "demo", "echo-agent", "version-uuid", "1.2.0")

	srv := &runtimeServer{db: db}
	_, err := srv.RunSession(context.Background(), &runtimev1.RunSessionRequest{
		AgentRef: &runtimev1.AgentRef{Namespace: "demo", Name: "echo-agent", Version: "9.9.9"},
	})
	assertGRPCCode(t, err, codes.FailedPrecondition)
	if !strings.Contains(statusMessage(t, err), "not the active deployment") {
		t.Fatalf("error = %v, want non-active version message", err)
	}
}

func TestRuntime_RunSession_noActiveDeployment(t *testing.T) {
	db, mock := testSQLxDB(t)
	mock.ExpectQuery(`FROM deployments d`).
		WithArgs("demo", "echo-agent").
		WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery(`FROM agents`).
		WithArgs("demo", "echo-agent").
		WillReturnRows(sqlmock.NewRows([]string{"id", "namespace", "name", "archived_at"}).
			AddRow("agent-1", "demo", "echo-agent", nil))

	srv := &runtimeServer{db: db}
	_, err := srv.RunSession(context.Background(), &runtimev1.RunSessionRequest{
		AgentRef: &runtimev1.AgentRef{Namespace: "demo", Name: "echo-agent"},
	})
	assertGRPCCode(t, err, codes.FailedPrecondition)
	if !strings.Contains(statusMessage(t, err), "no active deployment") {
		t.Fatalf("error = %v, want no active deployment", err)
	}
}

func TestRuntime_RunSession_retiredActiveVersion(t *testing.T) {
	db, mock := testSQLxDB(t)
	mock.ExpectQuery(`FROM deployments d`).
		WithArgs("demo", "echo-agent").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "version", "deprecated_at", "retired_at", "archived_at",
		}).AddRow("version-uuid", "1.0.0", nil, time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC), nil))

	srv := &runtimeServer{db: db}
	_, err := srv.RunSession(context.Background(), &runtimev1.RunSessionRequest{
		AgentRef: &runtimev1.AgentRef{Namespace: "demo", Name: "echo-agent"},
	})
	assertGRPCCode(t, err, codes.FailedPrecondition)
	if !strings.Contains(statusMessage(t, err), "retired") {
		t.Fatalf("error = %v, want retired", err)
	}
}

func TestRuntime_RunSession_invalidInput(t *testing.T) {
	srv := &runtimeServer{db: testServeDB(t)}
	_, err := srv.RunSession(context.Background(), &runtimev1.RunSessionRequest{
		AgentRef: &runtimev1.AgentRef{Namespace: "demo", Name: "echo"},
		Input:    []byte(`["not","object"]`),
	})
	assertGRPCCode(t, err, codes.InvalidArgument)
	if !strings.Contains(statusMessage(t, err), "JSON object") {
		t.Fatalf("error = %v, want JSON object", err)
	}
}

func TestNormalizeSessionInput_defaultsEmpty(t *testing.T) {
	raw, err := normalizeSessionInput(nil)
	if err != nil {
		t.Fatalf("normalizeSessionInput: %v", err)
	}
	if string(raw) != "{}" {
		t.Fatalf("input = %s, want {}", raw)
	}
}

func TestNormalizeSessionInput_invalidJSON(t *testing.T) {
	_, err := normalizeSessionInput([]byte(`{not json`))
	assertGRPCCode(t, err, codes.InvalidArgument)
	if !strings.Contains(statusMessage(t, err), "valid JSON") {
		t.Fatalf("error = %v, want valid JSON", err)
	}
}
