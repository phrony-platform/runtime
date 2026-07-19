package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/structpb"
)

func formatSessionInspectHuman(w io.Writer, sess *runtimev1.SessionInspect, timeline []*runtimev1.InspectTimelineEntry) error {
	if err := formatSessionInspectHeader(w, sess, 0); err != nil {
		return err
	}
	if len(timeline) > 0 {
		fmt.Fprintln(w, "\ntimeline:")
		formatInspectTimeline(w, timeline)
	}
	if len(sess.GetChildren()) > 0 {
		fmt.Fprintln(w, "\nchildren:")
		for _, child := range sess.GetChildren() {
			if err := formatSessionInspectChildBrief(w, child, 1); err != nil {
				return err
			}
		}
	}
	return nil
}

func formatSessionInspectHeader(w io.Writer, sess *runtimev1.SessionInspect, depth int) error {
	indent := strings.Repeat("  ", depth)
	fmt.Fprintf(w, "%s=== Session %s ===\n", indent, sess.GetId())

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

	if sess.GetInput() != nil {
		fmt.Fprintf(w, "\n%sinput:\n", indent)
		if err := writePrettyProtoValue(w, indent+"  ", sess.GetInput()); err != nil {
			return err
		}
	}

	if out := sess.GetOutput(); out != nil {
		fmt.Fprintf(w, "\n%soutput:\n", indent)
		b, err := protojson.MarshalOptions{Multiline: true, Indent: "  "}.Marshal(out)
		if err != nil {
			return err
		}
		for _, line := range strings.Split(strings.TrimRight(string(b), "\n"), "\n") {
			fmt.Fprintf(w, "%s  %s\n", indent, line)
		}
	}
	if errMsg := sess.GetError(); errMsg != "" {
		fmt.Fprintf(w, "\n%serror: %s\n", indent, errMsg)
	}
	return nil
}

func formatSessionInspectChildBrief(w io.Writer, sess *runtimev1.SessionInspect, depth int) error {
	indent := strings.Repeat("  ", depth)
	agent := sess.GetAgent()
	agentRef := sess.GetAgentVersionId()
	if agent != nil && agent.GetNamespace() != "" && agent.GetName() != "" {
		agentRef = fmt.Sprintf("%s/%s@%s", agent.GetNamespace(), agent.GetName(), agent.GetVersion())
	}
	fmt.Fprintf(w, "%s- id=%s status=%s agent=%s depth=%d\n", indent, sess.GetId(), sess.GetStatus(), agentRef, sess.GetDepth())
	for _, child := range sess.GetChildren() {
		if err := formatSessionInspectChildBrief(w, child, depth+1); err != nil {
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
		if ev := entry.GetEvent(); ev != nil && ev.GetPayload() != nil {
			fmt.Fprintf(w, "%s    event_payload: %s\n", depthIndent, protoValueCompactJSON(ev.GetPayload()))
		}
		if inv := entry.GetInvocation(); inv != nil && (entry.GetKind() == "invocation_dispatched" || entry.GetKind() == "invocation_completed") {
			fmt.Fprintf(w, "%s    worker: %s attempt=%d queue_ms=%d exec_ms=%d total_ms=%d\n",
				depthIndent, inv.GetWorkerIdentity(), inv.GetAttempt(),
				inv.GetQueueDelayMs(), inv.GetExecutionDurationMs(), inv.GetTotalDurationMs())
			if inv.GetArgs() != nil {
				fmt.Fprintf(w, "%s    args: %s\n", depthIndent, protoValueCompactJSON(inv.GetArgs()))
			}
			if inv.GetResult() != nil {
				fmt.Fprintf(w, "%s    result: %s\n", depthIndent, protoValueCompactJSON(inv.GetResult()))
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

func formatApprovalInspectDetail(w io.Writer, indent string, a *runtimev1.InspectApproval) {
	fmt.Fprintf(w, "%sstatus: %s tool=%s@%s call_id=%s\n", indent, a.GetStatus(), a.GetTool(), a.GetVersion(), a.GetCallId())
	fmt.Fprintf(w, "%sroute: %s reason: %s policy: %s\n", indent, a.GetRoute(), a.GetReason(), a.GetPolicyName())
	fmt.Fprintf(w, "%sapprovals: %d/%d comprehension_required: %v\n", indent, a.GetApprovalsReceived(), a.GetApprovalsRequired(), a.GetComprehensionRequired())
	if a.GetExpiresAt() != "" {
		fmt.Fprintf(w, "%sexpires_at: %s\n", indent, a.GetExpiresAt())
	}
	if a.GetArgs() != nil {
		fmt.Fprintf(w, "%sargs: %s\n", indent, protoValueCompactJSON(a.GetArgs()))
	}
	for _, v := range a.GetVotes() {
		fmt.Fprintf(w, "%svote: %s %s %q comprehension=%v\n", indent, v.GetDecidedBy(), v.GetDecision(), v.GetComment(), v.GetComprehensionAcknowledged())
	}
}

func writePrettyProtoValue(w io.Writer, indent string, v *structpb.Value) error {
	if v == nil {
		return nil
	}
	b, err := v.MarshalJSON()
	if err != nil {
		return err
	}
	var decoded any
	if err := json.Unmarshal(b, &decoded); err != nil {
		fmt.Fprintf(w, "%s%s\n", indent, string(b))
		return nil
	}
	pretty, err := json.MarshalIndent(decoded, indent, "  ")
	if err != nil {
		return err
	}
	fmt.Fprintln(w, string(pretty))
	return nil
}

func protoValueCompactJSON(v *structpb.Value) string {
	if v == nil {
		return ""
	}
	b, err := v.MarshalJSON()
	if err != nil {
		return ""
	}
	return string(b)
}
