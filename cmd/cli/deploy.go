package main

import (
	"errors"
	"fmt"
	"os"

	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
	"github.com/phrony-platform/runtime/internal/clierr"
	"github.com/phrony-platform/runtime/internal/manifest"
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
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return fmt.Errorf("read manifest: %w", err)
	}

	agent, err := manifest.Parse(data)
	if err != nil {
		return err
	}
	if err := manifest.Validate(agent); err != nil {
		// Preserve the full multi-field message for clierr.Format (ValidationErrors only unwraps the first field).
		return errors.New(err.Error())
	}

	clients, err := dialRuntime(cmd.Context(), *runtimeAddr)
	if err != nil {
		return err
	}
	defer clients.Close()

	_, err = clients.runtime.Deploy(cmd.Context(), &runtimev1.DeployRequest{
		Manifest: data,
	})
	if err != nil {
		return clierr.WrapRPC("deploy", err)
	}
	return nil
}
