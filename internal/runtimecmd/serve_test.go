package runtimecmd

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/phrony-platform/runtime/internal/common"
	"github.com/phrony-platform/runtime/internal/core"
)

func TestRunServe_success(t *testing.T) {
	restore := stubRuntimeDB(t)
	defer restore()
	t.Setenv("RUNTIME_DATABASE_URL", "postgres://unused")
	t.Setenv("RUNTIME_GRPC_ADDR", "127.0.0.1:0")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- runServeWithDeps(ctx, false, serveDeps{
			loadSettings: common.LoadSettings,
			openDB:       common.OpenDB,
			migrate:      core.Migrate,
			newServer:    core.NewServer,
			notifyContext: func(parent context.Context, _ ...os.Signal) (context.Context, context.CancelFunc) {
				child, stop := context.WithCancel(parent)
				go func() {
					time.Sleep(20 * time.Millisecond)
					stop()
				}()
				return child, func() {}
			},
		})
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("runServe: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for runServe")
	}
}

func TestRunServe_skipMigrate(t *testing.T) {
	restore := stubRuntimeDB(t)
	defer restore()
	t.Setenv("RUNTIME_DATABASE_URL", "postgres://unused")
	t.Setenv("RUNTIME_GRPC_ADDR", "127.0.0.1:0")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := runServeWithDeps(ctx, true, serveDeps{
		loadSettings: common.LoadSettings,
		openDB:       common.OpenDB,
		migrate: func(*sqlx.DB) error {
			t.Fatal("migrate should not run when skipMigrate is true")
			return nil
		},
		newServer: core.NewServer,
		notifyContext: func(ctx context.Context, _ ...os.Signal) (context.Context, context.CancelFunc) {
			return ctx, func() {}
		},
	})
	if err != nil {
		t.Fatalf("runServe: %v", err)
	}
}

func TestRunServe_loadSettingsFailed(t *testing.T) {
	t.Setenv("RUNTIME_DATABASE_URL", "")

	err := runServe(context.Background(), false)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "load settings") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunServe_openDBFailed(t *testing.T) {
	t.Setenv("RUNTIME_DATABASE_URL", "postgres://unused")

	err := runServeWithDeps(context.Background(), true, serveDeps{
		loadSettings: common.LoadSettings,
		openDB: func(common.Settings) (*sqlx.DB, error) {
			return nil, errors.New("open failed")
		},
		migrate:       core.Migrate,
		newServer:     core.NewServer,
		notifyContext: signalNotifyContext,
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "open database") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunServe_migrateFailed(t *testing.T) {
	restore := stubRuntimeDB(t)
	defer restore()
	t.Setenv("RUNTIME_DATABASE_URL", "postgres://unused")

	err := runServeWithDeps(context.Background(), false, serveDeps{
		loadSettings: common.LoadSettings,
		openDB:       common.OpenDB,
		migrate: func(*sqlx.DB) error {
			return errors.New("migrate failed")
		},
		newServer:     core.NewServer,
		notifyContext: signalNotifyContext,
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "migrate:") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunServe_serveFailed(t *testing.T) {
	restore := stubRuntimeDB(t)
	defer restore()
	t.Setenv("RUNTIME_DATABASE_URL", "postgres://unused")
	t.Setenv("RUNTIME_GRPC_ADDR", "127.0.0.1:-1")

	err := runServeWithDeps(context.Background(), true, serveDeps{
		loadSettings:  common.LoadSettings,
		openDB:        common.OpenDB,
		migrate:       core.Migrate,
		newServer:     core.NewServer,
		notifyContext: signalNotifyContext,
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "serve:") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func signalNotifyContext(ctx context.Context, _ ...os.Signal) (context.Context, context.CancelFunc) {
	return ctx, func() {}
}
