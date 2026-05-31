package main

import (
	"fmt"

	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
	"github.com/phrony-platform/runtime/internal/agentref"
	"github.com/phrony-platform/runtime/internal/clierr"
	"github.com/phrony-platform/runtime/internal/cliout"
	"github.com/spf13/cobra"
)

func newAgentsCommand(runtimeAddr *string) *cobra.Command {
	var namespace string

	cmd := &cobra.Command{
		Use:   "agents",
		Short: "List and manage deployed agents",
	}

	ls := &cobra.Command{
		Use:   "ls",
		Short: "List deployed agents",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runAgentsList(cmd, runtimeAddr, namespace)
		},
	}
	ls.Flags().StringVarP(&namespace, "namespace", "n", "", "filter by namespace")

	versions := &cobra.Command{
		Use:   "versions",
		Short: "List versions for a deployed agent",
	}
	versionsLs := &cobra.Command{
		Use:   "ls AGENT",
		Short: "List versions for AGENT (namespace/name)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAgentVersionsList(cmd, runtimeAddr, args[0])
		},
	}
	versions.AddCommand(versionsLs)

	deprecate := &cobra.Command{
		Use:   "deprecate AGENT",
		Short: "Mark an agent version as not runnable",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			version, _ := cmd.Flags().GetString("version")
			return runAgentDeprecate(cmd, runtimeAddr, args[0], version)
		},
	}
	deprecate.Flags().StringP("version", "v", "", "semver version label to deprecate")
	_ = deprecate.MarkFlagRequired("version")

	archive := &cobra.Command{
		Use:   "archive AGENT",
		Short: "Archive an agent and deprecate all of its versions",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAgentArchive(cmd, runtimeAddr, args[0])
		},
	}

	cmd.AddCommand(ls, versions, deprecate, archive)
	return cmd
}

func agentRef(agentName, version string) (*runtimev1.AgentRef, error) {
	namespace, name, err := agentref.Parse(agentName)
	if err != nil {
		return nil, err
	}
	return &runtimev1.AgentRef{
		Namespace: namespace,
		Name:      name,
		Version:   version,
	}, nil
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

func runAgentVersionsList(cmd *cobra.Command, runtimeAddr *string, agentName string) error {
	ref, err := agentRef(agentName, "")
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

		headers := []string{"VERSION", "ID", "CONTENT_HASH", "DEPLOYED_AT", "STATUS"}
		rows := make([][]string, 0, len(resp.GetVersions()))
		for _, v := range resp.GetVersions() {
			status := "runnable"
			if v.GetDeprecatedAt() != "" {
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

func runAgentDeprecate(cmd *cobra.Command, runtimeAddr *string, agentName, version string) error {
	ref, err := agentRef(agentName, version)
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

		fmt.Fprintf(cmd.OutOrStdout(), "deprecated %s version %s (id: %s)\n",
			agentName, version, resp.GetVersionId())
		return nil
	})
}

func runAgentArchive(cmd *cobra.Command, runtimeAddr *string, agentName string) error {
	ref, err := agentRef(agentName, "")
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
