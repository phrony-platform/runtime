package main

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var (
	tuiTitleStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("39"))
	tuiMetaStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	tuiHeaderBarStyle = lipgloss.NewStyle().
				Background(lipgloss.Color("234")).
				Padding(0, 1)
	tuiUserLabelStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("42"))
	tuiUserBlockStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("252")).
				Background(lipgloss.Color("235")).
				Padding(1, 2).
				MarginBottom(1)
	tuiAgentLabelStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("117"))
	tuiAgentBlockStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("252")).
				Background(lipgloss.Color("236")).
				Padding(1, 2).
				MarginBottom(1)
	tuiHelpStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	tuiErrorStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	tuiBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("240")).
			Padding(1, 2, 0, 2)
	tuiStatusBarStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("252")).
				Background(lipgloss.Color("235")).
				Padding(0, 1)
	tuiStatusLabelStyle = lipgloss.NewStyle().Bold(true)
	tuiStatusSepStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("238"))
	tuiStatusMutedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	tuiInputBoxStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color("240")).
				Padding(0, 1)
	tuiInputBoxFocusStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color("39")).
				Padding(0, 1)
	tuiInputTitleStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("245")).
				MarginBottom(0)
	tuiBlockedBoxStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color("196")).
				Foreground(lipgloss.Color("224")).
				Background(lipgloss.Color("52")).
				Padding(0, 1)
	tuiBlockedTitleStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("196"))
	tuiApprovalBoxStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color("220")).
				Foreground(lipgloss.Color("252")).
				Background(lipgloss.Color("58")).
				Padding(0, 1)
	tuiApprovalTitleStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("220"))
)

// bodyContentWidth is the viewport width inside the chat box (border + inner padding).
func (m *runTUI) bodyContentWidth() int {
	// Outer box width is m.width-2; subtract border (2) and tuiBoxStyle horizontal padding (4).
	w := m.width - 8 - m.sessionsPaneWidth()
	if w < 10 {
		return 10
	}
	return w
}

// messageContentWidth is the render width for user/agent message blocks inside the viewport.
func (m *runTUI) messageContentWidth() int {
	w := m.bodyContentWidth() - 2
	if w < 10 {
		return 10
	}
	return w
}

func (m *runTUI) headerContentWidth() int {
	w := m.width - 2
	if w < 10 {
		return 10
	}
	return w
}

func (m *runTUI) conversationText() string {
	entry := m.selectedEntry()
	if entry == nil {
		return ""
	}
	var b strings.Builder
	if entry.transcript.Len() > 0 {
		b.WriteString(entry.transcript.String())
	}
	if entry.streaming.Len() > 0 {
		body := entry.streaming.String()
		if m.status == "streaming" && entry.isParent {
			body += lipgloss.NewStyle().Foreground(lipgloss.Color("117")).Render("▌")
		} else if !entry.isParent && entry.status == "running" {
			body += lipgloss.NewStyle().Foreground(lipgloss.Color("117")).Render("▌")
		}
		b.WriteString("\n")
		b.WriteString(renderAgentBlock(m.messageContentWidth(), "AGENT", body, nil))
	}
	if b.Len() == 0 {
		switch m.status {
		case "connecting":
			return "Waiting for session…"
		case "ending":
			return "Ending session…"
		default:
			return ""
		}
	}
	return b.String()
}

func (m *runTUI) chromeHeights() (header, status, footer, chatFrame int) {
	if m.width <= 0 {
		return 0, 0, 0, tuiBoxStyle.GetVerticalFrameSize()
	}
	return lipgloss.Height(m.headerView()),
		lipgloss.Height(m.statusBarView()),
		lipgloss.Height(m.footerView()),
		tuiBoxStyle.GetVerticalFrameSize()
}

func (m *runTUI) layout() {
	if m.width <= 0 || m.height <= 0 {
		return
	}
	headerH, statusH, footerH, frameH := m.chromeHeights()
	// JoinVertical adds one line between header, chat, status bar, and footer.
	sectionGaps := 3
	inner := m.height - headerH - statusH - footerH - frameH - sectionGaps
	if inner < 1 {
		inner = 1
	}
	m.viewport.Width = m.bodyContentWidth()
	m.viewport.Height = inner
	inputInner := m.width - 6
	if inputInner < 10 {
		inputInner = 10
	}
	m.input.Width = inputInner
	m.refreshViewport()
}

func (m *runTUI) headerView() string {
	title := "Phrony"
	if m.sessionID != "" {
		title = fmt.Sprintf("Phrony · session %s", shortID(m.sessionID))
	}
	hw := m.headerContentWidth()
	metaParts := []string{
		fmt.Sprintf("agent version %s · %s", shortID(m.agentVersionID), formatModelLine(m.modelProvider, m.modelName)),
	}
	if entry := m.selectedEntry(); entry != nil && !entry.isParent && entry.id != "" {
		metaParts = append([]string{fmt.Sprintf("session %s · %s", shortID(entry.id), entry.label)}, metaParts...)
	}
	inner := strings.Join([]string{
		tuiTitleStyle.Render(title),
		wrapTUIText(hw, tuiMetaStyle.Render(strings.Join(metaParts, " · "))),
	}, "\n")
	return tuiHeaderBarStyle.Width(m.width - 2).Render(inner)
}

func (m *runTUI) statusIndicator() string {
	var label, color string
	switch m.status {
	case "connecting":
		label, color = "Connecting", "214"
	case "streaming":
		label, color = "Streaming", "39"
	case "input":
		label, color = "Ready", "42"
	case "approval":
		label, color = "Awaiting approval", "220"
	case "blocked":
		label, color = "Limit reached", "196"
	case "ending":
		label, color = "Ending", "214"
	case "done":
		label, color = "Finished", "244"
	case "failed":
		label, color = "Failed", "196"
	case "cancelled":
		label, color = "Cancelled", "214"
	case "error":
		label, color = "Error", "196"
	default:
		label, color = "Active", "252"
	}
	dot := lipgloss.NewStyle().Foreground(lipgloss.Color(color)).Render("●")
	return tuiStatusLabelStyle.Render(dot + " " + label)
}

func (m *runTUI) statusBarView() string {
	if m.width < 4 {
		return ""
	}
	segments := []string{m.statusIndicator()}
	if wc := formatWallClockLimit(m.sessionStartedAt, m.maxWallClockSeconds, m.wallClockNow()); wc != "" {
		segments = append(segments, tuiStatusMutedStyle.Render(wc))
	}
	if m.statusHint != "" {
		segments = append(segments, tuiStatusMutedStyle.Render(m.statusHint))
	} else if m.lastStats != nil {
		if turn := m.lastStats.GetTurn(); turn > 0 {
			segments = append(segments, tuiStatusMutedStyle.Render(fmt.Sprintf("turn %d", turn)))
		}
		if u := m.lastStats.GetSessionUsage(); u != nil {
			segments = append(segments, tuiStatusMutedStyle.Render("session "+formatTokenUsage(u)))
			if pct := formatTokenLimitPercent(u, m.maxTokensPerRun); pct != "" {
				segments = append(segments, tuiStatusMutedStyle.Render(pct))
			}
		} else if u := m.lastStats.GetTurnUsage(); u != nil {
			segments = append(segments, tuiStatusMutedStyle.Render("turn "+formatTokenUsage(u)))
		}
		if m.lastStopReason != "" {
			segments = append(segments, tuiStatusMutedStyle.Render(m.lastStopReason))
		}
	} else if m.status == "streaming" {
		segments = append(segments, tuiStatusMutedStyle.Render("agent is responding…"))
	}
	content := strings.Join(segments, tuiStatusSepStyle.Render(" │ "))
	return tuiStatusBarStyle.Width(m.width - 2).Render(content)
}

func (m *runTUI) inputPanelView() string {
	style := tuiInputBoxStyle
	if m.input.Focused() {
		style = tuiInputBoxFocusStyle
	}
	title := tuiInputTitleStyle.Render("Message")
	field := m.input.View()
	inner := lipgloss.JoinVertical(lipgloss.Left, title, field)
	return style.Width(m.width - 2).Render(inner)
}

func (m *runTUI) approvalPanelView() string {
	title := tuiApprovalTitleStyle.Render("Tool approval required")
	var bodyLines []string
	if ar := m.pendingApproval; ar != nil {
		bodyLines = append(bodyLines, formatInteractiveApprovalLine(ar))
		required := ar.GetApprovalsRequired()
		if required <= 0 {
			required = 1
		}
		if required > 1 || ar.GetApprovalsReceived() > 0 {
			bodyLines = append(bodyLines, fmt.Sprintf("Approvals: %d/%d", ar.GetApprovalsReceived(), required))
		}
		if ar.GetComprehensionRequired() {
			bodyLines = append(bodyLines, "Comprehension required — A/D acknowledges and decides")
		}
	}
	body := wrapTUIText(m.width-8, strings.Join(bodyLines, "\n"))
	inner := lipgloss.JoinVertical(lipgloss.Left, title, body)
	return tuiApprovalBoxStyle.Width(m.width - 2).Render(inner)
}

func (m *runTUI) blockedPanelView() string {
	title := tuiBlockedTitleStyle.Render("Session limit reached")
	body := wrapTUIText(m.width-8, m.inputBlockedReason)
	inner := lipgloss.JoinVertical(lipgloss.Left, title, body)
	return tuiBlockedBoxStyle.Width(m.width - 2).Render(inner)
}

func (m *runTUI) footerView() string {
	if m.streamErr != nil {
		return tuiErrorStyle.Render(m.streamErr.Error())
	}
	switch {
	case m.awaitingApprovalDecision():
		return m.footerPanelWithHelp(
			m.approvalPanelView(),
			"A approve  ·  D deny  ·  PgUp/PgDn scroll  ·  Ctrl+End latest  ·  Ctrl+P menu",
		)
	case m.inputBlocked():
		return m.footerPanelWithHelp(
			m.blockedPanelView(),
			"PgUp/PgDn scroll  ·  Ctrl+End latest  ·  Ctrl+P menu",
		)
	case m.awaitingInput:
		return m.footerPanelWithHelp(
			m.inputPanelView(),
			"Enter send  ·  PgUp/PgDn scroll  ·  Ctrl+End latest  ·  Ctrl+P menu",
		)
	case m.readOnly:
		return m.footerHelpLine(
			"Session finished — scroll to review  ·  PgUp/PgDn  ·  Ctrl+End latest  ·  Ctrl+P menu",
		)
	case m.status == "done":
		return m.footerHelpLine("Session finished  ·  Ctrl+P menu  ·  Ctrl+C exit")
	default:
		return m.footerHelpLine("PgUp/PgDn scroll  ·  Ctrl+End latest  ·  Ctrl+P menu")
	}
}

func (m *runTUI) footerPanelWithHelp(panel, helpBase string) string {
	help := m.footerHelpLine(helpBase)
	if panel == "" {
		return help
	}
	return panel + "\n" + help
}

func (m *runTUI) footerHelpWidth() int {
	w := m.width - 2
	if w < 1 {
		return 0
	}
	return w
}

func (m *runTUI) footerHelpLine(base string) string {
	if m.delegationPaneVisible {
		if m.sessionsPaneExpanded() {
			base += "  ·  Ctrl+S sessions"
		} else {
			base += "  ·  " + tuiSessionsPaneCollapsedHint
		}
	}
	styled := tuiHelpStyle.Render(base)
	if w := m.footerHelpWidth(); w > 0 {
		return wrapTUIText(w, styled)
	}
	return styled
}

func (m *runTUI) chatBoxView() string {
	_, _, _, frameH := m.chromeHeights()
	outerH := m.viewport.Height + frameH
	if !m.sessionsPaneExpanded() {
		return tuiBoxStyle.Width(m.width - 2).Height(outerH).Render(m.viewport.View())
	}
	chatW := m.bodyContentWidth() + tuiBoxStyle.GetHorizontalFrameSize()
	chat := tuiBoxStyle.Width(chatW).Height(outerH).Render(m.viewport.View())
	pane := m.sessionsPaneView(outerH)
	return lipgloss.JoinHorizontal(lipgloss.Top, chat, pane)
}

func (m *runTUI) sessionsPaneView(outerH int) string {
	borderColor := lipgloss.Color("240")
	if m.focusPane == tuiFocusSessions {
		borderColor = lipgloss.Color("39")
	}
	style := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		Padding(0, 1)
	title := tuiInputTitleStyle.Render("Sessions")
	var lines []string
	for i, entry := range m.sessions.entries {
		label := entry.label
		if entry.isParent {
			label = "Parent"
		}
		dotColor := sessionStatusDotColor(entry.status)
		prefix := "  "
		if i == m.sessions.selectedIdx {
			prefix = "● "
		}
		line := prefix + lipgloss.NewStyle().Foreground(lipgloss.Color(dotColor)).Render("●") + " " + label
		if i == m.sessions.selectedIdx {
			line = lipgloss.NewStyle().Bold(true).Render(line)
		}
		lines = append(lines, wrapTUIText(tuiSessionsPaneWidth-4, line))
	}
	body := strings.Join(lines, "\n")
	inner := lipgloss.JoinVertical(lipgloss.Left, title, body)
	return style.Width(tuiSessionsPaneWidth).Height(outerH).Render(inner)
}

func sessionStatusDotColor(status string) string {
	switch status {
	case "running":
		return "39"
	case "done":
		return "244"
	case "failed":
		return "196"
	case "cancelled":
		return "214"
	default:
		return "252"
	}
}

func (m *runTUI) View() string {
	if m.width == 0 {
		return "Starting…\n"
	}
	base := lipgloss.JoinVertical(lipgloss.Top,
		m.headerView(),
		m.chatBoxView(),
		m.statusBarView(),
		m.footerView(),
	)
	if m.menu.open {
		return overlayCenter(base, m.menu.view(m.menuModalWidth()))
	}
	return base
}

// menuModalWidth is the total width of the action menu modal, clamped to keep
// the dialog readable on both narrow and wide terminals.
func (m *runTUI) menuModalWidth() int {
	w := m.width - 8
	if w > 64 {
		w = 64
	}
	if w < 24 {
		w = 24
	}
	return w
}
