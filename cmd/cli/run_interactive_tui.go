package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"

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
	statsLine      string

	transcript strings.Builder
	streaming  strings.Builder

	status        string
	awaitingInput bool
	sendClosed    bool
	streamErr     error
	quitting      bool
}

var (
	tuiTitleStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("39"))
	tuiMetaStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	tuiStatsStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("243"))
	tuiUserStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("42"))
	tuiAssistStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("39"))
	tuiHelpStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	tuiErrorStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	tuiBoxStyle   = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("240"))
)

func runInteractiveSessionTUI(
	ctx context.Context,
	stream interactiveStream,
	start *runtimev1.RunSessionInteractiveStart,
) error {
	m := newRunTUI(ctx, stream, start)
	p := tea.NewProgram(m, tea.WithContext(ctx), tea.WithAltScreen())
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
	ti.Placeholder = "Message the agent…"
	ti.CharLimit = 0
	ti.Prompt = "> "
	ti.Blur()

	vp := viewport.New(0, 0)
	vp.SetContent("Connecting…")

	return &runTUI{
		ctx:      ctx,
		stream:   stream,
		start:    start,
		input:    ti,
		viewport: vp,
		status:   "connecting",
	}
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

	case tea.KeyMsg:
		if m.awaitingInput {
			switch msg.String() {
			case "ctrl+c":
				m.quitting = true
				return m, tea.Quit
			case "ctrl+d":
				if err := m.closeSend(); err != nil {
					m.streamErr = err
					m.quitting = true
					return m, tea.Quit
				}
				m.awaitingInput = false
				m.input.Blur()
				m.status = "ending"
				m.refreshViewport()
				return m, m.recvStream()
			}
		} else if msg.String() == "ctrl+c" {
			m.quitting = true
			return m, tea.Quit
		}
	}

	if m.awaitingInput {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		if key, ok := msg.(tea.KeyMsg); ok && key.String() == "enter" {
			text := strings.TrimSpace(m.input.Value())
			if text == "" {
				m.statsLine = "empty message — type text or press ctrl+d to end"
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

	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	return m, cmd
}

func (m *runTUI) sendUserMessage(text string) error {
	m.transcript.WriteString("\n\n")
	m.transcript.WriteString(tuiUserStyle.Render("You"))
	m.transcript.WriteString("\n")
	m.transcript.WriteString(text)

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
	m.statsLine = ""
	m.refreshViewport()
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
		m.status = "streaming"
		m.refreshViewport()
	case msg.GetTextDelta() != nil:
		m.streaming.WriteString(msg.GetTextDelta().GetDelta())
		m.refreshViewport()
	case msg.GetAwaitingInput() != nil:
		if err := m.flushStreamingTurn(); err != nil {
			return err
		}
		awaiting := msg.GetAwaitingInput()
		m.statsLine = formatSessionStatsLine(awaiting.GetStats(), awaiting.GetStopReason())
		m.status = "input"
		m.awaitingInput = true
		m.input.Focus()
		m.refreshViewport()
	case msg.GetCompleted() != nil:
		if err := m.flushStreamingTurn(); err != nil {
			return err
		}
		completed := msg.GetCompleted()
		m.statsLine = "session complete · " + formatSessionStatsLine(completed.GetStats(), completed.GetStopReason())
		m.status = "done"
		m.awaitingInput = false
		m.input.Blur()
		m.refreshViewport()
	case msg.GetFailed() != nil:
		return fmt.Errorf("session failed: %s", msg.GetFailed().GetMessage())
	default:
		return fmt.Errorf("run session: unexpected server message")
	}
	return nil
}

func (m *runTUI) flushStreamingTurn() error {
	raw := m.streaming.String()
	m.streaming.Reset()
	if strings.TrimSpace(raw) == "" {
		return nil
	}

	var formatted bytes.Buffer
	w := newCompletionWriter(&formatted)
	if err := w.WriteDelta(raw); err != nil {
		return err
	}
	if err := w.Flush(); err != nil {
		return err
	}

	m.transcript.WriteString("\n\n")
	m.transcript.WriteString(tuiAssistStyle.Render("Assistant"))
	m.transcript.WriteString("\n")
	if formatted.Len() > 0 {
		m.transcript.Write(formatted.Bytes())
	} else {
		m.transcript.WriteString(raw)
	}
	return nil
}

func (m *runTUI) refreshViewport() {
	m.viewport.SetContent(wrapTUILines(m.bodyContentWidth(), m.conversationText()))
	m.viewport.GotoBottom()
}

// bodyContentWidth is the usable width inside the conversation box (border included).
func (m *runTUI) bodyContentWidth() int {
	// View sits inside tuiBoxStyle, which uses Width(m.width-2) including left/right border.
	w := m.width - 4
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
		b.WriteString("\n\n")
		b.WriteString(tuiAssistStyle.Render("Assistant"))
		b.WriteString("\n")
		b.WriteString(m.streaming.String())
		if m.status == "streaming" {
			b.WriteString("▌")
		}
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

func (m *runTUI) layout() {
	headerLines := 4
	footerLines := 3
	if m.awaitingInput {
		footerLines = 5
	}
	if m.statsLine != "" {
		headerLines = 5
	}

	bodyHeight := m.height - headerLines - footerLines
	if bodyHeight < 1 {
		bodyHeight = 1
	}
	m.viewport.Width = m.bodyContentWidth()
	m.viewport.Height = bodyHeight
	m.input.Width = m.width - 6
	if m.input.Width < 10 {
		m.input.Width = 10
	}
	m.refreshViewport()
}

func (m *runTUI) headerView() string {
	title := "Phrony session"
	if m.sessionID != "" {
		title = fmt.Sprintf("Session %s", shortID(m.sessionID))
	}
	hw := m.headerContentWidth()
	lines := []string{
		tuiTitleStyle.Render(title),
		wrapTUIText(hw, tuiMetaStyle.Render(fmt.Sprintf(
			"version %s · model %s",
			shortID(m.agentVersionID),
			formatModelLine(m.modelProvider, m.modelName),
		))),
	}
	if m.statsLine != "" {
		lines = append(lines, wrapTUIText(hw, tuiStatsStyle.Render(m.statsLine)))
	} else if m.status == "streaming" {
		lines = append(lines, wrapTUIText(hw, tuiStatsStyle.Render("assistant is responding…")))
	}
	return strings.Join(lines, "\n")
}

func (m *runTUI) footerView() string {
	if m.streamErr != nil {
		return tuiErrorStyle.Render(m.streamErr.Error())
	}
	switch {
	case m.awaitingInput:
		return tuiHelpStyle.Render("enter send · ctrl+d end session · ctrl+c quit") + "\n" + m.input.View()
	case m.status == "done":
		return tuiHelpStyle.Render("session finished")
	default:
		return tuiHelpStyle.Render("ctrl+c quit")
	}
}

func (m *runTUI) View() string {
	if m.width == 0 {
		return "Starting…\n"
	}
	body := tuiBoxStyle.Width(m.width - 2).Height(m.viewport.Height).Render(m.viewport.View())
	return fmt.Sprintf("%s\n%s\n%s", m.headerView(), body, m.footerView())
}
