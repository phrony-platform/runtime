package main

import (
	"fmt"
	"strings"
	"time"

	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
)

func shortID(id string) string {
	if len(id) <= 8 {
		return id
	}
	return id[:8]
}

func formatTokenUsage(u *runtimev1.TokenUsage) string {
	if u == nil || (u.GetInputTokens() == 0 && u.GetOutputTokens() == 0) {
		return "—"
	}
	prefix := ""
	if u.GetEstimated() {
		prefix = "~"
	}
	total := u.GetTotalTokens()
	if total == 0 {
		total = u.GetInputTokens() + u.GetOutputTokens()
	}
	return fmt.Sprintf("%s%d in / %d out (%d total)", prefix, u.GetInputTokens(), u.GetOutputTokens(), total)
}

func formatSessionStatsLine(stats *runtimev1.InteractiveSessionStats, stopReason string) string {
	if stats == nil {
		if stopReason == "" {
			return ""
		}
		return fmt.Sprintf("stop_reason=%s", stopReason)
	}
	line := fmt.Sprintf("turn %d", stats.GetTurn())
	if stopReason != "" {
		line += fmt.Sprintf(" · stop_reason=%s", stopReason)
	}
	if stats.GetTurnUsage() != nil {
		line += fmt.Sprintf(" · turn tokens: %s", formatTokenUsage(stats.GetTurnUsage()))
	}
	if stats.GetSessionUsage() != nil {
		line += fmt.Sprintf(" · session tokens: %s", formatTokenUsage(stats.GetSessionUsage()))
	}
	return line
}

// formatTurnFooter is a compact per-assistant-turn summary for the TUI transcript.
func formatTurnFooter(stats *runtimev1.InteractiveSessionStats, stopReason string, duration time.Duration) string {
	var parts []string
	if stats != nil && stats.GetTurn() > 0 {
		parts = append(parts, fmt.Sprintf("turn %d", stats.GetTurn()))
	}
	if duration > 0 {
		parts = append(parts, formatDuration(duration))
	}
	if stopReason != "" {
		parts = append(parts, stopReason)
	}
	if stats != nil && stats.GetTurnUsage() != nil {
		parts = append(parts, formatTokenUsage(stats.GetTurnUsage()))
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, " · ")
}

func formatDuration(d time.Duration) string {
	switch {
	case d < time.Millisecond:
		return "<1ms"
	case d < time.Second:
		return fmt.Sprintf("%dms", d.Round(time.Millisecond)/time.Millisecond)
	default:
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
}

func durationFromHistoryMessage(msg *runtimev1.InteractiveConversationMessage) time.Duration {
	if msg == nil || msg.GetTurnDurationMs() <= 0 {
		return 0
	}
	return time.Duration(msg.GetTurnDurationMs()) * time.Millisecond
}

func statsFromHistoryMessage(msg *runtimev1.InteractiveConversationMessage, turn int32) *runtimev1.InteractiveSessionStats {
	if msg == nil || msg.GetRole() != "assistant" {
		return nil
	}
	if msg.GetTurnUsage() == nil && msg.GetStopReason() == "" {
		return nil
	}
	return &runtimev1.InteractiveSessionStats{
		Turn:      turn,
		TurnUsage: msg.GetTurnUsage(),
	}
}

func formatModelLine(provider, name string) string {
	provider = strings.TrimSpace(provider)
	name = strings.TrimSpace(name)
	switch {
	case provider != "" && name != "":
		return provider + "/" + name
	case name != "":
		return name
	case provider != "":
		return provider
	default:
		return "—"
	}
}
