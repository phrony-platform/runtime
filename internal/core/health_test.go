package core

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	grpc_health_v1 "github.com/phrony-platform/runtime/gen/grpc/health/v1"
	"github.com/jmoiron/sqlx"
	"google.golang.org/grpc/codes"
)

func TestHealth_Check_ready(t *testing.T) {
	db, mock := testSQLxDB(t)
	mock.ExpectExec(`SELECT 1`).WillReturnResult(sqlmock.NewResult(0, 0))

	srv := &healthServer{db: db}
	resp, err := srv.Check(context.Background(), &grpc_health_v1.HealthCheckRequest{})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if resp.GetStatus() != grpc_health_v1.HealthCheckResponse_SERVING {
		t.Fatalf("status = %v, want SERVING", resp.GetStatus())
	}
}

func TestHealth_Check_notReady(t *testing.T) {
	db, mock := testSQLxDB(t)
	mock.ExpectExec(`SELECT 1`).WillReturnError(context.Canceled)

	srv := &healthServer{db: db}
	resp, err := srv.Check(context.Background(), &grpc_health_v1.HealthCheckRequest{})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if resp.GetStatus() != grpc_health_v1.HealthCheckResponse_NOT_SERVING {
		t.Fatalf("status = %v, want NOT_SERVING", resp.GetStatus())
	}
}

func TestHealth_Check_unknownService(t *testing.T) {
	db, _ := testSQLxDB(t)
	srv := &healthServer{db: db}

	resp, err := srv.Check(context.Background(), &grpc_health_v1.HealthCheckRequest{Service: "other.Service"})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if resp.GetStatus() != grpc_health_v1.HealthCheckResponse_SERVICE_UNKNOWN {
		t.Fatalf("status = %v, want SERVICE_UNKNOWN", resp.GetStatus())
	}
}

func TestHealth_Check_runtimeService(t *testing.T) {
	db, mock := testSQLxDB(t)
	mock.ExpectExec(`SELECT 1`).WillReturnResult(sqlmock.NewResult(0, 0))

	srv := &healthServer{db: db}
	resp, err := srv.Check(context.Background(), &grpc_health_v1.HealthCheckRequest{Service: healthServiceRuntime})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if resp.GetStatus() != grpc_health_v1.HealthCheckResponse_SERVING {
		t.Fatalf("status = %v, want SERVING", resp.GetStatus())
	}
}

func TestHealth_Watch_unimplemented(t *testing.T) {
	srv := &healthServer{}
	err := srv.Watch(&grpc_health_v1.HealthCheckRequest{}, nil)
	assertGRPCCode(t, err, codes.Unimplemented)
}

func TestHealth_List(t *testing.T) {
	db, mock := testSQLxDB(t)
	mock.ExpectExec(`SELECT 1`).WillReturnResult(sqlmock.NewResult(0, 0))

	srv := &healthServer{db: db}
	resp, err := srv.List(context.Background(), &grpc_health_v1.HealthListRequest{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if resp.GetStatuses()[""] != grpc_health_v1.HealthCheckResponse_SERVING {
		t.Fatalf("overall status = %v, want SERVING", resp.GetStatuses()[""])
	}
}

func testSQLxDB(t *testing.T) (*sqlx.DB, sqlmock.Sqlmock) {
	t.Helper()
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	return sqlx.NewDb(sqlDB, "pgx"), mock
}
