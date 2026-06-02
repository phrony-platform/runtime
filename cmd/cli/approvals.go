package main

import (
	"encoding/json"
	"fmt"
	"os"

	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
	"github.com/phrony-platform/runtime/internal/clierr"
	"github.com/phrony-platform/runtime/internal/cliout"
	"github.com/spf13/cobra"
)

func newApprovalsCommand(runtimeAddr *string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "approvals",
		Short: "List, inspect, and decide pending tool approvals",
	}

	cmd.AddCommand(
		newApprovalsListCommand(runtimeAddr),
		newApprovalsShowCommand(runtimeAddr),
		newApprovalsApproveCommand(runtimeAddr),
		newApprovalsRejectCommand(runtimeAddr),
	)
	return cmd
}

func newApprovalsListCommand(runtimeAddr *string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List approvals",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runApprovalsList(cmd, runtimeAddr)
		},
	}
	cmd.Flags().String("status", "pending", "filter by approval status")
	cmd.Flags().String("route", "", "filter by route")
	cmd.Flags().String("session-id", "", "filter by session id")
	cmd.Flags().String("agent", "", "filter by agent namespace/name")
	return cmd
}

func newApprovalsShowCommand(runtimeAddr *string) *cobra.Command {
	return &cobra.Command{
		Use:   "show APPROVAL_ID",
		Short: "Show one approval with full context",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runApprovalsShow(cmd, runtimeAddr, args[0])
		},
	}
}

func newApprovalsApproveCommand(runtimeAddr *string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "approve APPROVAL_ID",
		Short: "Approve a pending approval",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runApprovalsDecide(cmd, runtimeAddr, args[0], runtimev1.ApprovalDecision_APPROVAL_DECISION_APPROVE)
		},
	}
	cmd.Flags().String("comment", "", "optional operator comment")
	cmd.Flags().String("args", "", "path to JSON file with edited tool args")
	cmd.Flags().Bool("comprehension", false, "acknowledge comprehension gate")
	return cmd
}

func newApprovalsRejectCommand(runtimeAddr *string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "reject APPROVAL_ID",
		Short: "Reject a pending approval",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runApprovalsDecide(cmd, runtimeAddr, args[0], runtimev1.ApprovalDecision_APPROVAL_DECISION_REJECT)
		},
	}
	cmd.Flags().String("comment", "", "optional operator comment")
	return cmd
}

func runApprovalsList(cmd *cobra.Command, runtimeAddr *string) error {
	status, _ := cmd.Flags().GetString("status")
	route, _ := cmd.Flags().GetString("route")
	sessionID, _ := cmd.Flags().GetString("session-id")
	agent, _ := cmd.Flags().GetString("agent")

	var ns, name string
	if agent != "" {
		ref, err := parseAgentRef(agent)
		if err != nil {
			return err
		}
		ns, name = ref.GetNamespace(), ref.GetName()
	}

	return withRuntimeClient(cmd, *runtimeAddr, func(rt runtimev1.RuntimeClient) error {
		resp, err := rt.ListApprovals(cmd.Context(), &runtimev1.ListApprovalsRequest{
			Status:         status,
			Route:          route,
			SessionId:      sessionID,
			AgentNamespace: ns,
			AgentName:      name,
		})
		if err != nil {
			return clierr.WrapRPC("list approvals", err)
		}
		headers := []string{"ID", "STATUS", "SESSION_ID", "TOOL", "ROUTE", "RECEIVED", "EXPIRES_AT"}
		rows := make([][]string, 0, len(resp.GetApprovals()))
		for _, a := range resp.GetApprovals() {
			received := fmt.Sprintf("%d/%d", a.GetApprovalsReceived(), a.GetApprovalsRequired())
			rows = append(rows, []string{
				a.GetId(),
				a.GetStatus(),
				a.GetSessionId(),
				a.GetTool(),
				a.GetRoute(),
				received,
				a.GetExpiresAt(),
			})
		}
		return cliout.WriteTable(cmd.OutOrStdout(), headers, rows)
	})
}

func runApprovalsShow(cmd *cobra.Command, runtimeAddr *string, approvalID string) error {
	return withRuntimeClient(cmd, *runtimeAddr, func(rt runtimev1.RuntimeClient) error {
		a, err := rt.GetApproval(cmd.Context(), &runtimev1.GetApprovalRequest{ApprovalId: approvalID})
		if err != nil {
			return clierr.WrapRPC("get approval", err)
		}
		out := cmd.OutOrStdout()
		fmt.Fprintf(out, "id: %s\n", a.GetId())
		fmt.Fprintf(out, "status: %s\n", a.GetStatus())
		fmt.Fprintf(out, "session_id: %s\n", a.GetSessionId())
		fmt.Fprintf(out, "call_id: %s\n", a.GetCallId())
		fmt.Fprintf(out, "tool: %s@%s\n", a.GetTool(), a.GetVersion())
		fmt.Fprintf(out, "route: %s\n", a.GetRoute())
		fmt.Fprintf(out, "reason: %s\n", a.GetReason())
		fmt.Fprintf(out, "policy: %s\n", a.GetPolicyName())
		fmt.Fprintf(out, "authority_ref: %s\n", a.GetAuthorityRef())
		fmt.Fprintf(out, "approvals: %d/%d\n", a.GetApprovalsReceived(), a.GetApprovalsRequired())
		fmt.Fprintf(out, "comprehension_required: %v\n", a.GetComprehensionRequired())
		fmt.Fprintf(out, "on_reject: %s\n", a.GetOnReject())
		fmt.Fprintf(out, "expires_at: %s\n", a.GetExpiresAt())
		if len(a.GetArgs()) > 0 {
			fmt.Fprintf(out, "args: %s\n", string(a.GetArgs()))
		}
		for _, v := range a.GetVotes() {
			fmt.Fprintf(out, "vote: %s %s %q\n", v.GetDecidedBy(), v.GetDecision(), v.GetComment())
		}
		return nil
	})
}

func runApprovalsDecide(cmd *cobra.Command, runtimeAddr *string, approvalID string, decision runtimev1.ApprovalDecision) error {
	comment, _ := cmd.Flags().GetString("comment")
	var args []byte
	if path, _ := cmd.Flags().GetString("args"); path != "" {
		b, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read args file: %w", err)
		}
		if !json.Valid(b) {
			return fmt.Errorf("args file must contain JSON")
		}
		args = b
	}
	comprehension, _ := cmd.Flags().GetBool("comprehension")

	return withRuntimeClient(cmd, *runtimeAddr, func(rt runtimev1.RuntimeClient) error {
		resp, err := rt.DecideApproval(cmd.Context(), &runtimev1.DecideApprovalRequest{
			ApprovalId:                approvalID,
			Decision:                  decision,
			Comment:                   comment,
			Args:                      args,
			ComprehensionAcknowledged: comprehension,
			Actor:                     cliActor(),
		})
		if err != nil {
			return clierr.WrapRPC("decide approval", err)
		}
		fmt.Fprintf(cmd.OutOrStdout(), "status: %s\n", resp.GetStatus())
		fmt.Fprintf(cmd.OutOrStdout(), "approvals_received: %d\n", resp.GetApprovalsReceived())
		if s := resp.GetSessionStatus(); s != "" {
			fmt.Fprintf(cmd.OutOrStdout(), "session_status: %s\n", s)
		}
		return nil
	})
}
