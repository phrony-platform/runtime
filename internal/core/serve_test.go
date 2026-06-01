package core

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/jmoiron/sqlx"
	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
	"github.com/phrony-platform/runtime/internal/tooldispatch"
	"github.com/phrony-platform/runtime/internal/tooldispatch/testworker"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type brokenListener struct{}

func (brokenListener) Accept() (net.Conn, error) { return nil, errors.New("accept failed") }
func (brokenListener) Close() error              { return nil }
func (brokenListener) Addr() net.Addr {
	return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1}
}

func TestServe_shutdownWithConnectedWorker(t *testing.T) {
	db := testServeDB(t)
	srv, err := NewServer(db)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	var lis net.Listener
	prev := listenTCP
	listenTCP = func(addr string) (net.Listener, error) {
		var listenErr error
		lis, listenErr = net.Listen("tcp", addr)
		return lis, listenErr
	}
	t.Cleanup(func() { listenTCP = prev })

	const serveAddr = "127.0.0.1:0"
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- srv.Serve(ctx, serveAddr)
	}()

	waitForListen(t, done)
	if lis == nil {
		t.Fatal("listener was not created")
	}

	cc, err := grpc.NewClient(lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("grpc.NewClient: %v", err)
	}
	t.Cleanup(func() { _ = cc.Close() })

	workerCtx, workerStop := context.WithCancel(context.Background())
	defer workerStop()
	workerDone := make(chan struct{})
	go func() {
		defer close(workerDone)
		_ = testworker.Run(workerCtx, cc, testworker.Options{
			WorkerID: "shutdown-test-worker",
			Handlers: []tooldispatch.HandlerAdvertisement{
				{Tool: "echo", Version: "default", MaxConcurrency: 1},
			},
			Handler: func(_ context.Context, _ *runtimev1.WorkInvoke) (json.RawMessage, *tooldispatch.ToolError) {
				return json.RawMessage(`{"ok":true}`), nil
			},
		})
	}()
	time.Sleep(50 * time.Millisecond)

	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Serve error = %v, want context.Canceled", err)
		}
	case <-time.After(12 * time.Second):
		t.Fatal("timed out waiting for Serve to stop with connected worker")
	}

	workerStop()
	select {
	case <-workerDone:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for worker to exit after runtime shutdown")
	}
}

func TestServe_gracefulShutdown(t *testing.T) {
	db := testServeDB(t)
	srv, err := NewServer(db)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- srv.Serve(ctx, "127.0.0.1:0")
	}()

	waitForListen(t, done)

	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Serve error = %v, want context.Canceled", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for Serve to stop")
	}
}

func TestServe_grpcServeFailed(t *testing.T) {
	db := testServeDB(t)
	srv, err := NewServer(db)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	prev := listenTCP
	listenTCP = func(string) (net.Listener, error) {
		return brokenListener{}, nil
	}
	t.Cleanup(func() { listenTCP = prev })

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- srv.Serve(ctx, "127.0.0.1:0")
	}()

	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Serve error = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for Serve to stop")
	}
}

func TestServe_listenFailed(t *testing.T) {
	db := testServeDB(t)
	srv, err := NewServer(db)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	prev := listenTCP
	listenTCP = func(addr string) (net.Listener, error) {
		return net.Listen("tcp", addr)
	}
	t.Cleanup(func() { listenTCP = prev })

	err = srv.Serve(context.Background(), "127.0.0.1:-1")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "listen") {
		t.Fatalf("expected listen error, got %v", err)
	}
}

func testServeDB(t *testing.T) *sqlx.DB {
	t.Helper()
	db, mock := testSQLxDB(t)
	mock.ExpectQuery(`FROM sessions`).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "agent_version_id", "input", "status", "output", "error", "history", "created_at", "updated_at",
		}))
	return db
}

func waitForListen(t *testing.T, done <-chan error) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("Serve returned early: %v", err)
			}
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}
}
