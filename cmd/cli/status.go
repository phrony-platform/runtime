package main

import (
	grpc_health_v1 "github.com/phrony-platform/runtime/gen/grpc/health/v1"
	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
	"github.com/phrony-platform/runtime/internal/clierr"
	"github.com/phrony-platform/runtime/internal/cliout"
	"github.com/phrony-platform/runtime/internal/common"
	"github.com/phrony-platform/runtime/internal/version"
	"github.com/spf13/cobra"
)

func newStatusCommand(runtimeAddr *string) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show CLI and runtime status",
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
		return clierr.WrapRPC("get version", err)
	}

	healthResp, err := clients.health.Check(cmd.Context(), &grpc_health_v1.HealthCheckRequest{})
	if err != nil {
		return clierr.WrapRPC("health check", err)
	}

	return cliout.WriteStatus(cmd.OutOrStdout(), cliout.StatusPanel{
		RuntimeAddr:    common.ResolveRuntimeAddr(*runtimeAddr),
		CLIVersion:     version.CLIVersion,
		RuntimeVersion: versionResp.GetVersion(),
		SchemaVersion:  versionResp.GetSchemaVersion(),
		Health:         healthResp.GetStatus().String(),
	})
}
