package main

import (
	"os"

	"github.com/phrony-platform/runtime/internal/version"
	"github.com/phrony-platform/runtime/internal/telemetry"
	"github.com/spf13/cobra"
)

// Execute runs the phrony operator CLI using process arguments.
func Execute() error {
	return runRoot(os.Args[1:])
}

func runRoot(args []string) error {
	root := NewRootCommand()
	if args != nil {
		root.SetArgs(args)
	}
	return root.Execute()
}

// NewRootCommand builds the cobra root for the phrony operator CLI.
func NewRootCommand() *cobra.Command {
	var runtimeAddr string

	root := &cobra.Command{
		Use:           "phrony",
		Short:         "Phrony runtime operator CLI",
		Long:          "Operator commands for phrony-runtime over gRPC (not the manifest-focused Node CLI).",
		Version:       version.CLIVersion,
		SilenceErrors: true,
		SilenceUsage:  true,
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			telemetry.Track(telemetry.EventCLICommand)
		},
	}
	root.PersistentFlags().StringVar(&runtimeAddr, "runtime-addr", "", "runtime gRPC address (overrides PHRONY_RUNTIME_ADDR)")

	root.AddCommand(
		newInitCommand(),
		newUpgradeCommand(),
		newTelemetryCommand(),
		newStatusCommand(&runtimeAddr),
		newRollbackCommand(&runtimeAddr),
		newRunCommand(&runtimeAddr),
		newSessionsCommand(&runtimeAddr),
		newApprovalsCommand(&runtimeAddr),
		newAgentsCommand(&runtimeAddr),
		newBundlesCommand(&runtimeAddr),
	)

	return root
}
