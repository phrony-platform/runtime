package main

import (
	"fmt"

	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
	"github.com/phrony-platform/runtime/internal/agentref"
	"github.com/phrony-platform/runtime/internal/clierr"
	"github.com/spf13/cobra"
)

func newAgentDeprecateCommand(runtimeAddr *string) *cobra.Command {
	return &cobra.Command{
		Use:   "deprecate AGENT@VERSION",
		Short: "Mark a published agent version as not runnable",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDeprecate(cmd, runtimeAddr, args[0])
		},
	}
}

func runDeprecate(cmd *cobra.Command, runtimeAddr *string, agentRef string) error {
	ref, err := parseAgentRefVersionRequired(agentRef)
	if err != nil {
		return err
	}

	return withRuntimeClient(cmd, *runtimeAddr, func(rt runtimev1.RuntimeClient) error {
		resp, err := rt.DeprecateAgentVersion(cmd.Context(), &runtimev1.DeprecateAgentVersionRequest{
			AgentRef: ref,
		})
		if err != nil {
			return clierr.WrapRPC("deprecate agent version", err)
		}

		fmt.Fprintf(cmd.OutOrStdout(), "deprecated %s (id: %s)\n",
			agentref.FormatVersioned(ref.GetNamespace(), ref.GetName(), ref.GetVersion()),
			resp.GetVersionId(),
		)
		return nil
	})
}
