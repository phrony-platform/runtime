package core

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
)

type brokenListener struct{}

func (brokenListener) Accept() (net.Conn, error) { return nil, errors.New("accept failed") }
func (brokenListener) Close() error              { return nil }
func (brokenListener) Addr() net.Addr {
	return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1}
}

func TestServe_gracefulShutdown(t *testing.T) {
	db := testServeDB(t)
	srv := NewServer(db)

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
	srv := NewServer(db)

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
	srv := NewServer(db)

	prev := listenTCP
	listenTCP = func(addr string) (net.Listener, error) {
		return net.Listen("tcp", addr)
	}
	t.Cleanup(func() { listenTCP = prev })

	err := srv.Serve(context.Background(), "127.0.0.1:-1")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "listen") {
		t.Fatalf("expected listen error, got %v", err)
	}
}

func testServeDB(t *testing.T) *sqlx.DB {
	t.Helper()
	db, _ := testSQLxDB(t)
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
