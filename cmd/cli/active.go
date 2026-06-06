package main

import (
	"fmt"

	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
	"github.com/phrony-platform/runtime/internal/agentref"
	"github.com/phrony-platform/runtime/internal/clierr"
	"github.com/spf13/cobra"
)

func newAgentActiveCommand(runtimeAddr *string) *cobra.Command {
	return &cobra.Command{
		Use:   "active AGENT",
		Short: "Show the active deployed version for an agent",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runActive(cmd, runtimeAddr, args[0])
		},
	}
}

func runActive(cmd *cobra.Command, runtimeAddr *string, agentName string) error {
	ref, err := parseAgentRef(agentName)
	if err != nil {
		return err
	}

	return withRuntimeClient(cmd, *runtimeAddr, func(rt runtimev1.RuntimeClient) error {
		resp, err := rt.GetActiveVersion(cmd.Context(), &runtimev1.GetActiveVersionRequest{
			AgentRef: ref,
		})
		if err != nil {
			return clierr.WrapRPC("get active version", err)
		}

		fmt.Fprintf(cmd.OutOrStdout(), "%s@%s",
			agentref.Format(ref.GetNamespace(), ref.GetName()),
			resp.GetVersion(),
		)
		if at := resp.GetDeployedAt(); at != "" {
			fmt.Fprintf(cmd.OutOrStdout(), " deployed %s", at)
		}
		if actor := resp.GetActor(); actor != "" {
			fmt.Fprintf(cmd.OutOrStdout(), " by %s", actor)
		}
		fmt.Fprintln(cmd.OutOrStdout())
		return nil
	})
}
