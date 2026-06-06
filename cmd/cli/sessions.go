package main

import (
	"fmt"

	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
	"github.com/phrony-platform/runtime/internal/clierr"
	"github.com/phrony-platform/runtime/internal/cliout"
	"github.com/phrony-platform/runtime/internal/model"
	"github.com/spf13/cobra"
	"google.golang.org/protobuf/encoding/protojson"
)

func newSessionsCommand(runtimeAddr *string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sessions",
		Short: "List, inspect, attach to, complete, and cancel agent sessions",
	}

	ls := &cobra.Command{
		Use:   "ls [AGENT]",
		Short: "List sessions (optionally filter by agent)",
		RunE: func(cmd *cobra.Command, args []string) error {
			var agentName string
			if len(args) > 0 {
				agentName = args[0]
			}
			status, _ := cmd.Flags().GetString("status")
			return runSessionsList(cmd, runtimeAddr, agentName, status)
		},
	}
	ls.Flags().String("status", "", "filter by session status (pending, running, awaiting_input, completed, failed, cancelled)")

	inspect := &cobra.Command{
		Use:   "inspect SESSION_ID",
		Short: "Dump full persisted session state (timeline, usage, invocations, children)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			asJSON, _ := cmd.Flags().GetBool("json")
			return runSessionsInspect(cmd, runtimeAddr, args[0], asJSON)
		},
	}
	inspect.Flags().Bool("json", false, "emit full InspectSession response as JSON")

	cancel := &cobra.Command{
		Use:   "cancel SESSION_ID",
		Short: "Cancel an in-progress session",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSessionsCancel(cmd, runtimeAddr, args[0])
		},
	}

	complete := &cobra.Command{
		Use:   "complete SESSION_ID",
		Short: "Complete an in-progress session, keeping its last output",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSessionsComplete(cmd, runtimeAddr, args[0])
		},
	}

	attach := &cobra.Command{
		Use:   "attach SESSION_ID",
		Short: "Attach to an existing session",
		Long:  "Connect to an existing session by id. Resume when status is awaiting_input; completed, failed, and cancelled sessions are read-only.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			noTUI, _ := cmd.Flags().GetBool("no-tui")
			start := &runtimev1.RunSessionInteractiveStart{
				SessionId: args[0],
			}
			return runInteractiveSessionCLI(cmd, runtimeAddr, start, noTUI)
		},
	}
	attach.Flags().Bool("no-tui", false, "disable interactive terminal UI (use plain stream output)")

	cmd.AddCommand(ls, inspect, cancel, complete, attach)
	return cmd
}

func runSessionsList(cmd *cobra.Command, runtimeAddr *string, agentName, status string) error {
	var ref *runtimev1.AgentRef
	if agentName != "" {
		var err error
		ref, err = parseAgentRef(agentName)
		if err != nil {
			return err
		}
	}

	return withRuntimeClient(cmd, *runtimeAddr, func(rt runtimev1.RuntimeClient) error {
		resp, err := rt.ListSessions(cmd.Context(), &runtimev1.ListSessionsRequest{
			AgentRef: ref,
			Status:   status,
		})
		if err != nil {
			return clierr.WrapRPC("list sessions", err)
		}

		headers := []string{"ID", "STATUS", "UPDATED_AT", "RESUMABLE"}
		rows := make([][]string, 0, len(resp.GetSessions()))
		for _, s := range resp.GetSessions() {
			resumable := ""
			if s.GetStatus() == model.SessionStatusAwaitingInput {
				resumable = "yes"
			}
			rows = append(rows, []string{
				s.GetId(),
				s.GetStatus(),
				s.GetUpdatedAt(),
				resumable,
			})
		}
		return cliout.WriteTable(cmd.OutOrStdout(), headers, rows)
	})
}

func runSessionsInspect(cmd *cobra.Command, runtimeAddr *string, sessionID string, asJSON bool) error {
	return withRuntimeClient(cmd, *runtimeAddr, func(rt runtimev1.RuntimeClient) error {
		resp, err := rt.InspectSession(cmd.Context(), &runtimev1.InspectSessionRequest{
			SessionId: sessionID,
		})
		if err != nil {
			return clierr.WrapRPC("inspect session", err)
		}
		out := cmd.OutOrStdout()
		if asJSON {
			b, err := protojson.MarshalOptions{Multiline: true, Indent: "  "}.Marshal(resp)
			if err != nil {
				return fmt.Errorf("marshal inspect response: %w", err)
			}
			fmt.Fprintln(out, string(b))
			return nil
		}
		return formatSessionInspectHuman(out, resp.GetSession(), 0)
	})
}

func runSessionsCancel(cmd *cobra.Command, runtimeAddr *string, sessionID string) error {
	return withRuntimeClient(cmd, *runtimeAddr, func(rt runtimev1.RuntimeClient) error {
		if _, err := rt.CancelSession(cmd.Context(), &runtimev1.CancelSessionRequest{
			SessionId: sessionID,
		}); err != nil {
			return clierr.WrapRPC("cancel session", err)
		}
		fmt.Fprintf(cmd.OutOrStdout(), "cancelled %s\n", sessionID)
		return nil
	})
}

func runSessionsComplete(cmd *cobra.Command, runtimeAddr *string, sessionID string) error {
	return withRuntimeClient(cmd, *runtimeAddr, func(rt runtimev1.RuntimeClient) error {
		if _, err := rt.CompleteSession(cmd.Context(), &runtimev1.CompleteSessionRequest{
			SessionId: sessionID,
		}); err != nil {
			return clierr.WrapRPC("complete session", err)
		}
		fmt.Fprintf(cmd.OutOrStdout(), "completed %s\n", sessionID)
		return nil
	})
}
