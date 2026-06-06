package main

import (
	"fmt"

	"github.com/phrony-platform/runtime/internal/agentref"
	"github.com/phrony-platform/runtime/internal/manifest"
	"github.com/spf13/cobra"
)

func newValidateCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "validate MANIFEST",
		Short: "Validate an agent manifest locally (no publish)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runValidate(cmd, args[0])
		},
	}
}

func runValidate(cmd *cobra.Command, manifestPath string) error {
	isBundle, err := isBundleManifestPath(manifestPath)
	if err != nil {
		return err
	}
	if isBundle {
		return runBundleValidate(cmd, manifestPath)
	}

	resolved, err := loadResolvedManifest(manifestPath)
	if err != nil {
		return err
	}

	agent := resolved.Agent
	for _, msg := range manifest.UnsetSecretEnvVars(agent) {
		fmt.Fprintf(cmd.ErrOrStderr(), "warning: %s is not set in the local environment\n", msg)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "valid: %s %s\n",
		agentref.Format(agent.Metadata.Namespace, agent.Metadata.Name),
		agent.Metadata.Version,
	)
	return nil
}
