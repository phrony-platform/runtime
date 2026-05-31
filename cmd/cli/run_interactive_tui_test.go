package main

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
)

func TestRunTUI_handleServerMsg_inputBlocked(t *testing.T) {
	m := newRunTUI(context.Background(), &mockInteractiveClientStream{}, &runtimev1.RunSessionInteractiveStart{})
	m.width = 80
	m.height = 24
	m.layout()

	err := m.handleServerMsg(&runtimev1.RunSessionInteractiveServerMsg{
		Body: &runtimev1.RunSessionInteractiveServerMsg_AwaitingInput{
			AwaitingInput: &runtimev1.RunSessionInteractiveAwaitingInput{
				InputBlockedReason: "run limit max_tokens_per_run exceeded (on_limit=halt)",
				Stats: &runtimev1.InteractiveSessionStats{
					Turn: 1,
					SessionUsage: &runtimev1.TokenUsage{
						InputTokens: 50, OutputTokens: 20, TotalTokens: 70,
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("awaiting_input: %v", err)
	}
	if !m.inputBlocked() || m.awaitingInput {
		t.Fatalf("blocked=%v awaitingInput=%v", m.inputBlocked(), m.awaitingInput)
	}
	footer := m.footerView()
	if !strings.Contains(footer, "Session limit reached") {
		t.Fatalf("footer = %q, want limit banner", footer)
	}
	if strings.Contains(footer, "Message") {
		t.Fatalf("footer = %q, should not show message input", footer)
	}
}

func TestRunTUI_statusBarView_wallClock(t *testing.T) {
	m := newRunTUI(context.Background(), &mockInteractiveClientStream{}, &runtimev1.RunSessionInteractiveStart{})
	m.width = 100
	m.maxWallClockSeconds = 60
	m.sessionStartedAt = time.Now().Add(-10 * time.Second)
	m.status = "input"
	m.lastStats = &runtimev1.InteractiveSessionStats{Turn: 1}

	bar := m.statusBarView()
	if !strings.Contains(bar, "10s / 60s") {
		t.Fatalf("statusBar = %q, want wall clock segment", bar)
	}
}

func TestRunTUI_handleServerMsg_turnWithStats(t *testing.T) {
	stream := &mockInteractiveClientStream{}
	m := newRunTUI(context.Background(), stream, &runtimev1.RunSessionInteractiveStart{})
	m.width = 100
	m.height = 30
	m.layout()

	if err := m.handleServerMsg(&runtimev1.RunSessionInteractiveServerMsg{
		Body: &runtimev1.RunSessionInteractiveServerMsg_SessionStarted{
			SessionStarted: &runtimev1.RunSessionInteractiveSessionStarted{
				SessionId:         "sess-abc",
				AgentVersionId:    "ver-123",
				ModelProvider:     "anthropic",
				ModelName:         "claude-sonnet-4-5",
				MaxTokensPerRun:   5000,
			},
		},
	}); err != nil {
		t.Fatalf("session_started: %v", err)
	}

	longJSON := `{"reply":"` + strings.Repeat("x", 120) + `","topics":["t"],"refused":false}`
	if err := m.handleServerMsg(&runtimev1.RunSessionInteractiveServerMsg{
		Body: &runtimev1.RunSessionInteractiveServerMsg_TextDelta{
			TextDelta: &runtimev1.RunSessionInteractiveTextDelta{Delta: longJSON},
		},
	}); err != nil {
		t.Fatalf("text_delta: %v", err)
	}

	stats := &runtimev1.InteractiveSessionStats{
		Turn: 1,
		TurnUsage: &runtimev1.TokenUsage{
			InputTokens:  50,
			OutputTokens: 20,
			TotalTokens:  70,
		},
		SessionUsage: &runtimev1.TokenUsage{
			InputTokens:  50,
			OutputTokens: 20,
			TotalTokens:  70,
		},
	}
	if err := m.handleServerMsg(&runtimev1.RunSessionInteractiveServerMsg{
		Body: &runtimev1.RunSessionInteractiveServerMsg_AwaitingInput{
			AwaitingInput: &runtimev1.RunSessionInteractiveAwaitingInput{
				StopReason: "end_turn",
				Stats:      stats,
			},
		},
	}); err != nil {
		t.Fatalf("awaiting_input: %v", err)
	}

	if !m.awaitingInput {
		t.Fatal("expected awaitingInput")
	}
	if m.lastStats == nil || m.lastStats.GetTurn() != 1 {
		t.Fatalf("lastStats = %+v, want turn 1", m.lastStats)
	}

	conversation := m.conversationText()
	if !strings.Contains(conversation, "#1") {
		t.Fatalf("conversation = %q, want turn badge in message panel", conversation)
	}
	if !strings.Contains(conversation, "▲") || !strings.Contains(conversation, "50") {
		t.Fatalf("conversation = %q, want token chips in message panel", conversation)
	}

	header := m.headerView()
	if !strings.Contains(header, "anthropic/claude") {
		t.Fatalf("header = %q, want model line", header)
	}

	statusBar := m.statusBarView()
	if !strings.Contains(statusBar, "Ready") {
		t.Fatalf("statusBar = %q, want Ready state", statusBar)
	}
	if !strings.Contains(statusBar, "70") {
		t.Fatalf("statusBar = %q, want session token total", statusBar)
	}
	if !strings.Contains(statusBar, "1% of limit") {
		t.Fatalf("statusBar = %q, want token limit percentage", statusBar)
	}
	if m.maxTokensPerRun != 5000 {
		t.Fatalf("maxTokensPerRun = %d, want 5000", m.maxTokensPerRun)
	}

	content := m.viewport.View()
	if maxLineWidth(content) > m.bodyContentWidth()+2 {
		t.Fatalf("viewport has lines wider than %d", m.bodyContentWidth())
	}
}

func TestRunTUI_handleServerMsg_sessionStartedWithHistory(t *testing.T) {
	m := newRunTUI(context.Background(), &mockInteractiveClientStream{}, &runtimev1.RunSessionInteractiveStart{})
	m.width = 80
	m.height = 24
	m.layout()

	err := m.handleServerMsg(&runtimev1.RunSessionInteractiveServerMsg{
		Body: &runtimev1.RunSessionInteractiveServerMsg_SessionStarted{
			SessionStarted: &runtimev1.RunSessionInteractiveSessionStarted{
				SessionId: "sess-attach",
				History: []*runtimev1.InteractiveConversationMessage{
					{Role: "user", Content: "hello"},
					{
						Role:       "assistant",
						Content:    "hi there",
						StopReason: "end_turn",
						TurnUsage: &runtimev1.TokenUsage{
							InputTokens: 12, OutputTokens: 4, TotalTokens: 16,
						},
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("session_started: %v", err)
	}
	text := m.conversationText()
	if !strings.Contains(text, "hello") || !strings.Contains(text, "hi there") {
		t.Fatalf("conversation = %q, want prior turns", text)
	}
	if !strings.Contains(text, "▲") || !strings.Contains(text, "12") {
		t.Fatalf("conversation = %q, want turn token chips from history", text)
	}
}

func TestRunTUI_layout_viewportMatchesBox(t *testing.T) {
	m := newRunTUI(context.Background(), &mockInteractiveClientStream{}, &runtimev1.RunSessionInteractiveStart{})
	m.width = 80
	m.height = 24
	m.lastStats = &runtimev1.InteractiveSessionStats{Turn: 1}
	m.layout()

	if m.viewport.Width != m.bodyContentWidth() {
		t.Fatalf("viewport.Width = %d, bodyContentWidth = %d", m.viewport.Width, m.bodyContentWidth())
	}
	if m.bodyContentWidth() != 72 {
		t.Fatalf("bodyContentWidth() = %d, want 72", m.bodyContentWidth())
	}
	if m.messageContentWidth() != 70 {
		t.Fatalf("messageContentWidth() = %d, want 70", m.messageContentWidth())
	}
}

func TestRunTUI_sendUserMessage_clearsStatsAndStreams(t *testing.T) {
	stream := &mockInteractiveClientStream{}
	m := newRunTUI(context.Background(), stream, &runtimev1.RunSessionInteractiveStart{})
	m.width = 80
	m.height = 24
	m.awaitingInput = true
	m.lastStats = &runtimev1.InteractiveSessionStats{
		Turn: 1,
		TurnUsage: &runtimev1.TokenUsage{
			InputTokens: 1, OutputTokens: 1, TotalTokens: 2,
		},
	}
	m.input.Focus()

	if err := m.sendUserMessage("hello"); err != nil {
		t.Fatalf("sendUserMessage: %v", err)
	}
	if m.lastStats != nil {
		t.Fatalf("lastStats = %+v, want cleared while streaming", m.lastStats)
	}
	if m.status != "streaming" {
		t.Fatalf("status = %q, want streaming", m.status)
	}
	if len(stream.sent) != 1 || stream.sent[0].GetUserMessage().GetText() != "hello" {
		t.Fatalf("sent = %+v, want user_message hello", stream.sent)
	}
}

func TestRunTUI_Update_windowSizeAndEmptyEnter(t *testing.T) {
	m := newRunTUI(context.Background(), &mockInteractiveClientStream{}, &runtimev1.RunSessionInteractiveStart{})
	m.awaitingInput = true
	m.input.Focus()

	model, _ := m.Update(tea.WindowSizeMsg{Width: 60, Height: 20})
	m = model.(*runTUI)
	if m.width != 60 || m.height != 20 {
		t.Fatalf("size = %dx%d, want 60x20", m.width, m.height)
	}

	m.input.SetValue("   ")
	model, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = model.(*runTUI)
	if !strings.Contains(m.statusBarView(), "Type a message") {
		t.Fatalf("statusBar = %q, want empty message hint", m.statusBarView())
	}
}

func TestRunTUI_scrollWhileInput_preservesHistory(t *testing.T) {
	m := newRunTUI(context.Background(), &mockInteractiveClientStream{}, &runtimev1.RunSessionInteractiveStart{})
	m.width = 40
	m.height = 12
	m.awaitingInput = true
	m.input.Focus()
	for i := 0; i < 40; i++ {
		m.transcript.WriteString(fmt.Sprintf("line %d\n", i))
	}
	m.layout()

	model, _ := m.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	m = model.(*runTUI)
	if m.followTail {
		t.Fatal("expected followTail false after scrolling up")
	}
	if m.viewport.AtBottom() {
		t.Fatal("expected viewport not at bottom after pgup")
	}

	model, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlEnd})
	m = model.(*runTUI)
	if !m.followTail || !m.viewport.AtBottom() {
		t.Fatal("expected jump to latest to re-enable follow tail")
	}
}

func TestRunTUI_refreshViewport_respectsFollowTail(t *testing.T) {
	m := newRunTUI(context.Background(), &mockInteractiveClientStream{}, &runtimev1.RunSessionInteractiveStart{})
	m.width = 40
	m.height = 12
	for i := 0; i < 30; i++ {
		m.transcript.WriteString(fmt.Sprintf("line %d\n", i))
	}
	m.layout()
	m.followTail = false
	m.viewport.LineUp(5)
	offset := m.viewport.YOffset

	m.transcript.WriteString("new line\n")
	m.refreshViewport()
	if m.viewport.YOffset != offset {
		t.Fatalf("YOffset = %d, want %d when not following tail", m.viewport.YOffset, offset)
	}

	m.followTail = true
	m.refreshViewport()
	if !m.viewport.AtBottom() {
		t.Fatal("expected GotoBottom when followTail is true")
	}
}

func TestTuiScrollWhileInput_doesNotCaptureSpace(t *testing.T) {
	if tuiScrollWhileInput(tea.KeyMsg{Type: tea.KeySpace}) {
		t.Fatal("space should be typed into the message box, not scroll the chat")
	}
}

func TestRunTUI_viewFitsTerminalWithInputFooter(t *testing.T) {
	m := newRunTUI(context.Background(), &mockInteractiveClientStream{}, &runtimev1.RunSessionInteractiveStart{})
	m.width = 80
	m.height = 24
	m.sessionID = "sess-1"
	m.agentVersionID = "ver-1"
	m.modelProvider = "anthropic"
	m.modelName = "claude"
	m.awaitingInput = true
	m.input.Focus()
	m.layout()

	view := m.View()
	if got := lipgloss.Height(view); got > m.height {
		t.Fatalf("view height = %d, terminal height = %d", got, m.height)
	}
}

func TestRunTUI_View_includesWrappedBody(t *testing.T) {
	m := newRunTUI(context.Background(), &mockInteractiveClientStream{}, &runtimev1.RunSessionInteractiveStart{})
	m.width = 50
	m.height = 20
	m.sessionID = "sess-1"
	m.layout()
	m.transcript.WriteString(renderAgentBlock(m.messageContentWidth(), "AGENT", strings.Repeat("word ", 30), nil))
	m.refreshViewport()

	view := m.View()
	if !strings.Contains(view, "session sess-1") {
		t.Fatalf("view = %q, want session header", view)
	}
	if strings.Count(view, "\n") < 3 {
		t.Fatalf("view should span multiple lines, got %q", view)
	}
}
