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

func TestRunTUI_wallClockBlockedFreezesDisplay(t *testing.T) {
	m := newRunTUI(context.Background(), &mockInteractiveClientStream{}, &runtimev1.RunSessionInteractiveStart{}, nil)
	m.width = 100
	m.maxWallClockSeconds = 60
	m.sessionStartedAt = time.Now().Add(-90 * time.Second)

	if err := m.handleServerMsg(&runtimev1.RunSessionInteractiveServerMsg{
		Body: &runtimev1.RunSessionInteractiveServerMsg_AwaitingInput{
			AwaitingInput: &runtimev1.RunSessionInteractiveAwaitingInput{
				InputBlockedReason: "run limit max_wall_clock_seconds exceeded (on_limit=halt)",
				Stats: &runtimev1.InteractiveSessionStats{
					Turn: 1,
					SessionUsage: &runtimev1.TokenUsage{
						InputTokens: 10, OutputTokens: 5, TotalTokens: 15,
					},
				},
			},
		},
	}); err != nil {
		t.Fatalf("awaiting_input: %v", err)
	}
	bar := m.statusBarView()
	if !strings.Contains(bar, "60s / 60s") {
		t.Fatalf("statusBar = %q, want wall clock capped at limit", bar)
	}
	model, cmd := m.Update(tuiClockTick{})
	m = model.(*runTUI)
	if cmd != nil {
		t.Fatal("clock tick should stop when wall-clock limit is reached")
	}
	if m.statusBarView() != bar {
		t.Fatalf("statusBar changed after tick: %q -> %q", bar, m.statusBarView())
	}
}

func TestRunTUI_handleServerMsg_approvalRequired(t *testing.T) {
	stream := &mockInteractiveClientStream{}
	m := newRunTUI(context.Background(), stream, &runtimev1.RunSessionInteractiveStart{}, nil)
	m.width = 80
	m.height = 24
	m.layout()

	ar := &runtimev1.RunSessionInteractiveApprovalRequired{
		ApprovalId: "appr-1",
		CallId:     "call-abc",
		Tool:       "payments.process-payment",
		Version:    "1.0.0",
		Reason:     "amount exceeds threshold",
		Args:       []byte(`{"amount":1500}`),
	}
	if err := m.handleServerMsg(&runtimev1.RunSessionInteractiveServerMsg{
		Body: &runtimev1.RunSessionInteractiveServerMsg_ApprovalRequired{
			ApprovalRequired: ar,
		},
	}); err != nil {
		t.Fatalf("approval_required: %v", err)
	}
	if !m.awaitingApprovalDecision() {
		t.Fatal("expected awaiting approval decision")
	}
	if m.status != "approval" {
		t.Fatalf("status = %q, want approval", m.status)
	}
	conv := m.conversationText()
	for _, want := range []string{"APPROVAL REQUIRED", "appr-1", "payments.process-payment", "1500"} {
		if !strings.Contains(conv, want) {
			t.Fatalf("conversation = %q, want %q", conv, want)
		}
	}
	footer := m.footerView()
	if !strings.Contains(footer, "Tool approval required") {
		t.Fatalf("footer = %q, want approval panel", footer)
	}
	if !strings.Contains(footer, "A approve") {
		t.Fatalf("footer = %q, want approve help", footer)
	}
	if strings.Contains(footer, "Message") {
		t.Fatalf("footer = %q, should not show message input", footer)
	}
}

func TestRunTUI_sendToolApproval_approveAndDeny(t *testing.T) {
	stream := &mockInteractiveClientStream{}
	m := newRunTUI(context.Background(), stream, &runtimev1.RunSessionInteractiveStart{}, nil)
	m.width = 80
	m.height = 24
	m.applyAwaitingApprovalState(&runtimev1.RunSessionInteractiveApprovalRequired{
		ApprovalId: "appr-1",
		Tool:       "danger.op",
	})

	if err := m.sendToolApproval(true); err != nil {
		t.Fatalf("approve: %v", err)
	}
	if len(stream.sent) != 1 {
		t.Fatalf("sent = %d messages, want 1", len(stream.sent))
	}
	ta := stream.sent[0].GetToolApproval()
	if ta == nil || !ta.GetApproved() || ta.GetApprovalId() != "appr-1" {
		t.Fatalf("sent = %+v, want approved tool_approval", stream.sent[0])
	}
	if m.awaitingApprovalDecision() {
		t.Fatal("expected approval cleared after full quorum")
	}
	if m.status != "streaming" {
		t.Fatalf("status = %q, want streaming", m.status)
	}

	m.applyAwaitingApprovalState(&runtimev1.RunSessionInteractiveApprovalRequired{
		ApprovalId: "appr-2",
		Tool:       "danger.op",
	})
	if err := m.sendToolApproval(false); err != nil {
		t.Fatalf("deny: %v", err)
	}
	if len(stream.sent) != 2 {
		t.Fatalf("sent = %d messages, want 2", len(stream.sent))
	}
	ta = stream.sent[1].GetToolApproval()
	if ta == nil || ta.GetApproved() {
		t.Fatalf("sent = %+v, want denied tool_approval", stream.sent[1])
	}
	if m.awaitingApprovalDecision() {
		t.Fatal("expected approval cleared after deny")
	}
}

func TestRunTUI_sendToolApproval_partialQuorum(t *testing.T) {
	stream := &mockInteractiveClientStream{}
	m := newRunTUI(context.Background(), stream, &runtimev1.RunSessionInteractiveStart{}, nil)
	m.applyAwaitingApprovalState(&runtimev1.RunSessionInteractiveApprovalRequired{
		ApprovalId:         "appr-1",
		Tool:               "danger.op",
		ApprovalsRequired:  2,
		ApprovalsReceived: 0,
	})

	if err := m.sendToolApproval(true); err != nil {
		t.Fatalf("approve: %v", err)
	}
	if !m.awaitingApprovalDecision() {
		t.Fatal("expected still awaiting approval after partial quorum")
	}
	if m.pendingApproval.GetApprovalsReceived() != 1 {
		t.Fatalf("approvals_received = %d, want 1", m.pendingApproval.GetApprovalsReceived())
	}
	if !strings.Contains(m.statusHint, "1/2") {
		t.Fatalf("statusHint = %q, want partial quorum hint", m.statusHint)
	}
}

func TestRunTUI_scheduleStreamRecv_singleInFlight(t *testing.T) {
	m := newRunTUI(context.Background(), &mockInteractiveClientStream{}, &runtimev1.RunSessionInteractiveStart{}, nil)
	if cmd := m.scheduleStreamRecv(); cmd == nil {
		t.Fatal("expected first scheduleStreamRecv to return a cmd")
	}
	if !m.streamRecvInFlight {
		t.Fatal("expected streamRecvInFlight after schedule")
	}
	if cmd := m.scheduleStreamRecv(); cmd != nil {
		t.Fatal("expected duplicate scheduleStreamRecv to return nil")
	}
}

func TestRunTUI_textDeltaOrder_viaUpdatePump(t *testing.T) {
	want := "You're welcome! If you need any more assistance, feel free to ask."
	deltas := []string{"You're ", "welcome", "! If you need any more assistance, feel free to ask."}
	var recv []*runtimev1.RunSessionInteractiveServerMsg
	recv = append(recv, &runtimev1.RunSessionInteractiveServerMsg{
		Body: &runtimev1.RunSessionInteractiveServerMsg_SessionStarted{
			SessionStarted: &runtimev1.RunSessionInteractiveSessionStarted{SessionId: "sess-1"},
		},
	})
	for _, d := range deltas {
		recv = append(recv, &runtimev1.RunSessionInteractiveServerMsg{
			Body: &runtimev1.RunSessionInteractiveServerMsg_TextDelta{
				TextDelta: &runtimev1.RunSessionInteractiveTextDelta{Delta: d},
			},
		})
	}
	recv = append(recv, &runtimev1.RunSessionInteractiveServerMsg{
		Body: &runtimev1.RunSessionInteractiveServerMsg_AwaitingInput{
			AwaitingInput: &runtimev1.RunSessionInteractiveAwaitingInput{StopReason: "end_turn"},
		},
	})
	stream := &mockInteractiveClientStream{recv: recv}
	m := newRunTUI(context.Background(), stream, &runtimev1.RunSessionInteractiveStart{}, nil)
	m.width = 100
	m.height = 24
	m.layout()

	model, _ := m.Update(streamServerMsg{msg: recv[0]})
	m = model.(*runTUI)
	for i := 1; i <= len(deltas); i++ {
		model, _ = m.Update(streamServerMsg{msg: recv[i]})
		m = model.(*runTUI)
	}
	model, _ = m.Update(streamServerMsg{msg: recv[len(recv)-1]})
	m = model.(*runTUI)

	conv := m.conversationText()
	if !strings.Contains(conv, want) {
		t.Fatalf("conversation = %q, want ordered assistant text %q", conv, want)
	}
}

func TestRunTUI_Update_approvalKeys(t *testing.T) {
	stream := &mockInteractiveClientStream{}
	m := newRunTUI(context.Background(), stream, &runtimev1.RunSessionInteractiveStart{}, nil)
	m.width = 80
	m.height = 24
	m.applyAwaitingApprovalState(&runtimev1.RunSessionInteractiveApprovalRequired{
		ApprovalId: "appr-1",
		Tool:       "danger.op",
	})

	m.streamRecvInFlight = true // recv already blocked after approval_required
	model, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	m = model.(*runTUI)
	if cmd != nil {
		t.Fatal("expected no second recv after approve while stream recv is in flight")
	}
	if len(stream.sent) != 1 || !stream.sent[0].GetToolApproval().GetApproved() {
		t.Fatalf("sent = %+v, want approve", stream.sent)
	}
}

func TestRunTUI_handleServerMsg_toolCallAndResult(t *testing.T) {
	m := newRunTUI(context.Background(), &mockInteractiveClientStream{}, &runtimev1.RunSessionInteractiveStart{}, nil)
	m.width = 80
	m.height = 24
	m.layout()

	if err := m.handleServerMsg(&runtimev1.RunSessionInteractiveServerMsg{
		Body: &runtimev1.RunSessionInteractiveServerMsg_ToolCall{
			ToolCall: &runtimev1.RunSessionInteractiveToolCall{
				CallId:  "call-1",
				Tool:    "weather.get-forecast",
				Version: "1.0.0",
				Args:    []byte(`{"city":"Boston"}`),
			},
		},
	}); err != nil {
		t.Fatalf("tool_call: %v", err)
	}
	if m.status != "streaming" {
		t.Fatalf("status = %q, want streaming", m.status)
	}
	if !strings.Contains(m.statusHint, "weather.get-forecast") {
		t.Fatalf("statusHint = %q, want tool name", m.statusHint)
	}
	conv := m.conversationText()
	for _, want := range []string{"TOOL CALL", "call-1", "weather.get-forecast@1.0.0", "Boston"} {
		if !strings.Contains(conv, want) {
			t.Fatalf("conversation = %q, want %q in transcript", conv, want)
		}
	}

	if err := m.handleServerMsg(&runtimev1.RunSessionInteractiveServerMsg{
		Body: &runtimev1.RunSessionInteractiveServerMsg_ToolResult{
			ToolResult: &runtimev1.RunSessionInteractiveToolResult{
				CallId:  "call-1",
				Payload: []byte(`{"temp":72}`),
			},
		},
	}); err != nil {
		t.Fatalf("tool_result: %v", err)
	}
	if !strings.Contains(m.statusHint, "tool result") {
		t.Fatalf("statusHint = %q, want result hint", m.statusHint)
	}
	conv = m.conversationText()
	for _, want := range []string{"TOOL RESULT", "call-1", "72"} {
		if !strings.Contains(conv, want) {
			t.Fatalf("conversation = %q, want %q after tool result", conv, want)
		}
	}
}

func TestRunTUI_handleServerMsg_inputBlocked(t *testing.T) {
	m := newRunTUI(context.Background(), &mockInteractiveClientStream{}, &runtimev1.RunSessionInteractiveStart{}, nil)
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
	m := newRunTUI(context.Background(), &mockInteractiveClientStream{}, &runtimev1.RunSessionInteractiveStart{}, nil)
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

func TestRunTUI_statusBarView_wallClockFrozenWhenEnded(t *testing.T) {
	start := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	ended := start.Add(18 * time.Second)

	m := newRunTUI(context.Background(), &mockInteractiveClientStream{}, &runtimev1.RunSessionInteractiveStart{}, nil)
	m.width = 100
	m.maxWallClockSeconds = 60
	m.sessionStartedAt = start
	m.sessionEndedAt = ended
	m.status = "done"
	m.readOnly = true

	bar := m.statusBarView()
	if !strings.Contains(bar, "18s / 60s") {
		t.Fatalf("statusBar = %q, want frozen 18s elapsed", bar)
	}
	if strings.Contains(bar, "30s / 60s") {
		t.Fatalf("statusBar = %q, wall clock should not use current time", bar)
	}
}

func TestRunTUI_handleServerMsg_turnWithStats(t *testing.T) {
	stream := &mockInteractiveClientStream{}
	m := newRunTUI(context.Background(), stream, &runtimev1.RunSessionInteractiveStart{}, nil)
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
	m := newRunTUI(context.Background(), &mockInteractiveClientStream{}, &runtimev1.RunSessionInteractiveStart{}, nil)
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

func TestRunTUI_attachCompletedReadOnly(t *testing.T) {
	stream := &mockInteractiveClientStream{}
	m := newRunTUI(context.Background(), stream, &runtimev1.RunSessionInteractiveStart{SessionId: "sess-done"}, nil)
	m.width = 80
	m.height = 24
	m.layout()

	endedAt := time.Date(2026, 3, 1, 12, 0, 18, 0, time.UTC)
	startedAt := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	if err := m.handleServerMsg(&runtimev1.RunSessionInteractiveServerMsg{
		Body: &runtimev1.RunSessionInteractiveServerMsg_SessionStarted{
			SessionStarted: &runtimev1.RunSessionInteractiveSessionStarted{
				SessionId:              "sess-done",
				SessionStartedAtUnixMs: startedAt.UnixMilli(),
				SessionEndedAtUnixMs:   endedAt.UnixMilli(),
				MaxWallClockSeconds:    60,
				History: []*runtimev1.InteractiveConversationMessage{
					{Role: "user", Content: "hello"},
					{Role: "assistant", Content: "done"},
				},
			},
		},
	}); err != nil {
		t.Fatalf("session_started: %v", err)
	}
	if err := m.handleServerMsg(&runtimev1.RunSessionInteractiveServerMsg{
		Body: &runtimev1.RunSessionInteractiveServerMsg_Completed{
			Completed: &runtimev1.RunSessionInteractiveCompleted{
				StopReason: "end_turn",
				Stats: &runtimev1.InteractiveSessionStats{
					Turn: 1,
					SessionUsage: &runtimev1.TokenUsage{
						InputTokens: 10, OutputTokens: 5, TotalTokens: 15,
					},
				},
				SessionEndedAtUnixMs: endedAt.UnixMilli(),
			},
		},
	}); err != nil {
		t.Fatalf("completed: %v", err)
	}
	if !m.sessionEndedAt.Equal(endedAt) {
		t.Fatalf("sessionEndedAt = %v, want %v", m.sessionEndedAt, endedAt)
	}
	if !strings.Contains(m.statusBarView(), "18s / 60s") {
		t.Fatalf("statusBar = %q, want frozen wall clock from session end", m.statusBarView())
	}
	if !strings.Contains(m.statusBarView(), "session") || !strings.Contains(m.statusBarView(), "10") {
		t.Fatalf("statusBar = %q, want session token usage", m.statusBarView())
	}
	if !m.readOnly {
		t.Fatal("expected readOnly attach replay")
	}
	if m.awaitingInput || m.input.Focused() {
		t.Fatal("input should stay disabled on completed attach")
	}
	if !m.sendClosed {
		t.Fatal("expected stream send closed after read-only completed")
	}

	model, cmd := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = model.(*runTUI)
	if m.quitting {
		t.Fatal("read-only completed attach should stay open for review")
	}
	if cmd != nil {
		t.Fatal("expected no recv cmd after window resize on read-only completed")
	}
	if !strings.Contains(m.footerView(), "scroll to review") {
		t.Fatalf("footer = %q, want scroll help", m.footerView())
	}
}

func TestRunTUI_attachCancelledReadOnly(t *testing.T) {
	stream := &mockInteractiveClientStream{}
	m := newRunTUI(context.Background(), stream, &runtimev1.RunSessionInteractiveStart{SessionId: "sess-cancelled"}, nil)
	m.width = 80
	m.height = 24
	m.layout()

	endedAt := time.Date(2026, 3, 1, 12, 0, 18, 0, time.UTC)
	startedAt := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	if err := m.handleServerMsg(&runtimev1.RunSessionInteractiveServerMsg{
		Body: &runtimev1.RunSessionInteractiveServerMsg_SessionStarted{
			SessionStarted: &runtimev1.RunSessionInteractiveSessionStarted{
				SessionId:              "sess-cancelled",
				SessionStartedAtUnixMs: startedAt.UnixMilli(),
				SessionEndedAtUnixMs:   endedAt.UnixMilli(),
				MaxWallClockSeconds:    60,
				History: []*runtimev1.InteractiveConversationMessage{
					{Role: "user", Content: "hello"},
					{Role: "assistant", Content: "partial"},
				},
			},
		},
	}); err != nil {
		t.Fatalf("session_started: %v", err)
	}
	if err := m.handleServerMsg(&runtimev1.RunSessionInteractiveServerMsg{
		Body: &runtimev1.RunSessionInteractiveServerMsg_Cancelled{
			Cancelled: &runtimev1.RunSessionInteractiveCancelled{
				SessionEndedAtUnixMs: endedAt.UnixMilli(),
			},
		},
	}); err != nil {
		t.Fatalf("cancelled: %v", err)
	}
	if !m.sessionEndedAt.Equal(endedAt) {
		t.Fatalf("sessionEndedAt = %v, want %v", m.sessionEndedAt, endedAt)
	}
	if !strings.Contains(m.statusBarView(), "18s / 60s") {
		t.Fatalf("statusBar = %q, want frozen wall clock from session end", m.statusBarView())
	}
	if m.status != "cancelled" {
		t.Fatalf("status = %q, want cancelled", m.status)
	}
	if !strings.Contains(m.statusBarView(), "Cancelled") {
		t.Fatalf("statusBar = %q, want cancelled indicator", m.statusBarView())
	}
	if !m.readOnly {
		t.Fatal("expected readOnly attach replay")
	}
	if m.awaitingInput || m.input.Focused() {
		t.Fatal("input should stay disabled on cancelled attach")
	}
	if !m.sendClosed {
		t.Fatal("expected stream send closed after read-only cancelled")
	}

	model, cmd := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = model.(*runTUI)
	if m.quitting {
		t.Fatal("read-only cancelled attach should stay open for review")
	}
	if cmd != nil {
		t.Fatal("expected no recv cmd after window resize on read-only cancelled")
	}
	if !strings.Contains(m.footerView(), "scroll to review") {
		t.Fatalf("footer = %q, want scroll help", m.footerView())
	}
}

func TestRunTUI_attachFailedReadOnlyWallClockFrozen(t *testing.T) {
	stream := &mockInteractiveClientStream{}
	m := newRunTUI(context.Background(), stream, &runtimev1.RunSessionInteractiveStart{SessionId: "sess-failed"}, nil)
	m.width = 80
	m.height = 24
	m.layout()

	startedAt := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	endedAt := startedAt.Add(18 * time.Second)
	if err := m.handleServerMsg(&runtimev1.RunSessionInteractiveServerMsg{
		Body: &runtimev1.RunSessionInteractiveServerMsg_SessionStarted{
			SessionStarted: &runtimev1.RunSessionInteractiveSessionStarted{
				SessionId:              "sess-failed",
				SessionStartedAtUnixMs: startedAt.UnixMilli(),
				SessionEndedAtUnixMs:   endedAt.UnixMilli(),
				MaxWallClockSeconds:    60,
				History: []*runtimev1.InteractiveConversationMessage{
					{Role: "user", Content: "hello"},
					{Role: "assistant", Content: "done"},
				},
			},
		},
	}); err != nil {
		t.Fatalf("session_started: %v", err)
	}
	if err := m.handleServerMsg(&runtimev1.RunSessionInteractiveServerMsg{
		Body: &runtimev1.RunSessionInteractiveServerMsg_Failed{
			Failed: &runtimev1.RunSessionInteractiveFailed{
				Message: "run limit max_wall_clock_seconds exceeded (on_limit=halt)",
			},
		},
	}); err != nil {
		t.Fatalf("failed: %v", err)
	}
	if !m.sessionEndedAt.Equal(endedAt) {
		t.Fatalf("sessionEndedAt = %v, want %v", m.sessionEndedAt, endedAt)
	}
	if !strings.Contains(m.statusBarView(), "18s / 60s") {
		t.Fatalf("statusBar = %q, want frozen wall clock", m.statusBarView())
	}
	barBefore := m.statusBarView()
	model, cmd := m.Update(tuiClockTick{})
	m = model.(*runTUI)
	if cmd != nil {
		t.Fatal("clock tick should stop on read-only failed attach")
	}
	if m.statusBarView() != barBefore {
		t.Fatalf("statusBar changed after tick: %q -> %q", barBefore, m.statusBarView())
	}
}

func TestRunTUI_layout_viewportMatchesBox(t *testing.T) {
	m := newRunTUI(context.Background(), &mockInteractiveClientStream{}, &runtimev1.RunSessionInteractiveStart{}, nil)
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
	m := newRunTUI(context.Background(), stream, &runtimev1.RunSessionInteractiveStart{}, nil)
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
	m := newRunTUI(context.Background(), &mockInteractiveClientStream{}, &runtimev1.RunSessionInteractiveStart{}, nil)
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

func TestRunTUI_mouseWheelScroll_disablesFollowTail(t *testing.T) {
	m := newRunTUI(context.Background(), &mockInteractiveClientStream{}, &runtimev1.RunSessionInteractiveStart{}, nil)
	m.width = 40
	m.height = 12
	for i := 0; i < 40; i++ {
		m.parentEntry().transcript.WriteString(fmt.Sprintf("line %d\n", i))
	}
	m.layout()
	if !m.followTail || !m.viewport.AtBottom() {
		t.Fatal("expected initial viewport at bottom with followTail")
	}

	wheelUp := tea.MouseMsg{
		Action: tea.MouseActionPress,
		Button: tea.MouseButtonWheelUp,
		Type:   tea.MouseWheelUp,
	}
	model, _ := m.Update(wheelUp)
	m = model.(*runTUI)
	if m.followTail {
		t.Fatal("expected followTail false after mouse wheel scroll up")
	}
	if m.viewport.AtBottom() {
		t.Fatal("expected viewport not at bottom after wheel up")
	}

	offset := m.viewport.YOffset
	model, _ = m.Update(tuiClockTick{})
	m = model.(*runTUI)
	if m.viewport.YOffset != offset {
		t.Fatalf("YOffset = %d, want %d after clock tick while not following tail", m.viewport.YOffset, offset)
	}
}

func TestRunTUI_scrollWhileInput_preservesHistory(t *testing.T) {
	m := newRunTUI(context.Background(), &mockInteractiveClientStream{}, &runtimev1.RunSessionInteractiveStart{}, nil)
	m.width = 40
	m.height = 12
	m.awaitingInput = true
	m.input.Focus()
	for i := 0; i < 40; i++ {
		m.parentEntry().transcript.WriteString(fmt.Sprintf("line %d\n", i))
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
	m := newRunTUI(context.Background(), &mockInteractiveClientStream{}, &runtimev1.RunSessionInteractiveStart{}, nil)
	m.width = 40
	m.height = 12
	for i := 0; i < 30; i++ {
		m.parentEntry().transcript.WriteString(fmt.Sprintf("line %d\n", i))
	}
	m.layout()
	m.followTail = false
	m.viewport.LineUp(5)
	offset := m.viewport.YOffset

	m.parentEntry().transcript.WriteString("new line\n")
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
	m := newRunTUI(context.Background(), &mockInteractiveClientStream{}, &runtimev1.RunSessionInteractiveStart{}, nil)
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
	m := newRunTUI(context.Background(), &mockInteractiveClientStream{}, &runtimev1.RunSessionInteractiveStart{}, nil)
	m.width = 50
	m.height = 20
	m.sessionID = "sess-1"
	m.layout()
	m.parentEntry().transcript.WriteString(renderAgentBlock(m.messageContentWidth(), "AGENT", strings.Repeat("word ", 30), nil))
	m.refreshViewport()

	view := m.View()
	if !strings.Contains(view, "session sess-1") {
		t.Fatalf("view = %q, want session header", view)
	}
	if strings.Count(view, "\n") < 3 {
		t.Fatalf("view should span multiple lines, got %q", view)
	}
}

func countTUIHelpLines(s string) int {
	n := 0
	for _, line := range strings.Split(s, "\n") {
		if strings.Contains(line, "PgUp/PgDn scroll") {
			n++
		}
	}
	return n
}

func TestRunTUI_footerHelpNotDuplicated(t *testing.T) {
	m := newRunTUI(context.Background(), &mockInteractiveClientStream{}, &runtimev1.RunSessionInteractiveStart{}, nil)
	for _, width := range []int{40, 80, 100, 120} {
		m.width = width
		m.height = 24
		m.awaitingInput = true
		m.delegationPaneVisible = true
		m.input.Focus()
		m.layout()
		footer := m.footerView()
		if c := strings.Count(footer, "Enter send"); c != 1 {
			t.Fatalf("width=%d footer Enter send count = %d, footer:\n%s", width, c, footer)
		}
		if c := countTUIHelpLines(footer); c != 1 {
			t.Fatalf("width=%d footer help lines = %d, footer:\n%s", width, c, footer)
		}
		view := m.View()
		if c := strings.Count(view, "Enter send"); c != 1 {
			t.Fatalf("width=%d view Enter send count = %d", width, c)
		}
		if c := countTUIHelpLines(view); c != 1 {
			t.Fatalf("width=%d view help lines = %d", width, c)
		}
	}
}

func TestRunTUI_footerHelpNotDuplicatedOnShortTerminal(t *testing.T) {
	m := newRunTUI(context.Background(), &mockInteractiveClientStream{}, &runtimev1.RunSessionInteractiveStart{}, nil)
	m.width = 120
	m.height = 10
	m.awaitingInput = true
	m.delegationPaneVisible = true
	m.input.Focus()
	m.layout()
	view := m.View()
	if c := strings.Count(view, "Enter send"); c != 1 {
		t.Fatalf("short terminal Enter send count = %d", c)
	}
	if c := countTUIHelpLines(view); c != 1 {
		t.Fatalf("short terminal help lines = %d", c)
	}
}
