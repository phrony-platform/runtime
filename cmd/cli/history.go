package main

import (
	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
	"github.com/phrony-platform/runtime/internal/clierr"
	"github.com/phrony-platform/runtime/internal/cliout"
	"github.com/spf13/cobra"
)

func newHistoryCommand(runtimeAddr *string) *cobra.Command {
	return &cobra.Command{
		Use:   "history AGENT",
		Short: "List deployment history for an agent",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runHistory(cmd, runtimeAddr, args[0])
		},
	}
}

func runHistory(cmd *cobra.Command, runtimeAddr *string, agentName string) error {
	ref, err := parseAgentRef(agentName)
	if err != nil {
		return err
	}

	return withRuntimeClient(cmd, *runtimeAddr, func(rt runtimev1.RuntimeClient) error {
		resp, err := rt.ListDeployments(cmd.Context(), &runtimev1.ListDeploymentsRequest{
			AgentRef: ref,
		})
		if err != nil {
			return clierr.WrapRPC("list deployments", err)
		}

		headers := []string{"VERSION", "ACTION", "ACTOR", "CREATED_AT"}
		rows := make([][]string, 0, len(resp.GetDeployments()))
		for _, d := range resp.GetDeployments() {
			rows = append(rows, []string{
				d.GetVersion(),
				d.GetAction(),
				d.GetActor(),
				d.GetCreatedAt(),
			})
		}
		return cliout.WriteTable(cmd.OutOrStdout(), headers, rows)
	})
}
