package main

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/jmoiron/sqlx"
	grpc_health_v1 "github.com/phrony-platform/runtime/gen/grpc/health/v1"
	"github.com/phrony-platform/runtime/internal/core"
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

	db := sqlx.NewDb(sqlDB, "pgx")
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = lis.Close() })

	srv := core.NewServer(db)
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
