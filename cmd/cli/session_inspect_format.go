package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
	"github.com/phrony-platform/runtime/internal/cliout"
)

func formatSessionInspectHuman(w io.Writer, sess *runtimev1.SessionInspect, mergedTimeline []*runtimev1.InspectTimelineEntry, depth int) error {
	indent := strings.Repeat("  ", depth)
	header := fmt.Sprintf("%s=== Session %s ===", indent, sess.GetId())
	if depth > 0 {
		header = fmt.Sprintf("%s--- Child session %s (depth %d) ---", indent, sess.GetId(), sess.GetDepth())
	}
	fmt.Fprintln(w, header)

	agent := sess.GetAgent()
	agentRef := sess.GetAgentVersionId()
	if agent != nil && agent.GetNamespace() != "" && agent.GetName() != "" {
		agentRef = fmt.Sprintf("%s/%s@%s", agent.GetNamespace(), agent.GetName(), agent.GetVersion())
	}
	fmt.Fprintf(w, "%sid:               %s\n", indent, sess.GetId())
	fmt.Fprintf(w, "%sstatus:           %s\n", indent, sess.GetStatus())
	fmt.Fprintf(w, "%sagent:            %s\n", indent, agentRef)
	if agent != nil {
		if agent.GetModelProvider() != "" || agent.GetModelName() != "" {
			fmt.Fprintf(w, "%smodel:            %s/%s\n", indent, agent.GetModelProvider(), agent.GetModelName())
		}
		if agent.GetMaxTokensPerRun() > 0 {
			fmt.Fprintf(w, "%smax_tokens/run:   %d\n", indent, agent.GetMaxTokensPerRun())
		}
		if agent.GetMaxWallClockSeconds() > 0 {
			fmt.Fprintf(w, "%smax_wall_clock:   %ds\n", indent, agent.GetMaxWallClockSeconds())
		}
	}
	fmt.Fprintf(w, "%sdepth:            %d\n", indent, sess.GetDepth())
	if p := sess.GetParentSessionId(); p != "" {
		fmt.Fprintf(w, "%sparent:           %s\n", indent, p)
	}
	if b := sess.GetBundleVersionId(); b != "" {
		fmt.Fprintf(w, "%sbundle_version:   %s\n", indent, b)
	}
	if sess.GetSessionStartedAtUnixMs() > 0 {
		elapsed := "running"
		if end := sess.GetSessionEndedAtUnixMs(); end > 0 {
			elapsed = fmt.Sprintf("%dms", end-sess.GetSessionStartedAtUnixMs())
		}
		fmt.Fprintf(w, "%ssession_elapsed:  %s\n", indent, elapsed)
	}
	if out := sess.GetOutput(); out != nil {
		if u := out.GetSessionUsage(); u != nil {
			fmt.Fprintf(w, "%ssession_tokens:   in=%d out=%d total=%d\n", indent, u.GetInputTokens(), u.GetOutputTokens(), u.GetTotalTokens())
		}
	}
	fmt.Fprintf(w, "%screated_at:       %s\n", indent, sess.GetCreatedAt())
	fmt.Fprintf(w, "%supdated_at:       %s\n", indent, sess.GetUpdatedAt())

	if len(sess.GetInput()) > 0 {
		fmt.Fprintf(w, "\n%sinput:\n", indent)
		if err := writePrettyJSON(w, indent+"  ", sess.GetInput()); err != nil {
			fmt.Fprintf(w, "%s  %s\n", indent, string(sess.GetInput()))
		}
	}

	if len(sess.GetOutputRaw()) > 0 {
		fmt.Fprintf(w, "\n%soutput:\n", indent)
		if err := writePrettyJSON(w, indent+"  ", sess.GetOutputRaw()); err != nil {
			fmt.Fprintf(w, "%s  %s\n", indent, string(sess.GetOutputRaw()))
		}
	}
	if err := sess.GetError(); err != "" {
		fmt.Fprintf(w, "\n%serror: %s\n", indent, err)
	}

	if len(sess.GetHistory()) > 0 {
		fmt.Fprintf(w, "\n%shistory:\n", indent)
		for i, msg := range sess.GetHistory() {
			fmt.Fprintf(w, "%s  [%d] %s\n", indent, i+1, msg.GetRole())
			fmt.Fprintf(w, "%s      %s\n", indent, msg.GetContent())
			if msg.GetRole() == "assistant" {
				if msg.GetStopReason() != "" {
					fmt.Fprintf(w, "%s      stop_reason: %s\n", indent, msg.GetStopReason())
				}
				if u := msg.GetTurnUsage(); u != nil {
					fmt.Fprintf(w, "%s      turn_usage: in=%d out=%d total=%d\n", indent, u.GetInputTokens(), u.GetOutputTokens(), u.GetTotalTokens())
				}
				if msg.GetTurnDurationMs() > 0 {
					fmt.Fprintf(w, "%s      turn_duration_ms: %d\n", indent, msg.GetTurnDurationMs())
				}
			}
		}
	}

	if depth == 0 && len(mergedTimeline) > 0 {
		fmt.Fprintf(w, "\n%smerged timeline:\n", indent)
		formatInspectTimeline(w, mergedTimeline)
	}

	if len(sess.GetTimeline()) > 0 {
		fmt.Fprintf(w, "\n%stimeline:\n", indent)
		formatInspectTimeline(w, sess.GetTimeline())
	}

	if len(sess.GetInvocations()) > 0 {
		fmt.Fprintf(w, "\n%sinvocations:\n", indent)
		headers := []string{"CALL_ID", "TURN", "TOOL", "STATUS", "QUEUE_MS", "EXEC_MS", "TOTAL_MS", "WORKER"}
		rows := make([][]string, 0, len(sess.GetInvocations()))
		for _, inv := range sess.GetInvocations() {
			rows = append(rows, []string{
				inv.GetCallId(),
				fmt.Sprintf("%d", inv.GetTurn()),
				fmt.Sprintf("%s@%s", inv.GetTool(), inv.GetVersion()),
				inv.GetStatus(),
				fmt.Sprintf("%d", inv.GetQueueDelayMs()),
				fmt.Sprintf("%d", inv.GetExecutionDurationMs()),
				fmt.Sprintf("%d", inv.GetTotalDurationMs()),
				inv.GetWorkerIdentity(),
			})
		}
		if err := cliout.WriteTable(w, headers, rows); err != nil {
			return err
		}
		for _, inv := range sess.GetInvocations() {
			fmt.Fprintf(w, "%s  %s provenance: descriptor=%s manifest=%s image=%s\n",
				indent, inv.GetCallId(), inv.GetDescriptorHash(), inv.GetManifestContentHash(), inv.GetImageDigest())
			if inv.GetErrorMessage() != "" {
				fmt.Fprintf(w, "%s  %s error: %s (%s)\n", indent, inv.GetCallId(), inv.GetErrorMessage(), inv.GetErrorCode())
			}
		}
	}

	if len(sess.GetApprovals()) > 0 {
		fmt.Fprintf(w, "\n%sapprovals:\n", indent)
		for _, appr := range sess.GetApprovals() {
			fmt.Fprintf(w, "%s  approval %s\n", indent, appr.GetId())
			formatApprovalInspectDetail(w, indent+"    ", appr)
		}
	}

	for _, child := range sess.GetChildren() {
		fmt.Fprintln(w)
		if err := formatSessionInspectHuman(w, child, nil, depth+1); err != nil {
			return err
		}
	}
	return nil
}

func formatInspectTimeline(w io.Writer, entries []*runtimev1.InspectTimelineEntry) {
	for _, entry := range entries {
		depthIndent := strings.Repeat("  ", int(entry.GetDepth()))
		ts := entry.GetTimestamp()
		if entry.GetTsUnixMs() > 0 {
			ts = fmt.Sprintf("%d", entry.GetTsUnixMs())
		}
		gap := ""
		if entry.GetGapMs() > 0 {
			gap = fmt.Sprintf(" (+%dms)", entry.GetGapMs())
		}
		sessLabel := ""
		if sid := entry.GetSessionId(); sid != "" {
			sessLabel = fmt.Sprintf(" session=%s", sid)
		}
		fmt.Fprintf(w, "%s  %s%s [%s/%s]%s %s\n", depthIndent, ts, gap, entry.GetSource(), entry.GetKind(), sessLabel, entry.GetSummary())
		if ev := entry.GetEvent(); ev != nil && len(ev.GetPayload()) > 0 {
			fmt.Fprintf(w, "%s    event_payload: %s\n", depthIndent, string(ev.GetPayload()))
		}
		if inv := entry.GetInvocation(); inv != nil && (entry.GetKind() == "invocation_dispatched" || entry.GetKind() == "invocation_completed") {
			fmt.Fprintf(w, "%s    worker: %s attempt=%d queue_ms=%d exec_ms=%d total_ms=%d\n",
				depthIndent, inv.GetWorkerIdentity(), inv.GetAttempt(),
				inv.GetQueueDelayMs(), inv.GetExecutionDurationMs(), inv.GetTotalDurationMs())
			if len(inv.GetArgs()) > 0 {
				fmt.Fprintf(w, "%s    args: %s\n", depthIndent, string(inv.GetArgs()))
			}
			if len(inv.GetResult()) > 0 {
				fmt.Fprintf(w, "%s    result: %s\n", depthIndent, string(inv.GetResult()))
			}
			if u := inv.GetUsage(); u != nil {
				fmt.Fprintf(w, "%s    usage: in=%d out=%d total=%d\n", depthIndent, u.GetInputTokens(), u.GetOutputTokens(), u.GetTotalTokens())
			}
		}
		if appr := entry.GetApproval(); appr != nil {
			formatApprovalInspectDetail(w, depthIndent+"    ", appr)
		}
	}
}

func formatApprovalInspectDetail(w io.Writer, indent string, a *runtimev1.Approval) {
	fmt.Fprintf(w, "%sstatus: %s tool=%s@%s call_id=%s\n", indent, a.GetStatus(), a.GetTool(), a.GetVersion(), a.GetCallId())
	fmt.Fprintf(w, "%sroute: %s reason: %s policy: %s\n", indent, a.GetRoute(), a.GetReason(), a.GetPolicyName())
	fmt.Fprintf(w, "%sapprovals: %d/%d comprehension_required: %v\n", indent, a.GetApprovalsReceived(), a.GetApprovalsRequired(), a.GetComprehensionRequired())
	if a.GetExpiresAt() != "" {
		fmt.Fprintf(w, "%sexpires_at: %s\n", indent, a.GetExpiresAt())
	}
	if len(a.GetArgs()) > 0 {
		fmt.Fprintf(w, "%sargs: %s\n", indent, string(a.GetArgs()))
	}
	for _, v := range a.GetVotes() {
		fmt.Fprintf(w, "%svote: %s %s %q comprehension=%v\n", indent, v.GetDecidedBy(), v.GetDecision(), v.GetComment(), v.GetComprehensionAcknowledged())
	}
}

func writePrettyJSON(w io.Writer, indent string, raw []byte) error {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return err
	}
	pretty, err := json.MarshalIndent(v, indent, "  ")
	if err != nil {
		return err
	}
	fmt.Fprintln(w, string(pretty))
	return nil
}
