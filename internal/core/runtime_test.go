package core

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/jmoiron/sqlx"
	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestRuntime_GetVersion(t *testing.T) {
	srv := &runtimeServer{}
	resp, err := srv.GetVersion(context.Background(), &runtimev1.GetVersionRequest{})
	if err != nil {
		t.Fatalf("GetVersion: %v", err)
	}
	if resp.GetVersion() != RuntimeVersion {
		t.Fatalf("version = %q, want %q", resp.GetVersion(), RuntimeVersion)
	}
	if resp.GetSchemaVersion() != "" {
		t.Fatalf("schema_version = %q, want empty without db", resp.GetSchemaVersion())
	}
}

func TestRuntime_GetVersion_schemaMetaQueryFailed(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	mock.ExpectQuery(`SELECT value FROM runtime_meta`).
		WithArgs(SchemaMetaVersionKey).
		WillReturnError(context.Canceled)

	db := sqlx.NewDb(sqlDB, "pgx")
	srv := &runtimeServer{db: db}
	resp, err := srv.GetVersion(context.Background(), &runtimev1.GetVersionRequest{})
	if err != nil {
		t.Fatalf("GetVersion: %v", err)
	}
	if resp.GetSchemaVersion() != "" {
		t.Fatalf("schema_version = %q, want empty on query error", resp.GetSchemaVersion())
	}
}

func TestRuntime_GetVersion_readsSchemaMeta(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	mock.ExpectQuery(`SELECT value FROM runtime_meta`).
		WithArgs(SchemaMetaVersionKey).
		WillReturnRows(sqlmock.NewRows([]string{"value"}).AddRow("1"))

	db := sqlx.NewDb(sqlDB, "pgx")
	srv := &runtimeServer{db: db}
	resp, err := srv.GetVersion(context.Background(), &runtimev1.GetVersionRequest{})
	if err != nil {
		t.Fatalf("GetVersion: %v", err)
	}
	if resp.GetSchemaVersion() != "1" {
		t.Fatalf("schema_version = %q, want 1", resp.GetSchemaVersion())
	}
}

func TestRuntime_RunSession_unimplemented(t *testing.T) {
	srv := &runtimeServer{}
	_, err := srv.RunSession(context.Background(), &runtimev1.RunSessionRequest{SessionId: "sess-1"})
	assertGRPCCode(t, err, codes.Unimplemented)
}

func assertGRPCCode(t *testing.T, err error, code codes.Code) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error with code %v, got nil", code)
	}
	st, ok := status.FromError(err)
	if !ok {
		t.Fatalf("expected gRPC status, got %v", err)
	}
	if st.Code() != code {
		t.Fatalf("code = %v, want %v", st.Code(), code)
	}
}
