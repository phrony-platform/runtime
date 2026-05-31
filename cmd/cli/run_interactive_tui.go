package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
	"github.com/phrony-platform/runtime/internal/clierr"
)

type streamServerMsg struct {
	msg *runtimev1.RunSessionInteractiveServerMsg
	err error
}

type runTUI struct {
	ctx    context.Context
	stream interactiveStream
	start  *runtimev1.RunSessionInteractiveStart

	viewport viewport.Model
	input    textinput.Model

	width  int
	height int

	sessionID      string
	agentVersionID string
	modelProvider  string
	modelName      string

	lastStats      *runtimev1.InteractiveSessionStats
	lastStopReason string
	statusHint     string
	turnStartedAt  time.Time

	transcript strings.Builder
	streaming  strings.Builder

	status        string
	awaitingInput bool
	sendClosed    bool
	streamErr     error
	quitting      bool

	// followTail auto-scrolls the transcript on new output while the user is at the bottom.
	followTail bool
	// historyMetaTurns is the highest assistant turn number rendered from session history metadata.
	historyMetaTurns int32
}

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
)

func runInteractiveSessionTUI(
	ctx context.Context,
	stream interactiveStream,
	start *runtimev1.RunSessionInteractiveStart,
) error {
	m := newRunTUI(ctx, stream, start)
	p := tea.NewProgram(m, tea.WithContext(ctx), tea.WithAltScreen(), tea.WithMouseCellMotion())
	final, err := p.Run()
	if err != nil {
		return err
	}
	if m, ok := final.(*runTUI); ok && m.streamErr != nil {
		return m.streamErr
	}
	return nil
}

func newRunTUI(ctx context.Context, stream interactiveStream, start *runtimev1.RunSessionInteractiveStart) *runTUI {
	ti := textinput.New()
	ti.Placeholder = "Type your message…"
	ti.CharLimit = 0
	ti.Prompt = ""
	ti.PromptStyle = lipgloss.NewStyle()
	ti.TextStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	ti.PlaceholderStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("238")).Italic(true)
	ti.Cursor.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("39"))
	ti.Blur()

	vp := viewport.New(0, 0)
	vp.MouseWheelEnabled = true
	vp.SetContent("Connecting…")

	return &runTUI{
		ctx:        ctx,
		stream:     stream,
		start:      start,
		input:      ti,
		viewport:   vp,
		status:     "connecting",
		followTail: true,
	}
}

var tuiScrollKeys = viewport.KeyMap{
	// Avoid binding printable keys (space, f, b, etc.) so the message box can accept normal text.
	PageDown: key.NewBinding(key.WithKeys("pgdown")),
	PageUp:   key.NewBinding(key.WithKeys("pgup")),
	HalfPageUp: key.NewBinding(
		key.WithKeys("ctrl+u"),
	),
	HalfPageDown: key.NewBinding(
		key.WithKeys("ctrl+f"),
	),
	Up: key.NewBinding(
		key.WithKeys("shift+up", "shift+k"),
	),
	Down: key.NewBinding(
		key.WithKeys("shift+down", "shift+j"),
	),
}

func tuiScrollWhileInput(msg tea.KeyMsg) bool {
	return key.Matches(msg, tuiScrollKeys.PageDown, tuiScrollKeys.PageUp,
		tuiScrollKeys.HalfPageUp, tuiScrollKeys.HalfPageDown,
		tuiScrollKeys.Up, tuiScrollKeys.Down)
}

func (m *runTUI) scrollViewport(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "shift+up", "shift+k":
			m.viewport.LineUp(1)
		case "shift+down", "shift+j":
			m.viewport.LineDown(1)
		default:
			var cmd tea.Cmd
			m.viewport, cmd = m.viewport.Update(msg)
			if cmd != nil {
				return cmd
			}
		}
	default:
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		return cmd
	}
	m.followTail = m.viewport.AtBottom()
	return nil
}

func (m *runTUI) jumpToLatest() {
	m.viewport.GotoBottom()
	m.followTail = true
}

func (m *runTUI) Init() tea.Cmd {
	return tea.Batch(m.sendStart(), m.recvStream(), textinput.Blink)
}

func (m *runTUI) sendStart() tea.Cmd {
	return func() tea.Msg {
		if err := m.stream.Send(&runtimev1.RunSessionInteractiveClientMsg{
			Body: &runtimev1.RunSessionInteractiveClientMsg_Start{Start: m.start},
		}); err != nil {
			return streamServerMsg{err: clierr.WrapRPC("run session", err)}
		}
		return nil
	}
}

func (m *runTUI) recvStream() tea.Cmd {
	return func() tea.Msg {
		msg, err := m.stream.Recv()
		return streamServerMsg{msg: msg, err: err}
	}
}

func (m *runTUI) closeSend() error {
	if m.sendClosed {
		return nil
	}
	m.sendClosed = true
	if err := m.stream.CloseSend(); err != nil {
		return clierr.WrapRPC("run session", err)
	}
	return nil
}

func (m *runTUI) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.quitting {
		return m, tea.Quit
	}

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.layout()
		return m, nil

	case streamServerMsg:
		if msg.err != nil {
			if msg.err == io.EOF {
				m.status = "done"
				m.quitting = true
				return m, tea.Quit
			}
			m.streamErr = msg.err
			m.status = "error"
			m.quitting = true
			return m, tea.Quit
		}
		if err := m.handleServerMsg(msg.msg); err != nil {
			m.streamErr = err
			m.status = "error"
			m.quitting = true
			return m, tea.Quit
		}
		if m.status == "done" {
			m.quitting = true
			return m, tea.Quit
		}
		return m, m.recvStream()

	case tea.MouseMsg:
		return m, m.scrollViewport(msg)

	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			m.quitting = true
			return m, tea.Quit
		}
		if msg.String() == "ctrl+end" || msg.String() == "shift+g" {
			m.jumpToLatest()
			return m, nil
		}
		if m.awaitingInput {
			switch msg.String() {
			case "ctrl+d":
				if err := m.closeSend(); err != nil {
					m.streamErr = err
					m.quitting = true
					return m, tea.Quit
				}
				m.awaitingInput = false
				m.input.Blur()
				m.status = "ending"
				m.statusHint = ""
				m.layout()
				return m, m.recvStream()
			}
			if tuiScrollWhileInput(msg) {
				return m, m.scrollViewport(msg)
			}
		}
	}

	if m.awaitingInput {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		if key, ok := msg.(tea.KeyMsg); ok && key.String() == "enter" {
			text := strings.TrimSpace(m.input.Value())
			if text == "" {
				m.statusHint = "Type a message or press Ctrl+D to end the session"
				m.refreshViewport()
				return m, nil
			}
			if err := m.sendUserMessage(text); err != nil {
				m.streamErr = err
				m.quitting = true
				return m, tea.Quit
			}
		}
		return m, cmd
	}

	return m, m.scrollViewport(msg)
}

func (m *runTUI) sendUserMessage(text string) error {
	m.transcript.WriteString("\n")
	m.transcript.WriteString(renderUserBlock(m.messageContentWidth(), text))

	if err := m.stream.Send(&runtimev1.RunSessionInteractiveClientMsg{
		Body: &runtimev1.RunSessionInteractiveClientMsg_UserMessage{
			UserMessage: &runtimev1.RunSessionInteractiveUserMessage{Text: text},
		},
	}); err != nil {
		return clierr.WrapRPC("run session", err)
	}

	m.input.SetValue("")
	m.input.Blur()
	m.awaitingInput = false
	m.status = "streaming"
	m.lastStats = nil
	m.lastStopReason = ""
	m.statusHint = ""
	m.turnStartedAt = time.Now()
	m.followTail = true
	m.layout()
	return nil
}

func (m *runTUI) handleServerMsg(msg *runtimev1.RunSessionInteractiveServerMsg) error {
	switch {
	case msg.GetSessionStarted() != nil:
		started := msg.GetSessionStarted()
		m.sessionID = started.GetSessionId()
		m.agentVersionID = started.GetAgentVersionId()
		m.modelProvider = started.GetModelProvider()
		m.modelName = started.GetModelName()
		if err := m.appendConversationHistory(started.GetHistory()); err != nil {
			return err
		}
		m.status = "streaming"
		m.layout()
	case msg.GetTextDelta() != nil:
		m.streaming.WriteString(msg.GetTextDelta().GetDelta())
		m.refreshViewport()
	case msg.GetAwaitingInput() != nil:
		duration := m.turnElapsed()
		awaiting := msg.GetAwaitingInput()
		stats := awaiting.GetStats()
		meta := turnMeta(stats, awaiting.GetStopReason(), duration)
		added, err := m.flushStreamingTurn(meta)
		if err != nil {
			return err
		}
		m.setLastTurnStats(stats, awaiting.GetStopReason())
		if !added && meta != nil && stats != nil && stats.GetTurnUsage() != nil && stats.GetTurn() > m.historyMetaTurns {
			m.transcript.WriteString("\n")
			m.transcript.WriteString(renderAgentMetaStrip(m.messageContentWidth(), meta))
		}
		m.status = "input"
		m.awaitingInput = true
		m.input.Focus()
		m.layout()
	case msg.GetCompleted() != nil:
		duration := m.turnElapsed()
		completed := msg.GetCompleted()
		meta := turnMeta(completed.GetStats(), completed.GetStopReason(), duration)
		if _, err := m.flushStreamingTurn(meta); err != nil {
			return err
		}
		if m.transcript.Len() == 0 {
			if err := m.appendCompletedOutput(completed.GetOutput(), meta); err != nil {
				return err
			}
		}
		m.setLastTurnStats(completed.GetStats(), completed.GetStopReason())
		m.status = "done"
		m.awaitingInput = false
		m.input.Blur()
		m.layout()
	case msg.GetFailed() != nil:
		return fmt.Errorf("session failed: %s", msg.GetFailed().GetMessage())
	default:
		return fmt.Errorf("run session: unexpected server message")
	}
	return nil
}

func (m *runTUI) turnElapsed() time.Duration {
	if m.turnStartedAt.IsZero() {
		return 0
	}
	return time.Since(m.turnStartedAt)
}

func (m *runTUI) setLastTurnStats(stats *runtimev1.InteractiveSessionStats, stopReason string) {
	m.lastStats = stats
	m.lastStopReason = stopReason
	m.turnStartedAt = time.Time{}
}

func (m *runTUI) appendConversationHistory(msgs []*runtimev1.InteractiveConversationMessage) error {
	var turn int32
	for _, msg := range msgs {
		role := msg.GetRole()
		content := msg.GetContent()
		switch role {
		case "user":
			m.transcript.WriteString("\n")
			m.transcript.WriteString(renderUserBlock(m.messageContentWidth(), content))
		case "assistant":
			turn++
			formatted, err := formatAssistantTranscript(content)
			if err != nil {
				return err
			}
			meta := turnMeta(statsFromHistoryMessage(msg, turn), msg.GetStopReason(), durationFromHistoryMessage(msg))
			m.transcript.WriteString("\n")
			m.transcript.WriteString(renderAgentBlock(m.messageContentWidth(), "AGENT", string(formatted), meta))
			if meta != nil {
				m.historyMetaTurns = turn
			}
		default:
			return fmt.Errorf("run session: unknown history role %q", role)
		}
	}
	return nil
}

func formatAssistantTranscript(content string) ([]byte, error) {
	if strings.TrimSpace(content) == "" {
		return nil, nil
	}
	var formatted bytes.Buffer
	w := newCompletionWriter(&formatted)
	if err := w.WriteDelta(content); err != nil {
		return nil, err
	}
	if err := w.Flush(); err != nil {
		return nil, err
	}
	if formatted.Len() > 0 {
		return formatted.Bytes(), nil
	}
	return []byte(content), nil
}

func (m *runTUI) appendCompletedOutput(raw []byte, meta *turnDisplayMeta) error {
	if len(raw) == 0 {
		return nil
	}
	pretty, err := prettifySessionOutput(raw)
	if err != nil {
		return err
	}
	m.transcript.WriteString("\n")
	m.transcript.WriteString(renderAgentBlock(m.messageContentWidth(), "Result", string(pretty), meta))
	return nil
}

func (m *runTUI) flushStreamingTurn(meta *turnDisplayMeta) (bool, error) {
	raw := m.streaming.String()
	m.streaming.Reset()
	if strings.TrimSpace(raw) == "" {
		return false, nil
	}

	var formatted bytes.Buffer
	w := newCompletionWriter(&formatted)
	if err := w.WriteDelta(raw); err != nil {
		return false, err
	}
	if err := w.Flush(); err != nil {
		return false, err
	}

	body := raw
	if formatted.Len() > 0 {
		body = formatted.String()
	}
	m.transcript.WriteString("\n")
	m.transcript.WriteString(renderAgentBlock(m.messageContentWidth(), "AGENT", body, meta))
	return true, nil
}

func (m *runTUI) refreshViewport() {
	m.viewport.SetContent(wrapTUILines(m.messageContentWidth(), m.conversationText()))
	if m.followTail {
		m.viewport.GotoBottom()
	}
}

// bodyContentWidth is the viewport width inside the chat box (border + inner padding).
func (m *runTUI) bodyContentWidth() int {
	// Outer box width is m.width-2; subtract border (2) and tuiBoxStyle horizontal padding (4).
	w := m.width - 8
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
	var b strings.Builder
	if m.transcript.Len() > 0 {
		b.WriteString(m.transcript.String())
	}
	if m.streaming.Len() > 0 {
		body := m.streaming.String()
		if m.status == "streaming" {
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
	inner := strings.Join([]string{
		tuiTitleStyle.Render(title),
		wrapTUIText(hw, tuiMetaStyle.Render(fmt.Sprintf(
			"agent version %s · %s",
			shortID(m.agentVersionID),
			formatModelLine(m.modelProvider, m.modelName),
		))),
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
	case "ending":
		label, color = "Ending", "214"
	case "done":
		label, color = "Finished", "244"
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
	if m.statusHint != "" {
		segments = append(segments, tuiStatusMutedStyle.Render(m.statusHint))
	} else if m.lastStats != nil {
		if turn := m.lastStats.GetTurn(); turn > 0 {
			segments = append(segments, tuiStatusMutedStyle.Render(fmt.Sprintf("turn %d", turn)))
		}
		if u := m.lastStats.GetSessionUsage(); u != nil {
			segments = append(segments, tuiStatusMutedStyle.Render("session "+formatTokenUsage(u)))
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

func (m *runTUI) footerView() string {
	if m.streamErr != nil {
		return tuiErrorStyle.Render(m.streamErr.Error())
	}
	switch {
	case m.awaitingInput:
		help := tuiHelpStyle.Render(
			"PgUp/PgDn scroll  ·  Shift+↑↓ line  ·  Ctrl+End latest  ·  Enter send  ·  Ctrl+D end  ·  Ctrl+C quit",
		)
		return m.inputPanelView() + "\n" + help
	case m.status == "done":
		return tuiHelpStyle.Render("Session finished — press Ctrl+C to exit")
	default:
		return tuiHelpStyle.Render("PgUp/PgDn scroll  ·  Ctrl+End latest  ·  Ctrl+C quit")
	}
}

func (m *runTUI) chatBoxView() string {
	_, _, _, frameH := m.chromeHeights()
	outerH := m.viewport.Height + frameH
	return tuiBoxStyle.Width(m.width - 2).Height(outerH).Render(m.viewport.View())
}

func (m *runTUI) View() string {
	if m.width == 0 {
		return "Starting…\n"
	}
	return lipgloss.JoinVertical(lipgloss.Top,
		m.headerView(),
		m.chatBoxView(),
		m.statusBarView(),
		m.footerView(),
	)
}
