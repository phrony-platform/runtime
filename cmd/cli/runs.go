package main

import (
	"fmt"

	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
	"github.com/phrony-platform/runtime/internal/clierr"
	"github.com/phrony-platform/runtime/internal/cliout"
	"github.com/phrony-platform/runtime/internal/model"
	"github.com/spf13/cobra"
)

func newRunsCommand(runtimeAddr *string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "runs",
		Short: "List, inspect, attach to, and cancel agent runs",
	}

	ls := &cobra.Command{
		Use:   "ls [AGENT]",
		Short: "List runs (optionally filter by agent)",
		RunE: func(cmd *cobra.Command, args []string) error {
			var agentName string
			if len(args) > 0 {
				agentName = args[0]
			}
			status, _ := cmd.Flags().GetString("status")
			return runRunsList(cmd, runtimeAddr, agentName, status)
		},
	}
	ls.Flags().String("status", "", "filter by run status (pending, running, awaiting_input, completed, failed, cancelled)")

	inspect := &cobra.Command{
		Use:   "inspect RUN_ID",
		Short: "Show metadata for a run",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRunsInspect(cmd, runtimeAddr, args[0])
		},
	}

	cancel := &cobra.Command{
		Use:   "cancel RUN_ID",
		Short: "Cancel an in-progress run",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRunsCancel(cmd, runtimeAddr, args[0])
		},
	}

	attach := &cobra.Command{
		Use:   "attach RUN_ID",
		Short: "Attach to an existing run",
		Long:  "Connect to an existing run by id. Resume when status is awaiting_input; completed and failed runs are read-only.",
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

	cmd.AddCommand(ls, inspect, cancel, attach)
	return cmd
}

func runRunsList(cmd *cobra.Command, runtimeAddr *string, agentName, status string) error {
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
			return clierr.WrapRPC("list runs", err)
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

func runRunsInspect(cmd *cobra.Command, runtimeAddr *string, runID string) error {
	return withRuntimeClient(cmd, *runtimeAddr, func(rt runtimev1.RuntimeClient) error {
		resp, err := rt.ListSessions(cmd.Context(), &runtimev1.ListSessionsRequest{})
		if err != nil {
			return clierr.WrapRPC("list runs", err)
		}
		for _, s := range resp.GetSessions() {
			if s.GetId() != runID {
				continue
			}
			fmt.Fprintf(cmd.OutOrStdout(), "id:               %s\n", s.GetId())
			fmt.Fprintf(cmd.OutOrStdout(), "status:           %s\n", s.GetStatus())
			fmt.Fprintf(cmd.OutOrStdout(), "agent_version_id: %s\n", s.GetAgentVersionId())
			fmt.Fprintf(cmd.OutOrStdout(), "created_at:       %s\n", s.GetCreatedAt())
			fmt.Fprintf(cmd.OutOrStdout(), "updated_at:       %s\n", s.GetUpdatedAt())
			return nil
		}
		return fmt.Errorf("run %s not found", runID)
	})
}

func runRunsCancel(cmd *cobra.Command, runtimeAddr *string, runID string) error {
	return withRuntimeClient(cmd, *runtimeAddr, func(rt runtimev1.RuntimeClient) error {
		if _, err := rt.CancelSession(cmd.Context(), &runtimev1.CancelSessionRequest{
			SessionId: runID,
		}); err != nil {
			return clierr.WrapRPC("cancel run", err)
		}
		fmt.Fprintf(cmd.OutOrStdout(), "cancelled %s\n", runID)
		return nil
	})
}
