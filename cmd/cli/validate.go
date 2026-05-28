package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newValidateCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "validate MANIFEST",
		Short: "Validate an agent manifest locally (no deploy)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runValidate(cmd, args[0])
		},
	}
}

func runValidate(cmd *cobra.Command, manifestPath string) error {
	resolved, err := loadResolvedManifest(manifestPath)
	if err != nil {
		return err
	}

	agent := resolved.Agent
	fmt.Fprintf(cmd.OutOrStdout(), "valid: %s %s\n",
		formatAgentName(agent.Metadata.Namespace, agent.Metadata.Name),
		agent.Metadata.Version,
	)
	return nil
}
