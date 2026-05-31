package main

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
)

func TestRunTUI_handleServerMsg_turnWithStats(t *testing.T) {
	stream := &mockInteractiveClientStream{}
	m := newRunTUI(context.Background(), stream, &runtimev1.RunSessionInteractiveStart{})
	m.width = 100
	m.height = 30
	m.layout()

	if err := m.handleServerMsg(&runtimev1.RunSessionInteractiveServerMsg{
		Body: &runtimev1.RunSessionInteractiveServerMsg_SessionStarted{
			SessionStarted: &runtimev1.RunSessionInteractiveSessionStarted{
				SessionId:      "sess-abc",
				AgentVersionId: "ver-123",
				ModelProvider:  "anthropic",
				ModelName:      "claude-sonnet-4-5",
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
	if !strings.Contains(m.statsLine, "turn 1") {
		t.Fatalf("statsLine = %q, want turn 1", m.statsLine)
	}
	if !strings.Contains(m.statsLine, "turn tokens") {
		t.Fatalf("statsLine = %q, want turn tokens", m.statsLine)
	}

	header := m.headerView()
	if !strings.Contains(header, "anthropic/claude") {
		t.Fatalf("header = %q, want model line", header)
	}
	if !strings.Contains(header, "50 in / 20 out") {
		t.Fatalf("header = %q, want token counts", header)
	}

	content := m.viewport.View()
	if maxLineWidth(content) > m.bodyContentWidth()+2 {
		t.Fatalf("viewport has lines wider than %d", m.bodyContentWidth())
	}
}

func TestRunTUI_layout_viewportMatchesBox(t *testing.T) {
	m := newRunTUI(context.Background(), &mockInteractiveClientStream{}, &runtimev1.RunSessionInteractiveStart{})
	m.width = 80
	m.height = 24
	m.statsLine = "turn 1 · stop_reason=end_turn"
	m.layout()

	if m.viewport.Width != m.bodyContentWidth() {
		t.Fatalf("viewport.Width = %d, bodyContentWidth = %d", m.viewport.Width, m.bodyContentWidth())
	}
	if m.bodyContentWidth() != 76 {
		t.Fatalf("bodyContentWidth() = %d, want 76", m.bodyContentWidth())
	}
}

func TestRunTUI_sendUserMessage_clearsStatsAndStreams(t *testing.T) {
	stream := &mockInteractiveClientStream{}
	m := newRunTUI(context.Background(), stream, &runtimev1.RunSessionInteractiveStart{})
	m.width = 80
	m.height = 24
	m.awaitingInput = true
	m.statsLine = "turn 1 · turn tokens: 1 in / 1 out (2 total)"
	m.input.Focus()

	if err := m.sendUserMessage("hello"); err != nil {
		t.Fatalf("sendUserMessage: %v", err)
	}
	if m.statsLine != "" {
		t.Fatalf("statsLine = %q, want cleared while streaming", m.statsLine)
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
	if !strings.Contains(m.statsLine, "empty message") {
		t.Fatalf("statsLine = %q, want empty message hint", m.statsLine)
	}
}

func TestRunTUI_View_includesWrappedBody(t *testing.T) {
	m := newRunTUI(context.Background(), &mockInteractiveClientStream{}, &runtimev1.RunSessionInteractiveStart{})
	m.width = 50
	m.height = 20
	m.sessionID = "sess-1"
	m.layout()
	m.transcript.WriteString("Assistant\n")
	m.transcript.WriteString(strings.Repeat("word ", 30))
	m.refreshViewport()

	view := m.View()
	if !strings.Contains(view, "Session") {
		t.Fatalf("view = %q, want session header", view)
	}
	if strings.Count(view, "\n") < 3 {
		t.Fatalf("view should span multiple lines, got %q", view)
	}
}
