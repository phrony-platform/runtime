package runtimecmd

import (
	"log/slog"
	"os"

	"github.com/spf13/cobra"
)

// Run is the daemon entrypoint for cmd/phrony-runtime.
func Run(args []string) int {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, nil)))
	if err := runRoot(args); err != nil {
		slog.Error("command failed", "error", err)
		return 1
	}
	return 0
}

func runRoot(args []string) error {
	root := &cobra.Command{
		Use:   "phrony-runtime",
		Short: "Phrony agent runtime daemon",
	}

	var skipMigrate bool
	serveCmd := &cobra.Command{
		Use:   "serve",
		Short: "Run database migrations and start the gRPC server",
		RunE: func(cmd *cobra.Command, _ []string) error {
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
	if args != nil {
		root.SetArgs(args)
	}
	return root.Execute()
}
