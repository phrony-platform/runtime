package main

import (
	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
	"github.com/phrony-platform/runtime/internal/clierr"
	"github.com/phrony-platform/runtime/internal/cliout"
	"github.com/phrony-platform/runtime/internal/model"
	"github.com/spf13/cobra"
)

func newSessionsCommand(runtimeAddr *string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sessions",
		Short: "List and attach to agent sessions",
	}

	ls := &cobra.Command{
		Use:   "ls AGENT",
		Short: "List sessions for a deployed agent",
		Long:  "Lists all sessions for AGENT (namespace/name). Sessions with status awaiting_input can be resumed with phrony sessions attach.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			version, _ := cmd.Flags().GetString("version")
			status, _ := cmd.Flags().GetString("status")
			return runSessionsList(cmd, runtimeAddr, args[0], version, status)
		},
	}
	ls.Flags().StringP("version", "v", "", "deployed agent version (semver from manifest metadata.version)")
	ls.Flags().String("status", "", "filter by session status (pending, running, awaiting_input, completed, failed)")

	attach := &cobra.Command{
		Use:   "attach SESSION_ID",
		Short: "Attach to an existing session",
		Long:  "Connect to an existing session by id. Resume when status is awaiting_input; completed and failed sessions are read-only.",
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

	cmd.AddCommand(ls, attach)
	return cmd
}

func runSessionsList(cmd *cobra.Command, runtimeAddr *string, agentName, version, status string) error {
	ref, err := agentRef(agentName, version)
	if err != nil {
		return err
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
