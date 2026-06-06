package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
	"github.com/phrony-platform/runtime/internal/clierr"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestRunTUI_childSessionShowsDelegationInput(t *testing.T) {
	m := newRunTUI(context.Background(), &mockInteractiveClientStream{}, &runtimev1.RunSessionInteractiveStart{}, nil)
	m.width = 120
	m.height = 24
	m.layout()

	tc := &runtimev1.RunSessionInteractiveToolCall{
		CallId:           "call-delegate",
		Args:             []byte(`{"task":"Explain quantum entanglement"}`),
		AgentDelegation:  true,
		ChildSessionId:   "run_child_1",
		DelegationTarget: "playground.explainer@1.0.0",
	}
	if err := m.handleServerMsg(&runtimev1.RunSessionInteractiveServerMsg{
		Body: &runtimev1.RunSessionInteractiveServerMsg_ToolCall{ToolCall: tc},
	}); err != nil {
		t.Fatalf("tool_call: %v", err)
	}
	m.sessions.selectedIdx = m.sessions.byID["run_child_1"]
	m.refreshViewport()
	conv := m.conversationText()
	if !strings.Contains(conv, "Explain quantum entanglement") {
		t.Fatalf("conversation = %q, want delegated task as child input", conv)
	}
	if !strings.Contains(conv, "YOU") {
		t.Fatalf("conversation = %q, want user-style input block", conv)
	}
}

func TestRunTUI_childSessionInputSkippedWhenHistoryHasUser(t *testing.T) {
	m := newRunTUI(context.Background(), &mockInteractiveClientStream{}, &runtimev1.RunSessionInteractiveStart{}, nil)
	m.width = 120
	m.sessions.registerChild("run_child_hist", "explainer@1.0.0")
	entry := m.childEntry("run_child_hist")
	entry.sessionInput = []byte(`{"task":"from delegation"}`)

	if err := m.handleChildServerMsg("run_child_hist", &runtimev1.RunSessionInteractiveServerMsg{
		Body: &runtimev1.RunSessionInteractiveServerMsg_SessionStarted{
			SessionStarted: &runtimev1.RunSessionInteractiveSessionStarted{
				SessionId: "run_child_hist",
				History: []*runtimev1.InteractiveConversationMessage{
					{Role: "user", Content: "from history"},
					{Role: "assistant", Content: "answer"},
				},
			},
		},
	}); err != nil {
		t.Fatalf("session_started: %v", err)
	}
	conv := entry.transcript.String()
	if strings.Contains(conv, "from delegation") {
		t.Fatalf("transcript = %q, should not duplicate seeded input when history has user", conv)
	}
	if !strings.Contains(conv, "from history") {
		t.Fatalf("transcript = %q, want history user message", conv)
	}
}

func TestRunTUI_handleServerMsg_delegationHidesToolBlocks(t *testing.T) {
	m := newRunTUI(context.Background(), &mockInteractiveClientStream{}, &runtimev1.RunSessionInteractiveStart{}, nil)
	m.width = 120
	m.height = 24
	m.layout()

	tc := &runtimev1.RunSessionInteractiveToolCall{
		CallId:           "call-delegate",
		Tool:             "support.explainer",
		Version:          "1.0.0",
		Args:             []byte(`{"question":"why?"}`),
		AgentDelegation:  true,
		ChildSessionId:   "run_child_1",
		DelegationTarget: "support.explainer@1.0.0",
	}
	if err := m.handleServerMsg(&runtimev1.RunSessionInteractiveServerMsg{
		Body: &runtimev1.RunSessionInteractiveServerMsg_ToolCall{ToolCall: tc},
	}); err != nil {
		t.Fatalf("tool_call: %v", err)
	}
	if !m.delegationPaneVisible {
		t.Fatal("expected sessions pane visible after delegation")
	}
	if len(m.sessions.entries) != 2 {
		t.Fatalf("sessions = %d, want parent + child", len(m.sessions.entries))
	}
	conv := m.conversationText()
	for _, want := range []string{"AGENT DELEGATION", "support.explainer@1.0.0", "call-delegate", "why?"} {
		if !strings.Contains(conv, want) {
			t.Fatalf("conversation = %q, want %q", conv, want)
		}
	}
	if strings.Contains(conv, "TOOL CALL") {
		t.Fatalf("conversation = %q, should not contain TOOL CALL", conv)
	}

	if err := m.handleServerMsg(&runtimev1.RunSessionInteractiveServerMsg{
		Body: &runtimev1.RunSessionInteractiveServerMsg_ToolResult{
			ToolResult: &runtimev1.RunSessionInteractiveToolResult{
				CallId:  "call-delegate",
				Payload: []byte(`{"output":"done"}`),
			},
		},
	}); err != nil {
		t.Fatalf("tool_result: %v", err)
	}
	conv = m.conversationText()
	if strings.Contains(conv, "TOOL RESULT") {
		t.Fatalf("conversation = %q, delegation tool result should be hidden", conv)
	}
}

func TestRunTUI_sessionsPaneFocusAndSelection(t *testing.T) {
	m := newRunTUI(context.Background(), &mockInteractiveClientStream{}, &runtimev1.RunSessionInteractiveStart{}, nil)
	m.width = 120
	m.height = 24
	m.delegationPaneVisible = true
	m.sessions.registerChild("run_child_a", "explainer@1.0.0")
	m.sessions.registerChild("run_child_b", "billing@1.0.0")
	m.layout()

	model, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	m = model.(*runTUI)
	if m.focusPane != tuiFocusSessions {
		t.Fatalf("focusPane = %v, want sessions after Ctrl+S", m.focusPane)
	}

	model, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = model.(*runTUI)
	if m.sessions.selectedIdx != 1 {
		t.Fatalf("selectedIdx = %d, want 1", m.sessions.selectedIdx)
	}

	m.parentEntry().transcript.WriteString("parent-only")
	m.childEntry("run_child_a").transcript.WriteString("child-a-only")
	m.sessions.selectedIdx = 1
	m.refreshViewport()
	if !strings.Contains(m.conversationText(), "child-a-only") {
		t.Fatalf("conversation = %q, want selected child transcript", m.conversationText())
	}

	model, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	m = model.(*runTUI)
	if m.focusPane != tuiFocusMain {
		t.Fatalf("focusPane = %v, want main after second Ctrl+S", m.focusPane)
	}
}

func TestIsChildAttachErrorRetryable(t *testing.T) {
	retryable := []string{
		"attach child session: session not found",
		"active session driver unavailable",
		"rpc error: code = FailedPrecondition desc = session is pending execution",
	}
	for _, msg := range retryable {
		if !isChildAttachErrorRetryable(errors.New(msg)) {
			t.Fatalf("expected retryable: %q", msg)
		}
	}
	if !isChildAttachErrorRetryable(status.Errorf(codes.NotFound, "session run_child not found")) {
		t.Fatal("expected gRPC NotFound to be retryable")
	}
	if isChildAttachErrorRetryable(errors.New("permission denied")) {
		t.Fatal("expected non-retryable permission error")
	}
}

func TestRunTUI_Update_childNotFoundDoesNotQuitParent(t *testing.T) {
	m := newRunTUI(context.Background(), &mockInteractiveClientStream{}, &runtimev1.RunSessionInteractiveStart{}, nil)
	m.width = 120
	m.height = 24
	m.sessions.registerChild("run_child_x", "worker@1.0.0")
	m.childStreams = map[string]*childStreamState{"run_child_x": {}}

	model, cmd := m.Update(tuiChildStreamMsg{
		childSessionID: "run_child_x",
		err:            status.Errorf(codes.NotFound, "session run_child_x not found"),
	})
	m = model.(*runTUI)
	if m.quitting {
		t.Fatal("parent TUI must not quit on child NotFound")
	}
	if m.streamErr != nil {
		t.Fatalf("streamErr = %v, want nil", m.streamErr)
	}
	if cmd == nil {
		t.Fatal("expected retry cmd after child NotFound")
	}
}

func TestRunTUI_attachChildStream_preservesAttachAttempts(t *testing.T) {
	m := newRunTUI(context.Background(), &mockInteractiveClientStream{}, &runtimev1.RunSessionInteractiveStart{}, nil)
	m.childStreams = map[string]*childStreamState{
		"run_child_x": {attachAttempts: 5},
	}
	m.attachChildStream("run_child_x", &mockInteractiveClientStream{})
	if m.childStreams["run_child_x"].attachAttempts != 5 {
		t.Fatalf("attachAttempts = %d, want preserved across stream attach", m.childStreams["run_child_x"].attachAttempts)
	}
}

func TestRunTUI_handleChildStreamMsg_attachFailureDoesNotAbortParent(t *testing.T) {
	m := newRunTUI(context.Background(), &mockInteractiveClientStream{}, &runtimev1.RunSessionInteractiveStart{}, nil)
	m.width = 120
	m.height = 24
	m.sessions.registerChild("run_child_fail", "worker@1.0.0")
	m.childStreams = map[string]*childStreamState{"run_child_fail": {}}

	cmd, err := m.handleChildStreamMsg(tuiChildStreamMsg{
		childSessionID: "run_child_fail",
		err:            clierr.WrapRPC("attach child session", errors.New("permission denied")),
	})
	if err != nil {
		t.Fatalf("handleChildStreamMsg: %v", err)
	}
	if cmd != nil {
		t.Fatal("expected no follow-up cmd after fatal attach error")
	}
	if m.quitting {
		t.Fatal("parent TUI should stay running when child attach fails")
	}
	if entry := m.childEntry("run_child_fail"); entry == nil || entry.status != "failed" {
		t.Fatalf("child status = %v, want failed", entry)
	}
}

func TestRunTUI_handleChildStreamMsg_retriesTransientAttachError(t *testing.T) {
	m := newRunTUI(context.Background(), &mockInteractiveClientStream{}, &runtimev1.RunSessionInteractiveStart{}, nil)
	m.sessions.registerChild("run_child_retry", "worker@1.0.0")
	m.childStreams = map[string]*childStreamState{"run_child_retry": {}}

	cmd, err := m.handleChildStreamMsg(tuiChildStreamMsg{
		childSessionID: "run_child_retry",
		err:            clierr.WrapRPC("attach child session", errors.New("session not found")),
	})
	if err != nil {
		t.Fatalf("handleChildStreamMsg: %v", err)
	}
	if cmd == nil {
		t.Fatal("expected retry cmd for transient attach error")
	}
	if m.childStreams["run_child_retry"].attachAttempts != 1 {
		t.Fatalf("attachAttempts = %d, want 1", m.childStreams["run_child_retry"].attachAttempts)
	}
}

func TestRunTUI_handleChildStreamMsg_childTextDelta(t *testing.T) {
	m := newRunTUI(context.Background(), &mockInteractiveClientStream{}, &runtimev1.RunSessionInteractiveStart{}, nil)
	m.width = 120
	m.height = 24
	m.sessions.registerChild("run_child_live", "explainer@1.0.0")
	m.sessions.selectedIdx = 1
	m.layout()

	if err := m.handleChildServerMsg("run_child_live", &runtimev1.RunSessionInteractiveServerMsg{
		Body: &runtimev1.RunSessionInteractiveServerMsg_TextDelta{
			TextDelta: &runtimev1.RunSessionInteractiveTextDelta{Delta: "hello child"},
		},
	}); err != nil {
		t.Fatalf("text_delta: %v", err)
	}
	if !strings.Contains(m.conversationText(), "hello child") {
		t.Fatalf("conversation = %q, want child streaming text", m.conversationText())
	}
}

func TestRunTUI_footerShowsCollapsedSessionsHintOnNarrowTerminal(t *testing.T) {
	m := newRunTUI(context.Background(), &mockInteractiveClientStream{}, &runtimev1.RunSessionInteractiveStart{}, nil)
	m.width = 80
	m.delegationPaneVisible = true
	m.sessions.registerChild("run_child_a", "explainer@1.0.0")
	footer := m.footerView()
	if !strings.Contains(footer, tuiSessionsPaneCollapsedHint) {
		t.Fatalf("footer = %q, want collapsed sessions hint", footer)
	}
}

func TestRunTUI_handleServerMsg_delegationSetsPendingAttach(t *testing.T) {
	m := newRunTUI(context.Background(), &mockInteractiveClientStream{}, &runtimev1.RunSessionInteractiveStart{}, &recordingRuntimeClient{})
	m.width = 120
	m.height = 24
	if err := m.handleServerMsg(&runtimev1.RunSessionInteractiveServerMsg{
		Body: &runtimev1.RunSessionInteractiveServerMsg_ToolCall{
			ToolCall: &runtimev1.RunSessionInteractiveToolCall{
				CallId:           "call-1",
				AgentDelegation:  true,
				ChildSessionId:   "run_child_sched",
				DelegationTarget: "demo.worker@1.0.0",
			},
		},
	}); err != nil {
		t.Fatalf("tool_call: %v", err)
	}
	if m.pendingCmd == nil {
		t.Fatal("expected pending child attach cmd when runtime client is configured")
	}
}

func TestRunTUI_sessionsRegistry_growsOnDelegation(t *testing.T) {
	m := newRunTUI(context.Background(), &mockInteractiveClientStream{}, &runtimev1.RunSessionInteractiveStart{}, nil)
	if len(m.sessions.entries) != 1 || !m.sessions.entries[0].isParent {
		t.Fatal("expected parent session at index 0")
	}
	m.registerDelegation(&runtimev1.RunSessionInteractiveToolCall{
		CallId:           "c1",
		AgentDelegation:  true,
		ChildSessionId:   "run_child_x",
		DelegationTarget: "demo.worker@1.0.0",
	})
	if m.sessions.byID["run_child_x"] != 1 {
		t.Fatalf("byID = %+v, want child at index 1", m.sessions.byID)
	}
	if !m.parentEntry().isDelegationCallID("c1") {
		t.Fatal("parent should track delegation call id")
	}
}

func TestRunTUI_childSessionInputNotDuplicatedWithHistory(t *testing.T) {
	m := newRunTUI(context.Background(), &mockInteractiveClientStream{}, &runtimev1.RunSessionInteractiveStart{}, nil)
	m.width = 120
	m.height = 24
	m.layout()

	tc := &runtimev1.RunSessionInteractiveToolCall{
		CallId:           "call-delegate",
		Args:             []byte(`{"task":"Explain quantum entanglement"}`),
		AgentDelegation:  true,
		ChildSessionId:   "run_child_dup",
		DelegationTarget: "playground.explainer@1.0.0",
	}
	if err := m.handleServerMsg(&runtimev1.RunSessionInteractiveServerMsg{
		Body: &runtimev1.RunSessionInteractiveServerMsg_ToolCall{ToolCall: tc},
	}); err != nil {
		t.Fatalf("tool_call: %v", err)
	}
	if err := m.handleChildServerMsg("run_child_dup", &runtimev1.RunSessionInteractiveServerMsg{
		Body: &runtimev1.RunSessionInteractiveServerMsg_SessionStarted{
			SessionStarted: &runtimev1.RunSessionInteractiveSessionStarted{
				SessionId: "run_child_dup",
				History: []*runtimev1.InteractiveConversationMessage{
					{Role: "user", Content: "Explain quantum entanglement"},
				},
			},
		},
	}); err != nil {
		t.Fatalf("session_started: %v", err)
	}
	m.sessions.selectedIdx = m.sessions.byID["run_child_dup"]
	conv := m.conversationText()
	if strings.Count(conv, "Explain quantum entanglement") != 1 {
		t.Fatalf("conversation = %q, want task once got %d", conv, strings.Count(conv, "Explain quantum entanglement"))
	}
}
