package main

import (
	"fmt"

	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
	"github.com/phrony-platform/runtime/internal/agentref"
	"github.com/phrony-platform/runtime/internal/clierr"
	"github.com/spf13/cobra"
)

func newRollbackCommand(runtimeAddr *string) *cobra.Command {
	var toVersion string
	cmd := &cobra.Command{
		Use:   "rollback AGENT",
		Short: "Roll back the active deployment for an agent",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRollback(cmd, runtimeAddr, args[0], toVersion)
		},
	}
	cmd.Flags().StringVar(&toVersion, "to", "", "target version to activate (default: previous active version)")
	return cmd
}

func runRollback(cmd *cobra.Command, runtimeAddr *string, agentName, toVersion string) error {
	ref, err := parseAgentRef(agentName)
	if err != nil {
		return err
	}

	return withRuntimeClient(cmd, *runtimeAddr, func(rt runtimev1.RuntimeClient) error {
		resp, err := rt.Rollback(cmd.Context(), &runtimev1.RollbackRequest{
			AgentRef:  ref,
			ToVersion: toVersion,
			Actor:     cliActor(),
		})
		if err != nil {
			return clierr.WrapRPC("rollback", err)
		}

		line := fmt.Sprintf("rolled back %s to %s",
			agentref.Format(ref.GetNamespace(), ref.GetName()),
			resp.GetVersion(),
		)
		if prev := resp.GetPreviousVersion(); prev != "" {
			line += fmt.Sprintf(" (was: %s)", prev)
		}
		fmt.Fprintln(cmd.OutOrStdout(), line)
		return nil
	})
}
