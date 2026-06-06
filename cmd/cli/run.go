package main

import (
	"encoding/json"
	"fmt"

	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
	"github.com/phrony-platform/runtime/internal/clierr"
	"github.com/spf13/cobra"
)

func newRunCommand(runtimeAddr *string) *cobra.Command {
	var version, input string
	var attach bool
	var envFiles []string

	cmd := &cobra.Command{
		Use:   "run AGENT[@VERSION]",
		Short: "Start a session for the active deployed agent",
		Long: "Start a new session for AGENT (namespace/name). Uses the active deployment; an explicit @version must match the active deployment. " +
			"By default the runtime runs the first turn in the background and the CLI prints the session id and exits. " +
			"Use --attach to start the session in the background and attach a foreground view (Ctrl+C detaches; use sessions cancel to stop).",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAgentSession(cmd, runtimeAddr, args[0], version, input, attach, envFiles)
		},
	}
	cmd.Flags().StringVarP(&version, "version", "v", "", "active agent version (alternative to AGENT@version)")
	cmd.Flags().StringVar(&input, "input", "", "session input as a JSON object")
	cmd.Flags().BoolVarP(&attach, "attach", "a", false, "start in the background and attach an interactive view")
	cmd.Flags().StringArrayVarP(&envFiles, "env-file", "e", nil, "load environment variables from a file before resolving secrets (repeatable; does not override existing env)")

	return cmd
}

func runAgentSession(cmd *cobra.Command, runtimeAddr *string, agentRefArg, version, input string, attach bool, envFiles []string) error {
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

	if attach {
		return runAttachedSession(cmd, runtimeAddr, ref, inputBytes, envFiles)
	}

	return runDetachedSession(cmd, runtimeAddr, ref, inputBytes, envFiles)
}

func runAttachedSession(cmd *cobra.Command, runtimeAddr *string, ref *runtimev1.AgentRef, input []byte, envFiles []string) error {
	return runAttachedSessionWithStart(cmd, runtimeAddr, "run session", func(rt runtimev1.RuntimeClient) (*runtimev1.RunSessionRequest, error) {
		resolvedSecrets, err := prepareRunSecrets(cmd.Context(), rt, ref, envFiles)
		if err != nil {
			return nil, err
		}
		return &runtimev1.RunSessionRequest{
			AgentRef:        ref,
			Input:           input,
			ResolvedSecrets: resolvedSecrets,
		}, nil
	})
}

func runAttachedSessionWithStart(cmd *cobra.Command, runtimeAddr *string, rpcOp string, buildReq func(rt runtimev1.RuntimeClient) (*runtimev1.RunSessionRequest, error)) error {
	var sessionID string
	if err := withRuntimeClient(cmd, *runtimeAddr, func(rt runtimev1.RuntimeClient) error {
		req, err := buildReq(rt)
		if err != nil {
			return err
		}
		resp, err := rt.RunSession(cmd.Context(), req)
		if err != nil {
			return clierr.WrapRPC(rpcOp, err)
		}
		sessionID = resp.GetSessionId()
		return nil
	}); err != nil {
		return err
	}

	start := &runtimev1.RunSessionInteractiveStart{SessionId: sessionID}
	return runInteractiveSessionCLI(cmd, runtimeAddr, start, false)
}

func runDetachedSession(cmd *cobra.Command, runtimeAddr *string, ref *runtimev1.AgentRef, input []byte, envFiles []string) error {
	return withRuntimeClient(cmd, *runtimeAddr, func(rt runtimev1.RuntimeClient) error {
		resolvedSecrets, err := prepareRunSecrets(cmd.Context(), rt, ref, envFiles)
		if err != nil {
			return err
		}
		resp, err := rt.RunSession(cmd.Context(), &runtimev1.RunSessionRequest{
			AgentRef:        ref,
			Input:           input,
			ResolvedSecrets: resolvedSecrets,
		})
		if err != nil {
			return clierr.WrapRPC("run session", err)
		}
		fmt.Fprintf(cmd.OutOrStdout(), "session %s started\n", resp.GetSessionId())
		return nil
	})
}
