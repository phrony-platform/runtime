package main

import (
	"context"
	"database/sql"
	"encoding/base64"
	"net"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/jmoiron/sqlx"
	grpc_health_v1 "github.com/phrony-platform/runtime/gen/grpc/health/v1"
	"github.com/phrony-platform/runtime/internal/core"
	"github.com/phrony-platform/runtime/internal/secrets"
)

func startTestRuntimeAddr(t *testing.T) string {
	t.Helper()

	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	for range 4 {
		mock.ExpectExec(`SELECT 1`).WillReturnResult(sqlmock.NewResult(0, 0))
	}
	mock.ExpectQuery(`SELECT value FROM runtime_meta`).
		WithArgs(core.SchemaMetaVersionKey).
		WillReturnRows(sqlmock.NewRows([]string{"value"}).AddRow("2"))

	db := sqlx.NewDb(sqlDB, "pgx")
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = lis.Close() })

	srv, err := core.NewServer(db)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	go func() { _ = srv.GRPC().Serve(lis) }()
	t.Cleanup(func() { srv.GRPC().Stop() })

	waitForGRPC(t, lis.Addr().String())
	return lis.Addr().String()
}

func startTestRuntimeAddrForDeploy(t *testing.T) string {
	t.Helper()

	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	mock.ExpectExec(`SELECT 1`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO agents`).
		WithArgs(sqlmock.AnyArg(), "demo", "echo-agent", "", sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("agent-uuid"))
	mock.ExpectQuery(`FROM agents`).
		WithArgs("agent-uuid").
		WillReturnRows(sqlmock.NewRows([]string{"id", "namespace", "name", "archived_at"}).
			AddRow("agent-uuid", "demo", "echo-agent", nil))
	mock.ExpectQuery(`SELECT av.id, av.content_hash`).
		WithArgs("agent-uuid", "1.2.0").
		WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery(`INSERT INTO agent_versions`).
		WithArgs(sqlmock.AnyArg(), "agent-uuid", "1.2.0", sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("version-uuid"))
	mock.ExpectCommit()

	db := sqlx.NewDb(sqlDB, "pgx")
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = lis.Close() })

	srv, err := core.NewServer(db)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	go func() { _ = srv.GRPC().Serve(lis) }()
	t.Cleanup(func() { srv.GRPC().Stop() })

	waitForGRPC(t, lis.Addr().String())
	return lis.Addr().String()
}

func startTestRuntimeAddrForDeployWithSecrets(t *testing.T) string {
	t.Helper()
	t.Setenv(secrets.EnvEncryptionKey, base64.StdEncoding.EncodeToString(make([]byte, 32)))

	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	mock.ExpectExec(`SELECT 1`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO agents`).
		WithArgs(sqlmock.AnyArg(), "demo", "echo-agent", "", sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("agent-uuid"))
	mock.ExpectQuery(`FROM agents`).
		WithArgs("agent-uuid").
		WillReturnRows(sqlmock.NewRows([]string{"id", "namespace", "name", "archived_at"}).
			AddRow("agent-uuid", "demo", "echo-agent", nil))
	mock.ExpectQuery(`SELECT av.id, av.content_hash`).
		WithArgs("agent-uuid", "1.2.0").
		WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery(`INSERT INTO agent_versions`).
		WithArgs(sqlmock.AnyArg(), "agent-uuid", "1.2.0", sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("version-uuid"))
	mock.ExpectExec(`INSERT INTO agent_version_secrets`).
		WithArgs("version-uuid", "anthropic", 1, sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	db := sqlx.NewDb(sqlDB, "pgx")
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = lis.Close() })

	srv, err := core.NewServer(db)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	go func() { _ = srv.GRPC().Serve(lis) }()
	t.Cleanup(func() { srv.GRPC().Stop() })

	waitForGRPC(t, lis.Addr().String())
	return lis.Addr().String()
}

func startTestRuntimeAddrForRun(t *testing.T) string {
	t.Helper()

	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	mock.ExpectExec(`SELECT 1`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(`FROM agent_versions av`).
		WithArgs("demo", "echo-agent").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("version-uuid"))
	mock.ExpectQuery(`INSERT INTO sessions`).
		WithArgs(sqlmock.AnyArg(), "version-uuid", sqlmock.AnyArg(), "pending").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("generated-session"))

	db := sqlx.NewDb(sqlDB, "pgx")
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = lis.Close() })

	srv, err := core.NewServer(db)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	go func() { _ = srv.GRPC().Serve(lis) }()
	t.Cleanup(func() { srv.GRPC().Stop() })

	waitForGRPC(t, lis.Addr().String())
	return lis.Addr().String()
}

func startTestRuntimeAddrForRunWithVersion(t *testing.T) string {
	t.Helper()

	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	mock.ExpectExec(`SELECT 1`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(`FROM agent_versions av`).
		WithArgs("demo", "echo-agent", "1.2.0").
		WillReturnRows(sqlmock.NewRows([]string{"id", "deprecated_at", "archived_at"}).AddRow("version-uuid", nil, nil))
	mock.ExpectQuery(`INSERT INTO sessions`).
		WithArgs(sqlmock.AnyArg(), "version-uuid", sqlmock.AnyArg(), "pending").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("generated-session"))

	db := sqlx.NewDb(sqlDB, "pgx")
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = lis.Close() })

	srv, err := core.NewServer(db)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	go func() { _ = srv.GRPC().Serve(lis) }()
	t.Cleanup(func() { srv.GRPC().Stop() })

	waitForGRPC(t, lis.Addr().String())
	return lis.Addr().String()
}

func startTestRuntimeAddrForAgentsList(t *testing.T) string {
	t.Helper()

	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	mock.ExpectExec(`SELECT 1`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(`FROM agents`).
		WithArgs("").
		WillReturnRows(sqlmock.NewRows([]string{"id", "namespace", "name", "owner", "archived_at", "created_at"}).
			AddRow("agent-uuid", "demo", "echo-agent", "", nil, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)))

	return startRuntimeOnDB(t, sqlDB)
}

func startTestRuntimeAddrForVersionsList(t *testing.T) string {
	t.Helper()

	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	mock.ExpectExec(`SELECT 1`).WillReturnResult(sqlmock.NewResult(0, 0))
	agentID := "agent-uuid"
	mock.ExpectQuery(`FROM agents`).
		WithArgs("demo", "echo-agent").
		WillReturnRows(sqlmock.NewRows([]string{"id", "namespace", "name", "archived_at"}).
			AddRow(agentID, "demo", "echo-agent", nil))
	mock.ExpectQuery(`FROM agent_versions`).
		WithArgs(agentID).
		WillReturnRows(sqlmock.NewRows([]string{"id", "version", "content_hash", "deployed_at", "deprecated_at"}).
			AddRow("ver-1", "1.2.0", "abc", time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), nil))

	return startRuntimeOnDB(t, sqlDB)
}

func startTestRuntimeAddrForDeprecate(t *testing.T) string {
	t.Helper()

	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	mock.ExpectExec(`SELECT 1`).WillReturnResult(sqlmock.NewResult(0, 0))
	agentID := "agent-uuid"
	mock.ExpectQuery(`FROM agents`).
		WithArgs("demo", "echo-agent").
		WillReturnRows(sqlmock.NewRows([]string{"id", "namespace", "name", "archived_at"}).
			AddRow(agentID, "demo", "echo-agent", nil))
	mock.ExpectQuery(`UPDATE agent_versions`).
		WithArgs(agentID, "1.2.0").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("ver-1"))

	return startRuntimeOnDB(t, sqlDB)
}

func startTestRuntimeAddrForArchive(t *testing.T) string {
	t.Helper()

	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	mock.ExpectExec(`SELECT 1`).WillReturnResult(sqlmock.NewResult(0, 0))
	agentID := "agent-uuid"
	mock.ExpectQuery(`FROM agents`).
		WithArgs("demo", "echo-agent").
		WillReturnRows(sqlmock.NewRows([]string{"id", "namespace", "name", "archived_at"}).
			AddRow(agentID, "demo", "echo-agent", nil))
	mock.ExpectBegin()
	mock.ExpectQuery(`UPDATE agents`).
		WithArgs(agentID).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(agentID))
	mock.ExpectExec(`UPDATE agent_versions`).
		WithArgs(agentID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	return startRuntimeOnDB(t, sqlDB)
}

func startRuntimeOnDB(t *testing.T, sqlDB *sql.DB) string {
	t.Helper()
	db := sqlx.NewDb(sqlDB, "pgx")
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = lis.Close() })

	srv, err := core.NewServer(db)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	go func() { _ = srv.GRPC().Serve(lis) }()
	t.Cleanup(func() { srv.GRPC().Stop() })

	waitForGRPC(t, lis.Addr().String())
	return lis.Addr().String()
}

func waitForGRPC(t *testing.T, addr string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := dialRuntime(context.Background(), addr)
		if err == nil {
			_, err = conn.health.Check(context.Background(), &grpc_health_v1.HealthCheckRequest{})
			_ = conn.Close()
			if err == nil {
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("runtime gRPC at %s did not become ready", addr)
}
