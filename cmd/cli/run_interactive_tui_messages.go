package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
)

// turnDisplayMeta is per-message stats shown inside a message block.
type turnDisplayMeta struct {
	stats      *runtimev1.InteractiveSessionStats
	stopReason string
	duration   time.Duration
}

var (
	tuiTurnBadgeStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("236")).
				Background(lipgloss.Color("117")).
				Bold(true).
				Padding(0, 1)
	tuiAgentPanelBG = lipgloss.Color("236")
	tuiMetaBarStyle = lipgloss.NewStyle().
				BorderStyle(lipgloss.NormalBorder()).
				BorderTop(true).
				BorderForeground(lipgloss.Color("238")).
				Background(tuiAgentPanelBG).
				PaddingTop(1)
	tuiMetaTextStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	tuiMetaChipIconStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("117"))
	tuiMetaSepStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("238"))
	tuiAgentMetaStripStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("252")).
				Background(lipgloss.Color("236")).
				Padding(0, 2).
				MarginBottom(1)
)

func turnMeta(stats *runtimev1.InteractiveSessionStats, stopReason string, duration time.Duration) *turnDisplayMeta {
	if stats == nil && stopReason == "" && duration <= 0 {
		return nil
	}
	if stats != nil && stats.GetTurn() == 0 && stats.GetTurnUsage() == nil && stopReason == "" && duration <= 0 {
		return nil
	}
	return &turnDisplayMeta{stats: stats, stopReason: stopReason, duration: duration}
}

func turnNumber(meta *turnDisplayMeta) int32 {
	if meta == nil || meta.stats == nil {
		return 0
	}
	return meta.stats.GetTurn()
}

func formatTokenChip(n int32, estimated bool) string {
	if estimated {
		return fmt.Sprintf("~%d", n)
	}
	return fmt.Sprintf("%d", n)
}

func renderMetaItem(icon, label string) string {
	return tuiMetaChipIconStyle.Render(icon) + tuiMetaTextStyle.Render(" "+label)
}

// messageInnerWidth is the content width inside a message panel (border + padding).
func messageInnerWidth(outerWidth int) int {
	w := outerWidth - 6
	if w < 10 {
		return 10
	}
	return w
}

func renderMessageMetaBar(innerWidth int, meta turnDisplayMeta) string {
	var items []string
	if meta.duration > 0 {
		items = append(items, renderMetaItem("⏱", formatDuration(meta.duration)))
	}
	if meta.stats != nil && meta.stats.GetTurnUsage() != nil {
		u := meta.stats.GetTurnUsage()
		items = append(items, renderMetaItem("▼", formatTokenChip(u.GetInputTokens(), u.GetEstimated())+" in"))
		items = append(items, renderMetaItem("▲", formatTokenChip(u.GetOutputTokens(), u.GetEstimated())+" out"))
		total := u.GetTotalTokens()
		if total == 0 {
			total = u.GetInputTokens() + u.GetOutputTokens()
		}
		if total > 0 {
			items = append(items, renderMetaItem("Σ", formatTokenChip(total, u.GetEstimated())))
		}
	}
	if meta.stopReason != "" {
		items = append(items, renderMetaItem("●", meta.stopReason))
	}
	if len(items) == 0 {
		return ""
	}
	sep := tuiMetaSepStyle.Render(" · ")
	line := strings.Join(items, sep)
	if innerWidth > 0 {
		return tuiMetaBarStyle.Width(innerWidth).Render(line)
	}
	return tuiMetaBarStyle.Render(line)
}

func renderMessageHeader(_ int, label string, meta *turnDisplayMeta, labelStyle lipgloss.Style) string {
	if meta == nil || turnNumber(meta) <= 0 {
		return labelStyle.Render(label)
	}
	badge := tuiTurnBadgeStyle.Render(fmt.Sprintf("#%d", turnNumber(meta)))
	return lipgloss.JoinHorizontal(lipgloss.Top, badge, lipgloss.NewStyle().Render(" "), labelStyle.Render(label))
}

func renderUserBlock(width int, body string) string {
	if strings.TrimSpace(body) == "" {
		body = " "
	}
	innerW := messageInnerWidth(width)
	inner := renderMessageHeader(innerW, "YOU", nil, tuiUserLabelStyle) + "\n" + body
	if width > 0 {
		return tuiUserBlockStyle.Width(width).Render(inner)
	}
	return tuiUserBlockStyle.Render(inner)
}

func renderAgentBlock(width int, label, body string, meta *turnDisplayMeta) string {
	if strings.TrimSpace(body) == "" {
		body = " "
	}
	innerW := messageInnerWidth(width)
	var parts []string
	parts = append(parts, renderMessageHeader(innerW, label, meta, tuiAgentLabelStyle))
	parts = append(parts, body)
	if meta != nil {
		if bar := renderMessageMetaBar(innerW, *meta); bar != "" {
			parts = append(parts, bar)
		}
	}
	inner := strings.Join(parts, "\n")
	if width > 0 {
		return tuiAgentBlockStyle.Width(width).Render(inner)
	}
	return tuiAgentBlockStyle.Render(inner)
}

// renderAgentMetaStrip appends a meta bar with the same panel styling (for late stats on re-attach).
func renderAgentMetaStrip(width int, meta *turnDisplayMeta) string {
	if meta == nil {
		return ""
	}
	bar := renderMessageMetaBar(messageInnerWidth(width), *meta)
	if bar == "" {
		return ""
	}
	if width > 0 {
		return tuiAgentMetaStripStyle.Width(width).Render(bar)
	}
	return tuiAgentMetaStripStyle.Render(bar)
}
