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
	"github.com/phrony-platform/runtime/internal/model"
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
	expectCLIRunSecretsMocks(mock)
	mock.ExpectQuery(`FROM deployments d`).
		WithArgs("demo", "echo-agent").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "version", "deprecated_at", "retired_at", "archived_at",
		}).AddRow("version-uuid", "1.2.0", nil, nil, nil))
	expectInteractiveRunSessionMocks(mock, "version-uuid", sqlmock.AnyArg())

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

func startTestRuntimeAddrForRunAttach(t *testing.T) string {
	t.Helper()

	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	mock.MatchExpectationsInOrder(false)
	t.Cleanup(func() { _ = sqlDB.Close() })
	mock.ExpectExec(`SELECT 1`).WillReturnResult(sqlmock.NewResult(0, 0))
	expectCLIRunSecretsMocks(mock)
	mock.ExpectQuery(`FROM deployments d`).
		WithArgs("demo", "echo-agent").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "version", "deprecated_at", "retired_at", "archived_at",
		}).AddRow("version-uuid", "1.2.0", nil, nil, nil))
	expectRunAttachSessionMocks(mock, "version-uuid", sqlmock.AnyArg())

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
	expectCLIRunSecretsVersionedMocks(mock, "1.2.0")
	mock.ExpectQuery(`FROM deployments d`).
		WithArgs("demo", "echo-agent").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "version", "deprecated_at", "retired_at", "archived_at",
		}).AddRow("version-uuid", "1.2.0", nil, nil, nil))
	expectInteractiveRunSessionMocks(mock, "version-uuid", sqlmock.AnyArg())

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

const runTestManifestJSON = `{
	"apiVersion":"phrony.com/v1",
	"kind":"Agent",
	"metadata":{"name":"echo-agent","namespace":"demo","version":"1.2.0"},
	"spec":{
		"purpose":"p",
		"instructions":{"text":"System."},
		"model":{"provider":"stub","name":"stub-script"}
	}
}`

// expectCLIRunSecretsMocks satisfies GetActiveVersion and GetAgentVersion before RunSession.
func expectCLIRunSecretsMocks(mock sqlmock.Sqlmock) {
	now := time.Now()
	mock.ExpectQuery(`SELECT av.version, d.created_at, d.actor`).
		WithArgs("demo", "echo-agent").
		WillReturnRows(sqlmock.NewRows([]string{"version", "created_at", "actor"}).
			AddRow("1.2.0", now, "test"))
	expectCLIRunSecretsVersionedMocks(mock, "1.2.0")
}

func expectCLIRunSecretsVersionedMocks(mock sqlmock.Sqlmock, version string) {
	now := time.Now()
	mock.ExpectQuery(`FROM agent_versions av`).
		WithArgs("demo", "echo-agent", version).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "version", "content_hash", "manifest", "published_at", "deprecated_at", "retired_at",
		}).AddRow("version-uuid", version, "hash", []byte(runTestManifestJSON), now, nil, nil))
}

func expectInteractiveRunSessionMocks(mock sqlmock.Sqlmock, versionID string, input any) {
	manifest := []byte(runTestManifestJSON)
	mock.ExpectQuery(`SELECT manifest`).
		WithArgs(versionID).
		WillReturnRows(sqlmock.NewRows([]string{"manifest"}).AddRow(manifest))
	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO sessions`).
		WithArgs(sqlmock.AnyArg(), versionID, input, model.SessionStatusRunning, nil, 0, "").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("generated-session"))
	mock.ExpectCommit()
	mock.ExpectQuery(`SELECT manifest`).
		WithArgs(versionID).
		WillReturnRows(sqlmock.NewRows([]string{"manifest"}).AddRow(manifest))
	mock.ExpectQuery(`UPDATE sessions`).
		WillReturnRows(sqlmock.NewRows([]string{"updated_at"}).AddRow(time.Now()))
}

// expectRunAttachSessionMocks satisfies GetSession when phrony run --attach calls
// RunSession then RunSessionInteractive(session_id).
func expectRunAttachSessionMocks(mock sqlmock.Sqlmock, versionID string, input any) {
	expectInteractiveRunSessionMocks(mock, versionID, input)
	sessionInput := []byte(`{}`)
	now := time.Now()
	mock.ExpectQuery(`FROM sessions`).
		WithArgs(sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "agent_version_id", "input", "status", "output", "error", "history", "created_at", "updated_at",
		}).AddRow("run_attach_test", versionID, sessionInput, model.SessionStatusRunning, nil, nil, []byte(`[]`), now, now))
	mock.ExpectQuery(`SELECT manifest`).
		WithArgs(versionID).
		WillReturnError(sql.ErrNoRows)
}

func startTestRuntimeAddrForBundlesList(t *testing.T) string {
	t.Helper()

	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	mock.ExpectExec(`SELECT 1`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(`FROM bundles`).
		WithArgs("").
		WillReturnRows(sqlmock.NewRows([]string{"id", "namespace", "name", "owner", "created_at"}).
			AddRow("bundle-uuid", "demo", "payment-desk-hitl", "team", time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)))

	return startRuntimeOnDB(t, sqlDB)
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
		WillReturnRows(sqlmock.NewRows([]string{"id", "version", "content_hash", "deployed_at", "deprecated_at", "retired_at"}).
			AddRow("ver-1", "1.2.0", "abc", time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), nil, nil))

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

func startTestRuntimeAddrForActive(t *testing.T) string {
	t.Helper()

	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	mock.ExpectExec(`SELECT 1`).WillReturnResult(sqlmock.NewResult(0, 0))
	deployed := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	mock.ExpectQuery(`FROM deployments d`).
		WithArgs("demo", "echo-agent").
		WillReturnRows(sqlmock.NewRows([]string{"version", "created_at", "actor"}).
			AddRow("1.2.0", deployed, "alice"))

	return startRuntimeOnDB(t, sqlDB)
}

func startTestRuntimeAddrForHistory(t *testing.T) string {
	t.Helper()

	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	agentID := "agent-uuid"
	mock.ExpectExec(`SELECT 1`).WillReturnResult(sqlmock.NewResult(0, 0))
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	mock.ExpectQuery(`FROM agents`).
		WithArgs("demo", "echo-agent").
		WillReturnRows(sqlmock.NewRows([]string{"id", "namespace", "name", "archived_at"}).
			AddRow(agentID, "demo", "echo-agent", nil))
	mock.ExpectQuery(`FROM deployments d`).
		WithArgs(agentID).
		WillReturnRows(sqlmock.NewRows([]string{"version", "action", "actor", "created_at"}).
			AddRow("1.2.0", "deploy", "alice", now).
			AddRow("1.0.0", "rollback", "bob", now.Add(-time.Hour)))

	return startRuntimeOnDB(t, sqlDB)
}

func startTestRuntimeAddrForBundleVersionsList(t *testing.T) string {
	t.Helper()

	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	bundleID := "bundle-uuid"
	mock.ExpectExec(`SELECT 1`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(`FROM bundles`).
		WithArgs("demo", "payment-desk-hitl").
		WillReturnRows(sqlmock.NewRows([]string{"id", "namespace", "name"}).
			AddRow(bundleID, "demo", "payment-desk-hitl"))
	mock.ExpectQuery(`FROM bundle_versions`).
		WithArgs(bundleID).
		WillReturnRows(sqlmock.NewRows([]string{"id", "version", "lock_hash", "created_at"}).
			AddRow("bv-1", "1.0.0", "sha256:abc", time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)))

	return startRuntimeOnDB(t, sqlDB)
}

func startTestRuntimeAddrForBundleActive(t *testing.T) string {
	t.Helper()

	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	deployed := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	mock.ExpectExec(`SELECT 1`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(`FROM bundle_deployments bd`).
		WithArgs("demo", "payment-desk-hitl").
		WillReturnRows(sqlmock.NewRows([]string{"version", "lock_hash", "created_at", "actor"}).
			AddRow("1.0.0", "sha256:abc", deployed, "alice"))

	return startRuntimeOnDB(t, sqlDB)
}

func startTestRuntimeAddrForBundleHistory(t *testing.T) string {
	t.Helper()

	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	bundleID := "bundle-uuid"
	mock.ExpectExec(`SELECT 1`).WillReturnResult(sqlmock.NewResult(0, 0))
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	mock.ExpectQuery(`FROM bundles`).
		WithArgs("demo", "payment-desk-hitl").
		WillReturnRows(sqlmock.NewRows([]string{"id", "namespace", "name"}).
			AddRow(bundleID, "demo", "payment-desk-hitl"))
	mock.ExpectQuery(`FROM bundle_deployments bd`).
		WithArgs(bundleID).
		WillReturnRows(sqlmock.NewRows([]string{"version", "lock_hash", "action", "actor", "created_at"}).
			AddRow("1.0.1", "sha256:def", "deploy", "alice", now).
			AddRow("1.0.0", "sha256:abc", "deploy", "bob", now.Add(-time.Hour)))

	return startRuntimeOnDB(t, sqlDB)
}

func startTestRuntimeAddrForBundleVersionsEmpty(t *testing.T) string {
	t.Helper()

	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	bundleID := "bundle-uuid"
	mock.ExpectExec(`SELECT 1`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(`FROM bundles`).
		WithArgs("demo", "payment-desk-hitl").
		WillReturnRows(sqlmock.NewRows([]string{"id", "namespace", "name"}).
			AddRow(bundleID, "demo", "payment-desk-hitl"))
	mock.ExpectQuery(`FROM bundle_versions`).
		WithArgs(bundleID).
		WillReturnRows(sqlmock.NewRows([]string{"id", "version", "lock_hash", "created_at"}))

	return startRuntimeOnDB(t, sqlDB)
}

func startTestRuntimeAddrForBundleVersionsNotFound(t *testing.T) string {
	t.Helper()

	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	mock.ExpectExec(`SELECT 1`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(`FROM bundles`).
		WithArgs("demo", "missing").
		WillReturnError(sql.ErrNoRows)

	return startRuntimeOnDB(t, sqlDB)
}

func startTestRuntimeAddrForBundleActiveNoDeployment(t *testing.T) string {
	t.Helper()

	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	mock.ExpectExec(`SELECT 1`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(`FROM bundle_deployments bd`).
		WithArgs("demo", "payment-desk-hitl").
		WillReturnError(sql.ErrNoRows)

	return startRuntimeOnDB(t, sqlDB)
}

func startTestRuntimeAddrForBundleHistoryNotFound(t *testing.T) string {
	t.Helper()

	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	mock.ExpectExec(`SELECT 1`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(`FROM bundles`).
		WithArgs("demo", "missing").
		WillReturnError(sql.ErrNoRows)

	return startRuntimeOnDB(t, sqlDB)
}

func startTestRuntimeAddrForRollback(t *testing.T) string {
	t.Helper()

	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	agentID := "agent-uuid"
	mock.ExpectExec(`SELECT 1`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(`FROM agents`).
		WithArgs("demo", "echo-agent").
		WillReturnRows(sqlmock.NewRows([]string{"id", "namespace", "name", "archived_at"}).
			AddRow(agentID, "demo", "echo-agent", nil))
	mock.ExpectQuery(`FROM deployments d`).
		WithArgs("demo", "echo-agent").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "version", "deprecated_at", "retired_at", "archived_at",
		}).AddRow("ver-2", "1.2.0", nil, nil, nil))
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

	return startRuntimeOnDB(t, sqlDB)
}

func startTestRuntimeAddrForRetire(t *testing.T) string {
	t.Helper()

	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	agentID := "agent-uuid"
	mock.ExpectExec(`SELECT 1`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(`FROM agents`).
		WithArgs("demo", "echo-agent").
		WillReturnRows(sqlmock.NewRows([]string{"id", "namespace", "name", "archived_at"}).
			AddRow(agentID, "demo", "echo-agent", nil))
	mock.ExpectQuery(`UPDATE agent_versions`).
		WithArgs(agentID, "1.0.0").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("ver-old"))

	return startRuntimeOnDB(t, sqlDB)
}

func startTestRuntimeAddrForRunsCancel(t *testing.T) string {
	t.Helper()

	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	mock.ExpectExec(`SELECT 1`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(`UPDATE sessions`).
		WithArgs("run_abc").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("run_abc"))

	return startRuntimeOnDB(t, sqlDB)
}

func startTestRuntimeAddrForDeployActivate(t *testing.T) string {
	t.Helper()

	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	agentID := "agent-uuid"
	mock.ExpectExec(`SELECT 1`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(`FROM agents`).
		WithArgs("demo", "echo-agent").
		WillReturnRows(sqlmock.NewRows([]string{"id", "namespace", "name", "archived_at"}).
			AddRow(agentID, "demo", "echo-agent", nil))
	mock.ExpectQuery(`SELECT av.id, av.deprecated_at`).
		WithArgs("demo", "echo-agent", "1.2.0").
		WillReturnRows(sqlmock.NewRows([]string{"id", "deprecated_at", "retired_at", "archived_at"}).
			AddRow("ver-1", nil, nil, nil))
	mock.ExpectQuery(`FROM deployments d`).
		WithArgs("demo", "echo-agent").
		WillReturnError(sql.ErrNoRows)
	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO deployments`).
		WithArgs(sqlmock.AnyArg(), agentID, "ver-1", "deploy", sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("dep-1"))
	mock.ExpectCommit()

	return startRuntimeOnDB(t, sqlDB)
}

func sessionListMockColumns() []string {
	return []string{
		"id", "agent_version_id", "status", "created_at", "updated_at", "bundle_version_id",
		"agent_namespace", "agent_name", "agent_version",
		"bundle_namespace", "bundle_name", "bundle_version",
	}
}

func addSessionListMockRow(rows *sqlmock.Rows, id, agentVersionID, status string, createdAt, updatedAt time.Time) *sqlmock.Rows {
	return rows.AddRow(
		id, agentVersionID, status, createdAt, updatedAt, nil,
		"demo", "echo-agent", "1.2.0",
		nil, nil, nil,
	)
}

func startTestRuntimeAddrForRunsListAll(t *testing.T) string {
	t.Helper()

	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	mock.ExpectExec(`SELECT 1`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(`FROM sessions`).
		WithArgs("", "", false, "", "").
		WillReturnRows(addSessionListMockRow(
			sqlmock.NewRows(sessionListMockColumns()),
			"sess-await", "version-uuid", "awaiting_input",
			time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC), time.Date(2026, 3, 1, 13, 0, 0, 0, time.UTC),
		))

	return startRuntimeOnDB(t, sqlDB)
}

func startTestRuntimeAddrForSessionsList(t *testing.T) string {
	t.Helper()

	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	mock.ExpectExec(`SELECT 1`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(`FROM deployments d`).
		WithArgs("demo", "echo-agent").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "version", "deprecated_at", "retired_at", "archived_at",
		}).AddRow("version-uuid", "1.2.0", nil, nil, nil))
	rows := sqlmock.NewRows(sessionListMockColumns())
	rows = addSessionListMockRow(rows, "sess-await", "version-uuid", "awaiting_input",
		time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC), time.Date(2026, 3, 1, 13, 0, 0, 0, time.UTC))
	rows = addSessionListMockRow(rows, "sess-done", "version-uuid", "completed",
		time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC), time.Date(2026, 3, 1, 11, 0, 0, 0, time.UTC))
	mock.ExpectQuery(`FROM sessions`).
		WithArgs("version-uuid", "", false, "", "").
		WillReturnRows(rows)

	return startRuntimeOnDB(t, sqlDB)
}

const attachTestManifestJSON = `{
	"apiVersion":"phrony.com/v1",
	"kind":"Agent",
	"metadata":{"name":"echo-agent","namespace":"demo","version":"1.2.0"},
	"secrets":{"anthropic":{"fromEnv":"ANTHROPIC_API_KEY"}},
	"spec":{
		"purpose":"p",
		"instructions":{"text":"System."},
		"model":{"provider":"anthropic","name":"claude-sonnet-4-5"}
	}
}`

func startTestRuntimeAddrForApprovalsList(t *testing.T) string {
	t.Helper()

	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	mock.ExpectExec(`SELECT 1`).WillReturnResult(sqlmock.NewResult(0, 0))
	now := time.Now()
	mock.ExpectQuery(`FROM approvals a`).
		WithArgs("pending", "", "", "", "").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "session_id", "call_id", "status", "route", "reason", "decided_by", "comment",
			"created_at", "decided_at",
			"tool", "version", "args", "authority_ref", "policy_name",
			"approvals_required", "approvals_received", "comprehension_required",
			"on_reject", "on_modify", "expires_at", "policy_runtime",
		}).AddRow(
			"appr-1", "sess-1", "call-1", "pending", "ops", "severity", "", "",
			now, nil,
			"payments.charge", "1.0.0", []byte(`{"amount":100}`), "", "payment-policy",
			1, 0, false,
			"", "", nil, nil,
		))

	return startRuntimeOnDB(t, sqlDB)
}

func startTestRuntimeAddrForApprovalsShow(t *testing.T) string {
	t.Helper()

	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	mock.ExpectExec(`SELECT 1`).WillReturnResult(sqlmock.NewResult(0, 0))
	now := time.Now()
	mock.ExpectQuery(`FROM approvals`).WithArgs("appr-1").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "session_id", "call_id", "status", "route", "reason", "decided_by", "comment",
			"created_at", "decided_at",
			"tool", "version", "args", "authority_ref", "policy_name",
			"approvals_required", "approvals_received", "comprehension_required",
			"on_reject", "on_modify", "expires_at", "policy_runtime",
		}).AddRow(
			"appr-1", "sess-1", "call-1", "pending", "ops", "severity", "", "",
			now, nil,
			"payments.charge", "1.0.0", []byte(`{"amount":100}`), "", "payment-policy",
			1, 0, true,
			"return_to_agent", "revalidate", nil, nil,
		))
	mock.ExpectQuery(`FROM approval_votes`).WithArgs("appr-1").
		WillReturnRows(sqlmock.NewRows([]string{
			"decided_by", "decision", "comment", "comprehension_acknowledged", "created_at",
		}))

	return startRuntimeOnDB(t, sqlDB)
}

func startTestRuntimeAddrForSessionsListFiltered(t *testing.T) string {
	t.Helper()

	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	mock.ExpectExec(`SELECT 1`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(`FROM deployments d`).
		WithArgs("demo", "echo-agent").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "version", "deprecated_at", "retired_at", "archived_at",
		}).AddRow("version-uuid", "1.2.0", nil, nil, nil))
	mock.ExpectQuery(`FROM sessions`).
		WithArgs("version-uuid", "awaiting_input", false, "", "").
		WillReturnRows(addSessionListMockRow(
			sqlmock.NewRows(sessionListMockColumns()),
			"sess-await", "version-uuid", "awaiting_input",
			time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC), time.Date(2026, 3, 1, 13, 0, 0, 0, time.UTC),
		))

	return startRuntimeOnDB(t, sqlDB)
}

func startTestRuntimeAddrForRunAttachFailed(t *testing.T) string {
	t.Helper()

	testKey := make([]byte, 32)
	t.Setenv(secrets.EnvEncryptionKey, base64.StdEncoding.EncodeToString(testKey))
	enc, err := secrets.NewEncryptor(testKey, 1)
	if err != nil {
		t.Fatalf("NewEncryptor: %v", err)
	}
	sealed, err := enc.Encrypt("sess-failed", "anthropic", []byte("sk-test"))
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	mock.ExpectExec(`SELECT 1`).WillReturnResult(sqlmock.NewResult(0, 0))
	now := time.Now()
	errMsg := "model unavailable"
	mock.ExpectQuery(`FROM sessions`).
		WithArgs("sess-failed").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "agent_version_id", "input", "status", "output", "error", "history", "created_at", "updated_at",
		}).AddRow("sess-failed", "version-uuid", []byte("{}"), "failed", nil, &errMsg, []byte(`[]`), now, now))
	mock.ExpectQuery(`SELECT manifest`).
		WithArgs("version-uuid").
		WillReturnRows(sqlmock.NewRows([]string{"manifest"}).AddRow([]byte(attachTestManifestJSON)))
	mock.ExpectQuery(`SELECT key_version, nonce, ciphertext`).
		WithArgs("sess-failed", "anthropic").
		WillReturnRows(sqlmock.NewRows([]string{"key_version", "nonce", "ciphertext"}).
			AddRow(sealed.KeyVersion, sealed.Nonce, sealed.Ciphertext))

	return startRuntimeOnDB(t, sqlDB)
}

func startTestRuntimeAddrForRunAttachCompleted(t *testing.T) string {
	t.Helper()

	testKey := make([]byte, 32)
	t.Setenv(secrets.EnvEncryptionKey, base64.StdEncoding.EncodeToString(testKey))
	enc, err := secrets.NewEncryptor(testKey, 1)
	if err != nil {
		t.Fatalf("NewEncryptor: %v", err)
	}
	sealed, err := enc.Encrypt("sess-completed", "anthropic", []byte("sk-test"))
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	mock.ExpectExec(`SELECT 1`).WillReturnResult(sqlmock.NewResult(0, 0))
	now := time.Now()
	output := []byte(`{"message":"done","stop_reason":"end_turn"}`)
	mock.ExpectQuery(`FROM sessions`).
		WithArgs("sess-completed").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "agent_version_id", "input", "status", "output", "error", "history", "created_at", "updated_at",
		}).AddRow("sess-completed", "version-uuid", []byte("{}"), "completed", output, nil, []byte(`[]`), now, now))
	mock.ExpectQuery(`SELECT manifest`).
		WithArgs("version-uuid").
		WillReturnRows(sqlmock.NewRows([]string{"manifest"}).AddRow([]byte(attachTestManifestJSON)))
	mock.ExpectQuery(`SELECT key_version, nonce, ciphertext`).
		WithArgs("sess-completed", "anthropic").
		WillReturnRows(sqlmock.NewRows([]string{"key_version", "nonce", "ciphertext"}).
			AddRow(sealed.KeyVersion, sealed.Nonce, sealed.Ciphertext))

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
