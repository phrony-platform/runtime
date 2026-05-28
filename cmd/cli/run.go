package main

import (
	"fmt"

	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
	"github.com/spf13/cobra"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func newRunCommand(runtimeAddr *string) *cobra.Command {
	return &cobra.Command{
		Use:   "run SESSION_ID",
		Short: "Run an agent session on the runtime",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSession(cmd, runtimeAddr, args[0])
		},
	}
}

func runSession(cmd *cobra.Command, runtimeAddr *string, sessionID string) error {
	clients, err := dialRuntime(cmd.Context(), *runtimeAddr)
	if err != nil {
		return err
	}
	defer clients.Close()

	_, err = clients.runtime.RunSession(cmd.Context(), &runtimev1.RunSessionRequest{
		SessionId: sessionID,
	})
	if err != nil {
		return formatRPCError("run session", err)
	}
	return nil
}

func formatRPCError(action string, err error) error {
	if st, ok := status.FromError(err); ok && st.Code() == codes.Unimplemented {
		return fmt.Errorf("%s: %s (not implemented on this runtime yet)", action, st.Message())
	}
	return fmt.Errorf("%s: %w", action, err)
}
