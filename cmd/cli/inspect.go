package main

import (
	"fmt"

	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
	"github.com/phrony-platform/runtime/internal/agentref"
	"github.com/phrony-platform/runtime/internal/clierr"
	"github.com/spf13/cobra"
)

func newInspectCommand(runtimeAddr *string) *cobra.Command {
	return &cobra.Command{
		Use:   "inspect AGENT@VERSION",
		Short: "Show metadata for a published agent version",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runInspect(cmd, runtimeAddr, args[0])
		},
	}
}

func runInspect(cmd *cobra.Command, runtimeAddr *string, agentRef string) error {
	ref, err := parseAgentRefVersionRequired(agentRef)
	if err != nil {
		return err
	}

	return withRuntimeClient(cmd, *runtimeAddr, func(rt runtimev1.RuntimeClient) error {
		resp, err := rt.GetAgentVersion(cmd.Context(), &runtimev1.GetAgentVersionRequest{
			AgentRef: ref,
		})
		if err != nil {
			return clierr.WrapRPC("get agent version", err)
		}

		label := agentref.FormatVersioned(ref.GetNamespace(), ref.GetName(), resp.GetVersion())
		fmt.Fprintf(cmd.OutOrStdout(), "agent:        %s\n", label)
		fmt.Fprintf(cmd.OutOrStdout(), "content_hash: %s\n", resp.GetContentHash())
		fmt.Fprintf(cmd.OutOrStdout(), "published_at: %s\n", resp.GetPublishedAt())
		if d := resp.GetDeprecatedAt(); d != "" {
			fmt.Fprintf(cmd.OutOrStdout(), "deprecated_at: %s\n", d)
		}
		if r := resp.GetRetiredAt(); r != "" {
			fmt.Fprintf(cmd.OutOrStdout(), "retired_at: %s\n", r)
		}
		return nil
	})
}
