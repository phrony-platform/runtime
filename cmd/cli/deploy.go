package main

import (
	"fmt"
	"strings"

	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
	"github.com/phrony-platform/runtime/internal/agentref"
	"github.com/phrony-platform/runtime/internal/clierr"
	"github.com/spf13/cobra"
)

func newDeployCommand(runtimeAddr *string) *cobra.Command {
	return &cobra.Command{
		Use:   "deploy AGENT@VERSION",
		Short: "Activate a published agent version",
		Long:  "Record a deployment so the given version becomes the active version for sessions in this runtime.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDeployActivate(cmd, runtimeAddr, args[0])
		},
	}
}

func runDeployActivate(cmd *cobra.Command, runtimeAddr *string, agentRef string) error {
	ref, err := parseAgentRefVersionRequired(agentRef)
	if err != nil {
		return err
	}
	if strings.HasPrefix(ref.GetVersion(), "sha256:") {
		return runBundleDeploy(cmd, runtimeAddr, agentRef)
	}

	return withRuntimeClient(cmd, *runtimeAddr, func(rt runtimev1.RuntimeClient) error {
		resp, err := rt.Deploy(cmd.Context(), &runtimev1.DeployRequest{
			AgentRef: ref,
			Actor:    cliActor(),
		})
		if err != nil {
			return clierr.WrapRPC("deploy", err)
		}

		line := fmt.Sprintf("deployed %s@%s",
			agentref.Format(resp.GetNamespace(), resp.GetName()),
			resp.GetVersion(),
		)
		if prev := resp.GetPreviousVersion(); prev != "" {
			line += fmt.Sprintf(" (previous: %s)", prev)
		}
		if at := resp.GetDeployedAt(); at != "" {
			line += fmt.Sprintf(" at %s", at)
		}
		fmt.Fprintln(cmd.OutOrStdout(), line)
		return nil
	})
}
