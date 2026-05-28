package main

import (
	"fmt"

	grpc_health_v1 "github.com/phrony-platform/runtime/gen/grpc/health/v1"
	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
	"github.com/spf13/cobra"
)

func newStatusCommand(runtimeAddr *string) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show runtime version and health",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runStatus(cmd, runtimeAddr)
		},
	}
}

func runStatus(cmd *cobra.Command, runtimeAddr *string) error {
	clients, err := dialRuntime(cmd.Context(), *runtimeAddr)
	if err != nil {
		return err
	}
	defer clients.Close()

	versionResp, err := clients.runtime.GetVersion(cmd.Context(), &runtimev1.GetVersionRequest{})
	if err != nil {
		return fmt.Errorf("get version: %w", err)
	}

	healthResp, err := clients.health.Check(cmd.Context(), &grpc_health_v1.HealthCheckRequest{})
	if err != nil {
		return fmt.Errorf("health check: %w", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "version: %s\n", versionResp.GetVersion())
	fmt.Fprintf(cmd.OutOrStdout(), "health: %s\n", healthResp.GetStatus().String())
	return nil
}
