package main

import (
	"fmt"

	"github.com/phrony-platform/runtime/internal/agentref"
	"github.com/phrony-platform/runtime/internal/manifest"
	"github.com/spf13/cobra"
)

func newAgentValidateCommand(runtimeAddr *string) *cobra.Command {
	return &cobra.Command{
		Use:   "validate MANIFEST",
		Short: "Validate an agent manifest locally (no publish)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAgentValidate(cmd, runtimeAddr, args[0])
		},
	}
}

func runAgentValidate(cmd *cobra.Command, runtimeAddr *string, manifestPath string) error {
	kind, err := readManifestKind(manifestPath)
	if err != nil {
		return err
	}
	if kind == manifest.KindBundle {
		return fmt.Errorf("bundle manifest: use phrony bundles validate instead")
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
