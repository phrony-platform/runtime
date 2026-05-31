package main

import (
	"encoding/json"
	"fmt"
	"strings"

	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
	"github.com/spf13/cobra"
)

func newRunCommand(runtimeAddr *string) *cobra.Command {
	var version, input string
	var noTUI bool

	cmd := &cobra.Command{
		Use:   "run AGENT",
		Short: "Start a session for a deployed agent",
		Long:  "Start a new session for AGENT (namespace/name).",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runNewSession(cmd, runtimeAddr, args[0], version, input, noTUI)
		},
	}
	cmd.Flags().StringVarP(&version, "version", "v", "", "deployed agent version (semver from manifest metadata.version)")
	cmd.Flags().StringVar(&input, "input", "", "session input as a JSON object")
	cmd.Flags().BoolVar(&noTUI, "no-tui", false, "disable interactive terminal UI (use plain stream output)")

	return cmd
}

func runNewSession(cmd *cobra.Command, runtimeAddr *string, agentName, version, input string, noTUI bool) error {
	ref, err := agentRef(agentName, version)
	if err != nil {
		return err
	}

	var inputBytes []byte
	if input != "" {
		if !json.Valid([]byte(input)) {
			return fmt.Errorf("input must be valid JSON")
		}
		inputBytes = []byte(input)
	}

	start := &runtimev1.RunSessionInteractiveStart{
		AgentRef: ref,
		Input:    inputBytes,
	}
	return runInteractiveSessionCLI(cmd, runtimeAddr, start, noTUI)
}

func parseAgentName(agentName string) (namespace, name string, err error) {
	namespace, name, ok := strings.Cut(agentName, "/")
	if !ok || namespace == "" || name == "" {
		return "", "", fmt.Errorf("agent must be namespace/name, got %q", agentName)
	}
	return namespace, name, nil
}
