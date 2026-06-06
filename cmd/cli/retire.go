package main

import (
	"fmt"

	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
	"github.com/phrony-platform/runtime/internal/agentref"
	"github.com/phrony-platform/runtime/internal/clierr"
	"github.com/spf13/cobra"
)

func newAgentRetireCommand(runtimeAddr *string) *cobra.Command {
	return &cobra.Command{
		Use:   "retire AGENT@VERSION",
		Short: "Retire a published agent version",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRetire(cmd, runtimeAddr, args[0])
		},
	}
}

func runRetire(cmd *cobra.Command, runtimeAddr *string, agentRef string) error {
	ref, err := parseAgentRefVersionRequired(agentRef)
	if err != nil {
		return err
	}

	return withRuntimeClient(cmd, *runtimeAddr, func(rt runtimev1.RuntimeClient) error {
		resp, err := rt.RetireAgentVersion(cmd.Context(), &runtimev1.RetireAgentVersionRequest{
			AgentRef: ref,
		})
		if err != nil {
			return clierr.WrapRPC("retire agent version", err)
		}

		fmt.Fprintf(cmd.OutOrStdout(), "retired %s (id: %s)\n",
			agentref.FormatVersioned(ref.GetNamespace(), ref.GetName(), ref.GetVersion()),
			resp.GetVersionId(),
		)
		return nil
	})
}
