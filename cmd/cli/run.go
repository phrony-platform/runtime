package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
	"github.com/phrony-platform/runtime/internal/clierr"
	"github.com/spf13/cobra"
)

func newRunCommand(runtimeAddr *string) *cobra.Command {
	var version, input string
	var noTUI bool

	cmd := &cobra.Command{
		Use:   "run AGENT",
		Short: "Start a session for a deployed agent",
		Long:  "AGENT is namespace/name (for example demo/echo-agent). Uses the latest deployed version unless -v is set.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSession(cmd, runtimeAddr, args[0], version, input, noTUI)
		},
	}
	cmd.Flags().StringVarP(&version, "version", "v", "", "deployed agent version (semver from manifest metadata.version)")
	cmd.Flags().StringVar(&input, "input", "", "session input as a JSON object")
	cmd.Flags().BoolVar(&noTUI, "no-tui", false, "disable interactive terminal UI (use plain stream output)")

	return cmd
}

func runSession(cmd *cobra.Command, runtimeAddr *string, agentName, version, input string, noTUI bool) error {
	if noTUI {
		_ = os.Setenv("PHRONY_NO_TUI", "1")
	}
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

	return withRuntimeClient(cmd, *runtimeAddr, func(rt runtimev1.RuntimeClient) error {
		stream, err := rt.RunSessionInteractive(cmd.Context())
		if err != nil {
			return clierr.WrapRPC("run session", err)
		}

		return runInteractiveSession(
			cmd.Context(),
			stream,
			start,
			cmd.InOrStdin(),
			cmd.OutOrStdout(),
			cmd.ErrOrStderr(),
		)
	})
}

func parseAgentName(agentName string) (namespace, name string, err error) {
	namespace, name, ok := strings.Cut(agentName, "/")
	if !ok || namespace == "" || name == "" {
		return "", "", fmt.Errorf("agent must be namespace/name, got %q", agentName)
	}
	return namespace, name, nil
}
