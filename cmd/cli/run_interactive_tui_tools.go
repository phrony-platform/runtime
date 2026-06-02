package main

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
)

var (
	tuiToolCallLabelStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("214"))
	tuiToolCallBlockStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("252")).
				Background(lipgloss.Color("237")).
				Padding(1, 2).
				MarginBottom(1)
	tuiToolResultLabelStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("81"))
	tuiToolResultBlockStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("252")).
				Background(lipgloss.Color("23")).
				Padding(1, 2).
				MarginBottom(1)
	tuiToolResultErrorBlockStyle = lipgloss.NewStyle().
					Foreground(lipgloss.Color("224")).
					Background(lipgloss.Color("52")).
					Padding(1, 2).
					MarginBottom(1)
	tuiToolApprovalLabelStyle = lipgloss.NewStyle().
					Bold(true).
					Foreground(lipgloss.Color("220"))
	tuiToolApprovalBlockStyle = lipgloss.NewStyle().
					Foreground(lipgloss.Color("252")).
					Background(lipgloss.Color("58")).
					Padding(1, 2).
					MarginBottom(1)
	tuiToolFieldLabelStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("245"))
)

func formatToolBindingName(tool, version string) string {
	tool = strings.TrimSpace(tool)
	version = strings.TrimSpace(version)
	if tool == "" {
		tool = "tool"
	}
	if version != "" {
		return fmt.Sprintf("%s@%s", tool, version)
	}
	return tool
}

func formatToolDisplayJSON(raw []byte) string {
	if len(raw) == 0 {
		return "{}"
	}
	pretty, err := indentJSONBytes(raw)
	if err != nil || len(pretty) == 0 {
		return string(raw)
	}
	return string(pretty)
}

func renderToolFieldLines(fields [][2]string) string {
	if len(fields) == 0 {
		return " "
	}
	lines := make([]string, 0, len(fields))
	for _, f := range fields {
		key := strings.TrimSpace(f[0])
		val := strings.TrimSpace(f[1])
		if val == "" {
			val = "—"
		}
		lines = append(lines, tuiToolFieldLabelStyle.Render(key)+":\n"+val)
	}
	return strings.Join(lines, "\n\n")
}

func renderToolPanel(width int, label string, labelStyle, blockStyle lipgloss.Style, fields [][2]string) string {
	inner := labelStyle.Render(label) + "\n" + renderToolFieldLines(fields)
	if width > 0 {
		return blockStyle.Width(width).Render(inner)
	}
	return blockStyle.Render(inner)
}

func renderToolCallBlock(width int, tc *runtimev1.RunSessionInteractiveToolCall) string {
	if tc == nil {
		return renderToolPanel(width, "TOOL CALL", tuiToolCallLabelStyle, tuiToolCallBlockStyle, nil)
	}
	fields := [][2]string{
		{"call_id", tc.GetCallId()},
		{"tool", formatToolBindingName(tc.GetTool(), tc.GetVersion())},
		{"input", formatToolDisplayJSON(tc.GetArgs())},
	}
	return renderToolPanel(width, "TOOL CALL", tuiToolCallLabelStyle, tuiToolCallBlockStyle, fields)
}

func renderToolResultBlock(width int, tr *runtimev1.RunSessionInteractiveToolResult) string {
	if tr == nil {
		return renderToolPanel(width, "TOOL RESULT", tuiToolResultLabelStyle, tuiToolResultBlockStyle, nil)
	}
	fields := [][2]string{{"call_id", tr.GetCallId()}}
	labelStyle := tuiToolResultLabelStyle
	blockStyle := tuiToolResultBlockStyle
	label := "TOOL RESULT"
	if msg := strings.TrimSpace(tr.GetErrorMessage()); msg != "" {
		label = "TOOL FAILED"
		fields = append(fields, [2]string{"error", msg})
		blockStyle = tuiToolResultErrorBlockStyle
	} else {
		fields = append(fields, [2]string{"output", formatToolDisplayJSON(tr.GetPayload())})
	}
	return renderToolPanel(width, label, labelStyle, blockStyle, fields)
}

func renderToolApprovalBlock(width int, ar *runtimev1.RunSessionInteractiveApprovalRequired) string {
	if ar == nil {
		return renderToolPanel(width, "APPROVAL REQUIRED", tuiToolApprovalLabelStyle, tuiToolApprovalBlockStyle, nil)
	}
	fields := [][2]string{
		{"approval_id", ar.GetApprovalId()},
		{"call_id", ar.GetCallId()},
		{"tool", formatToolBindingName(ar.GetTool(), ar.GetVersion())},
		{"reason", ar.GetReason()},
	}
	if route := strings.TrimSpace(ar.GetRoute()); route != "" {
		fields = append(fields, [2]string{"route", route})
	}
	required := ar.GetApprovalsRequired()
	if required <= 0 {
		required = 1
	}
	if required > 1 || ar.GetApprovalsReceived() > 0 {
		fields = append(fields, [2]string{"approvals", fmt.Sprintf("%d/%d", ar.GetApprovalsReceived(), required)})
	}
	if ar.GetComprehensionRequired() {
		fields = append(fields, [2]string{"comprehension", "required"})
	}
	fields = append(fields, [2]string{"input", formatToolDisplayJSON(ar.GetArgs())})
	return renderToolPanel(width, "APPROVAL REQUIRED", tuiToolApprovalLabelStyle, tuiToolApprovalBlockStyle, fields)
}
