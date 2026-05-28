package main

import (
	"fmt"
	"os"

	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
	"github.com/spf13/cobra"
)

func newDeployCommand(runtimeAddr *string) *cobra.Command {
	var manifestPath string

	cmd := &cobra.Command{
		Use:   "deploy",
		Short: "Deploy a manifest to the runtime",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDeploy(cmd, runtimeAddr, manifestPath)
		},
	}
	cmd.Flags().StringVar(&manifestPath, "file", "", "path to manifest file")
	_ = cmd.MarkFlagRequired("file")

	return cmd
}

func runDeploy(cmd *cobra.Command, runtimeAddr *string, manifestPath string) error {
	manifest, err := os.ReadFile(manifestPath)
	if err != nil {
		return fmt.Errorf("read manifest: %w", err)
	}

	clients, err := dialRuntime(cmd.Context(), *runtimeAddr)
	if err != nil {
		return err
	}
	defer clients.Close()

	_, err = clients.runtime.Deploy(cmd.Context(), &runtimev1.DeployRequest{
		Manifest: manifest,
	})
	if err != nil {
		return formatRPCError("deploy", err)
	}
	return nil
}
