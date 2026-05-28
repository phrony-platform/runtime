package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/phrony-platform/runtime/internal/common"
	"github.com/phrony-platform/runtime/internal/core"
	"github.com/jmoiron/sqlx"
	"github.com/spf13/cobra"
)

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, nil)))

	root := &cobra.Command{
		Use:   "phrony-runtime",
		Short: "Phrony agent runtime daemon",
	}

	var skipMigrate bool
	serveCmd := &cobra.Command{
		Use:   "serve",
		Short: "Run database migrations and start the gRPC server",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runServe(cmd.Context(), skipMigrate)
		},
	}
	serveCmd.Flags().BoolVar(&skipMigrate, "skip-migrate", false, "skip schema migration on startup")

	migrateCmd := &cobra.Command{
		Use:   "migrate",
		Short: "Apply database schema migrations and exit",
		RunE: func(*cobra.Command, []string) error {
			return runMigrate()
		},
	}

	root.AddCommand(serveCmd, migrateCmd)

	if err := root.Execute(); err != nil {
		slog.Error("command failed", "error", err)
		os.Exit(1)
	}
}

func runServe(ctx context.Context, skipMigrate bool) error {
	settings, err := common.LoadSettings()
	if err != nil {
		return fmt.Errorf("load settings: %w", err)
	}

	db, err := common.OpenDB(settings)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer closeDB(db)

	if !skipMigrate {
		if err := core.Migrate(db); err != nil {
			return fmt.Errorf("migrate: %w", err)
		}
		slog.Info("database migrated")
	}

	srv := core.NewServer(db)

	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := srv.Serve(ctx, settings.GRPCAddr); err != nil && ctx.Err() == nil {
		return fmt.Errorf("serve: %w", err)
	}
	return nil
}

func runMigrate() error {
	settings, err := common.LoadSettings()
	if err != nil {
		return fmt.Errorf("load settings: %w", err)
	}

	db, err := common.OpenDB(settings)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer closeDB(db)

	if err := core.Migrate(db); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}

	slog.Info("database migrated")
	return nil
}

func closeDB(db *sqlx.DB) {
	if err := db.Close(); err != nil {
		slog.Warn("close database", "error", err)
	}
}
