package main

import (
	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
	"github.com/phrony-platform/runtime/internal/clierr"
	"github.com/phrony-platform/runtime/internal/cliout"
	"github.com/spf13/cobra"
)

func newVersionsCommand(runtimeAddr *string) *cobra.Command {
	return &cobra.Command{
		Use:   "versions AGENT",
		Short: "List published versions for an agent",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runVersionsList(cmd, runtimeAddr, args[0])
		},
	}
}

func runVersionsList(cmd *cobra.Command, runtimeAddr *string, agentName string) error {
	ref, err := parseAgentRef(agentName)
	if err != nil {
		return err
	}

	return withRuntimeClient(cmd, *runtimeAddr, func(rt runtimev1.RuntimeClient) error {
		resp, err := rt.ListAgentVersions(cmd.Context(), &runtimev1.ListAgentVersionsRequest{
			AgentRef: ref,
		})
		if err != nil {
			return clierr.WrapRPC("list agent versions", err)
		}

		headers := []string{"VERSION", "ID", "CONTENT_HASH", "PUBLISHED_AT", "STATUS"}
		rows := make([][]string, 0, len(resp.GetVersions()))
		for _, v := range resp.GetVersions() {
			status := "published"
			switch {
			case v.GetRetiredAt() != "":
				status = "retired"
			case v.GetDeprecatedAt() != "":
				status = "deprecated"
			}
			rows = append(rows, []string{
				v.GetVersion(),
				v.GetId(),
				v.GetContentHash(),
				v.GetDeployedAt(),
				status,
			})
		}
		return cliout.WriteTable(cmd.OutOrStdout(), headers, rows)
	})
}
