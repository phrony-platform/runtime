package runtimecmd

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/phrony-platform/runtime/internal/cliout"
	"github.com/phrony-platform/runtime/internal/common"
	"github.com/phrony-platform/runtime/internal/core"
	"github.com/phrony-platform/runtime/internal/telemetry"
)

type serveDeps struct {
	loadSettings  func() (common.Settings, error)
	openDB        func(common.Settings) (*sqlx.DB, error)
	migrate       func(*sqlx.DB) error
	newServer     func(*sqlx.DB) (*core.Server, error)
	notifyContext func(context.Context, ...os.Signal) (context.Context, context.CancelFunc)
}

var defaultServeDeps = serveDeps{
	loadSettings:  common.LoadSettings,
	openDB:        common.OpenDB,
	migrate:       core.Migrate,
	newServer:     core.NewServer,
	notifyContext: signal.NotifyContext,
}

func runServe(ctx context.Context, skipMigrate bool) error {
	return runServeWithDeps(ctx, skipMigrate, defaultServeDeps)
}

func runServeWithDeps(ctx context.Context, skipMigrate bool, deps serveDeps) error {
	settings, err := deps.loadSettings()
	if err != nil {
		return fmt.Errorf("load settings: %w", err)
	}

	db, err := deps.openDB(settings)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer closeDB(db)

	if !skipMigrate {
		if err := deps.migrate(db); err != nil {
			return fmt.Errorf("migrate: %w", err)
		}
		slog.Info("database migrated")
		telemetry.Track(telemetry.EventMigrateRun)
	}

	srv, err := deps.newServer(db)
	if err != nil {
		return fmt.Errorf("init server: %w", err)
	}

	ctx, stop := deps.notifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := cliout.WriteLogo(os.Stderr); err != nil {
		return fmt.Errorf("write logo: %w", err)
	}
	if err := telemetry.WriteNotice(os.Stderr); err != nil {
		return fmt.Errorf("write telemetry notice: %w", err)
	}

	if telemetry.Enabled() {
		telemetry.Track(telemetry.EventDaemonStarted)
		telemetry.StartPeriodicFlush(ctx, 30*time.Second)
		defer telemetry.Flush()
	}

	if err := srv.Serve(ctx, settings.GRPCAddr); err != nil && ctx.Err() == nil {
		return fmt.Errorf("serve: %w", err)
	}
	return nil
}
