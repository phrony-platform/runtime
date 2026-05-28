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
	return &cobra.Command{
		Use:   "deploy MANIFEST",
		Short: "Deploy an agent manifest to the runtime",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDeploy(cmd, runtimeAddr, args[0])
		},
	}
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

	resolved, err := manifest.ResolveBundle(manifestPath, agent)
	if err != nil {
		return err
	}
	manifestJSON, err := resolved.JSON()
	if err != nil {
		return fmt.Errorf("encode resolved manifest: %w", err)
	}

	clients, err := dialRuntime(cmd.Context(), *runtimeAddr)
	if err != nil {
		return err
	}
	defer clients.Close()

	resp, err := clients.runtime.Deploy(cmd.Context(), &runtimev1.DeployRequest{
		Manifest: manifestJSON,
	})
	if err != nil {
		return clierr.WrapRPC("deploy", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "%s %s\n",
		formatAgentName(resp.GetNamespace(), resp.GetName()),
		resp.GetVersion(),
	)
	return nil
}

func formatAgentName(namespace, name string) string {
	if namespace == "" {
		return name
	}
	return namespace + "/" + name
}
