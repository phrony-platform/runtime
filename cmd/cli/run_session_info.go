package main

import (
	"fmt"
	"strings"

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
