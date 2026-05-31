package main

import (
	"encoding/json"
	"fmt"

	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
	"github.com/spf13/cobra"
)

func newSessionCommand(runtimeAddr *string) *cobra.Command {
	var version, input string
	var noTUI bool

	cmd := &cobra.Command{
		Use:   "session AGENT[@VERSION]",
		Short: "Start a session for the active deployed agent",
		Long:  "Start a new session for AGENT (namespace/name). Uses the active deployment; an explicit @version must match the active deployment.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runNewSession(cmd, runtimeAddr, args[0], version, input, noTUI)
		},
	}
	cmd.Flags().StringVarP(&version, "version", "v", "", "active agent version (alternative to AGENT@version)")
	cmd.Flags().StringVar(&input, "input", "", "session input as a JSON object")
	cmd.Flags().BoolVar(&noTUI, "no-tui", false, "disable interactive terminal UI (use plain stream output)")

	return cmd
}

func runNewSession(cmd *cobra.Command, runtimeAddr *string, agentRefArg, version, input string, noTUI bool) error {
	ref, err := parseAgentRef(agentRefArg)
	if err != nil {
		return err
	}
	if ref.GetVersion() == "" && version != "" {
		ref.Version = version
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
