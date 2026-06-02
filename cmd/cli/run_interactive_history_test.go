package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
)

func TestPrintConversationHistory_skipsSystemAndTool(t *testing.T) {
	var stdout bytes.Buffer
	err := printConversationHistory(&stdout, []*runtimev1.InteractiveConversationMessage{
		{Role: "system", Content: "You are a payment agent."},
		{Role: "tool", Content: `{"status":"ok"}`},
		{Role: "user", Content: "pay $10"},
		{Role: "assistant", Content: "done"},
	})
	if err != nil {
		t.Fatalf("printConversationHistory: %v", err)
	}
	out := stdout.String()
	if strings.Contains(out, "payment agent") {
		t.Fatalf("output = %q, want system prompt omitted", out)
	}
	if strings.Contains(out, `"status":"ok"`) {
		t.Fatalf("output = %q, want tool payload omitted", out)
	}
	if !strings.Contains(out, "pay $10") || !strings.Contains(out, "done") {
		t.Fatalf("output = %q, want user and assistant turns", out)
	}
}

func TestRunTUI_handleServerMsg_sessionStartedWithSystemInHistory(t *testing.T) {
	m := newRunTUI(context.Background(), &mockInteractiveClientStream{}, &runtimev1.RunSessionInteractiveStart{SessionId: "sess-tool"})
	m.width = 80
	m.height = 24
	m.layout()

	err := m.handleServerMsg(&runtimev1.RunSessionInteractiveServerMsg{
		Body: &runtimev1.RunSessionInteractiveServerMsg_SessionStarted{
			SessionStarted: &runtimev1.RunSessionInteractiveSessionStarted{
				SessionId: "sess-tool",
				History: []*runtimev1.InteractiveConversationMessage{
					{Role: "system", Content: "When the user asks to send a payment, call process_payment."},
					{Role: "user", Content: "send $5 to alice"},
					{Role: "assistant", Content: "calling tool"},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("session_started: %v", err)
	}
	text := m.conversationText()
	if strings.Contains(text, "process_payment") {
		t.Fatalf("conversation = %q, want system instructions hidden", text)
	}
	if !strings.Contains(text, "send $5 to alice") || !strings.Contains(text, "calling tool") {
		t.Fatalf("conversation = %q, want user and assistant turns", text)
	}
}
