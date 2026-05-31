package main

import (
	"os"

	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
	"github.com/phrony-platform/runtime/internal/clierr"
	"github.com/spf13/cobra"
)

func runInteractiveSessionCLI(
	cmd *cobra.Command,
	runtimeAddr *string,
	start *runtimev1.RunSessionInteractiveStart,
	noTUI bool,
) error {
	if noTUI {
		_ = os.Setenv("PHRONY_NO_TUI", "1")
	}

	return withRuntimeClient(cmd, *runtimeAddr, func(rt runtimev1.RuntimeClient) error {
		stream, err := rt.RunSessionInteractive(cmd.Context())
		if err != nil {
			return clierr.WrapRPC("run session", err)
		}

		return runInteractiveSession(
			cmd.Context(),
			stream,
			start,
			cmd.InOrStdin(),
			cmd.OutOrStdout(),
			cmd.ErrOrStderr(),
		)
	})
}
