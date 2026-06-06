package main

import (
	"fmt"

	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
	"github.com/phrony-platform/runtime/internal/clierr"
	"github.com/phrony-platform/runtime/internal/cliout"
	"github.com/spf13/cobra"
)

func newAgentsCommand(runtimeAddr *string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "agents",
		Short: "Manage agent manifests and registry",
	}

	cmd.AddCommand(
		newAgentsLsCommand(runtimeAddr),
		newAgentsArchiveCommand(runtimeAddr),
		newAgentValidateCommand(runtimeAddr),
		newAgentDiffCommand(runtimeAddr),
		newAgentPublishCommand(runtimeAddr),
		newAgentVersionsCommand(runtimeAddr),
		newAgentInspectCommand(runtimeAddr),
		newAgentDeprecateCommand(runtimeAddr),
		newAgentRetireCommand(runtimeAddr),
		newAgentDeployCommand(runtimeAddr),
		newAgentActiveCommand(runtimeAddr),
		newAgentHistoryCommand(runtimeAddr),
	)
	return cmd
}

func newAgentsLsCommand(runtimeAddr *string) *cobra.Command {
	var namespace string

	cmd := &cobra.Command{
		Use:   "ls",
		Short: "List agents",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runAgentsList(cmd, runtimeAddr, namespace)
		},
	}
	cmd.Flags().StringVarP(&namespace, "namespace", "n", "", "filter by namespace")
	return cmd
}

func newAgentsArchiveCommand(runtimeAddr *string) *cobra.Command {
	return &cobra.Command{
		Use:   "archive AGENT",
		Short: "Archive an agent and deprecate all of its versions",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAgentArchive(cmd, runtimeAddr, args[0])
		},
	}
}

func runAgentsList(cmd *cobra.Command, runtimeAddr *string, namespace string) error {
	return withRuntimeClient(cmd, *runtimeAddr, func(rt runtimev1.RuntimeClient) error {
		resp, err := rt.ListAgents(cmd.Context(), &runtimev1.ListAgentsRequest{
			Namespace: namespace,
		})
		if err != nil {
			return clierr.WrapRPC("list agents", err)
		}

		headers := []string{"ID", "NAMESPACE", "NAME", "OWNER", "STATUS"}
		rows := make([][]string, 0, len(resp.GetAgents()))
		for _, a := range resp.GetAgents() {
			status := "active"
			if a.GetArchivedAt() != "" {
				status = "archived"
			}
			rows = append(rows, []string{
				a.GetId(),
				a.GetNamespace(),
				a.GetName(),
				a.GetOwner(),
				status,
			})
		}
		return cliout.WriteTable(cmd.OutOrStdout(), headers, rows)
	})
}

func runAgentArchive(cmd *cobra.Command, runtimeAddr *string, agentName string) error {
	ref, err := parseAgentRef(agentName)
	if err != nil {
		return err
	}

	return withRuntimeClient(cmd, *runtimeAddr, func(rt runtimev1.RuntimeClient) error {
		if _, err := rt.ArchiveAgent(cmd.Context(), &runtimev1.ArchiveAgentRequest{
			AgentRef: ref,
		}); err != nil {
			return clierr.WrapRPC("archive agent", err)
		}

		fmt.Fprintf(cmd.OutOrStdout(), "archived agent %s (all versions deprecated)\n", agentName)
		return nil
	})
}
