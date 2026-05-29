package main

import (
	"encoding/json"
	"fmt"
	"strings"

	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
	"github.com/phrony-platform/runtime/internal/clierr"
	"github.com/spf13/cobra"
)

func newRunCommand(runtimeAddr *string) *cobra.Command {
	var version, input string

	cmd := &cobra.Command{
		Use:   "run AGENT",
		Short: "Start a session for a deployed agent",
		Long:  "AGENT is namespace/name (for example demo/echo-agent). Uses the latest deployed version unless -v is set.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSession(cmd, runtimeAddr, args[0], version, input)
		},
	}
	cmd.Flags().StringVarP(&version, "version", "v", "", "deployed agent version (semver from manifest metadata.version)")
	cmd.Flags().StringVar(&input, "input", "", "session input as a JSON object")

	return cmd
}

func runSession(cmd *cobra.Command, runtimeAddr *string, agentName, version, input string) error {
	ref, err := agentRef(agentName, version)
	if err != nil {
		return err
	}

	req := &runtimev1.RunSessionRequest{AgentRef: ref}

	if input != "" {
		if !json.Valid([]byte(input)) {
			return fmt.Errorf("input must be valid JSON")
		}
		req.Input = []byte(input)
	}

	return withRuntimeClient(cmd, *runtimeAddr, func(rt runtimev1.RuntimeClient) error {
		resp, err := rt.RunSession(cmd.Context(), req)
		if err != nil {
			return clierr.WrapRPC("run session", err)
		}

		fmt.Fprintf(cmd.OutOrStdout(), "session %s created (status: %s)\n",
			resp.GetSessionId(), resp.GetStatus())
		return nil
	})
}

func parseAgentName(agentName string) (namespace, name string, err error) {
	namespace, name, ok := strings.Cut(agentName, "/")
	if !ok || namespace == "" || name == "" {
		return "", "", fmt.Errorf("agent must be namespace/name, got %q", agentName)
	}
	return namespace, name, nil
}
