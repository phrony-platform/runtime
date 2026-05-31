package core

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/jmoiron/sqlx"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	phronyhealth "github.com/phrony-platform/runtime/gen/grpc/health/v1"
	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
	"github.com/phrony-platform/runtime/internal/version"
)

func TestNewServer_registersServices(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	mock.ExpectQuery(`SELECT value FROM runtime_meta`).
		WithArgs(SchemaMetaVersionKey).
		WillReturnRows(sqlmock.NewRows([]string{"value"}).AddRow("1"))
	mock.ExpectExec(`SELECT 1`).WillReturnResult(sqlmock.NewResult(0, 0))

	db := sqlx.NewDb(sqlDB, "pgx")

	lis := startTestListener(t, db)
	t.Cleanup(func() { lis.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	conn, err := grpc.NewClient(lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("grpc.NewClient: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	versionClient := runtimev1.NewRuntimeClient(conn)
	versionResp, err := versionClient.GetVersion(ctx, &runtimev1.GetVersionRequest{})
	if err != nil {
		t.Fatalf("GetVersion: %v", err)
	}
	if versionResp.GetVersion() != version.Version {
		t.Fatalf("version = %q, want %q", versionResp.GetVersion(), version.Version)
	}
	if versionResp.GetSchemaVersion() != "1" {
		t.Fatalf("schema_version = %q, want 1", versionResp.GetSchemaVersion())
	}

	healthClient := phronyhealth.NewHealthClient(conn)
	healthResp, err := healthClient.Check(ctx, &phronyhealth.HealthCheckRequest{})
	if err != nil {
		t.Fatalf("Health.Check: %v", err)
	}
	if healthResp.GetStatus() != phronyhealth.HealthCheckResponse_SERVING {
		t.Fatalf("health status = %v, want SERVING", healthResp.GetStatus())
	}
}

func startTestListener(t *testing.T, db *sqlx.DB) net.Listener {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	srv, err := NewServer(db)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	go func() {
		_ = srv.GRPC().Serve(lis)
	}()
	t.Cleanup(func() {
		srv.GRPC().Stop()
	})
	return lis
}
