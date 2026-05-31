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

// tuiClockTick refreshes the wall-clock line in the status bar.
type tuiClockTick struct{}

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

	lastStats         *runtimev1.InteractiveSessionStats
	lastStopReason    string
	maxTokensPerRun     int32
	maxWallClockSeconds int32
	sessionStartedAt    time.Time
	sessionEndedAt      time.Time
	statusHint          string
	turnStartedAt  time.Time

	transcript strings.Builder
	streaming  strings.Builder

	status             string
	awaitingInput      bool
	inputBlockedReason string
	inputEverEnabled   bool
	readOnly           bool
	sendClosed         bool
	streamErr          error
	quitting           bool

	// followTail auto-scrolls the transcript on new output while the user is at the bottom.
	followTail bool
	// historyMetaTurns is the highest assistant turn number rendered from session history metadata.
	historyMetaTurns int32
}

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

func (m *runTUI) clockTickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return tuiClockTick{} })
}

func (m *runTUI) Init() tea.Cmd {
	return tea.Batch(m.sendStart(), m.recvStream(), textinput.Blink, m.clockTickCmd())
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
				if m.readOnly {
					m.quitting = true
					return m, tea.Quit
				}
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
		if m.status == "done" && !m.readOnly {
			m.quitting = true
			return m, tea.Quit
		}
		if m.readOnly {
			return m, nil
		}
		return m, m.recvStream()

	case tuiClockTick:
		if m.quitting || m.status == "done" || m.status == "error" {
			return m, nil
		}
		// Re-render status bar wall clock; no-op until session_started provides limits.
		return m, m.clockTickCmd()

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
		if m.awaitingInput || m.inputBlocked() {
			switch msg.String() {
			case "ctrl+d":
				if err := m.closeSend(); err != nil {
					m.streamErr = err
					m.quitting = true
					return m, tea.Quit
				}
				m.awaitingInput = false
				m.inputBlockedReason = ""
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

	if m.awaitingInput && !m.inputBlocked() {
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
		m.maxTokensPerRun = started.GetMaxTokensPerRun()
		m.maxWallClockSeconds = started.GetMaxWallClockSeconds()
		if ms := started.GetSessionStartedAtUnixMs(); ms > 0 {
			m.sessionStartedAt = time.UnixMilli(ms)
		}
		if ms := started.GetSessionEndedAtUnixMs(); ms > 0 {
			m.sessionEndedAt = time.UnixMilli(ms)
		}
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
		m.applyAwaitingInputState(awaiting.GetInputBlockedReason())
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
		if ms := completed.GetSessionEndedAtUnixMs(); ms > 0 {
			m.sessionEndedAt = time.UnixMilli(ms)
		} else if m.sessionEndedAt.IsZero() {
			m.sessionEndedAt = time.Now()
		}
		m.status = "done"
		m.awaitingInput = false
		m.input.Blur()
		if strings.TrimSpace(m.start.GetSessionId()) != "" && !m.inputEverEnabled {
			m.readOnly = true
			if err := m.closeSend(); err != nil {
				return err
			}
		}
		m.layout()
	case msg.GetFailed() != nil:
		return fmt.Errorf("session failed: %s", msg.GetFailed().GetMessage())
	default:
		return fmt.Errorf("run session: unexpected server message")
	}
	return nil
}

func (m *runTUI) wallClockNow() time.Time {
	if !m.sessionEndedAt.IsZero() {
		return m.sessionEndedAt
	}
	return time.Now()
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

func (m *runTUI) applyAwaitingInputState(inputBlockedReason string) {
	m.inputBlockedReason = strings.TrimSpace(inputBlockedReason)
	if m.inputBlockedReason != "" {
		m.status = "blocked"
		m.awaitingInput = false
		m.input.Blur()
		m.input.SetValue("")
		return
	}
	m.status = "input"
	m.awaitingInput = true
	m.inputEverEnabled = true
	m.input.Focus()
}

func (m *runTUI) inputBlocked() bool {
	return m.inputBlockedReason != ""
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
