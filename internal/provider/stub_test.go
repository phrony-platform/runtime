package provider

import (
	"context"
	"encoding/json"
	"testing"
)

func TestStubProvider_sequence(t *testing.T) {
	t.Setenv(envEnableStubProvider, "true")

	script := `{
		"turns": [
			[
				{"type": "tool_call", "name": "process_payment", "args": {"amount": 500}},
				{"type": "completed", "stop_reason": "tool_use"}
			],
			[
				{"type": "text_delta", "text": "done"},
				{"type": "completed", "stop_reason": "end_turn"}
			]
		]
	}`

	p, err := newStubProvider(script)
	if err != nil {
		t.Fatalf("newStubProvider: %v", err)
	}
	if p.ID() != IDStub {
		t.Fatalf("ID() = %q, want %q", p.ID(), IDStub)
	}

	ch := make(chan CompletionEvent, 8)
	if err := p.Complete(context.Background(), CompletionRequest{}, ch); err != nil {
		t.Fatalf("Complete turn 1: %v", err)
	}
	var events []CompletionEvent
	for ev := range ch {
		events = append(events, ev)
	}
	if len(events) != 2 || events[0].Type != EventToolCall || events[1].StopReason != StopReasonToolUse {
		t.Fatalf("turn 1 events = %+v", events)
	}
	if events[0].ToolCall == nil || events[0].ToolCall.Name != "process_payment" {
		t.Fatalf("tool call = %+v", events[0].ToolCall)
	}

	ch = make(chan CompletionEvent, 8)
	if err := p.Complete(context.Background(), CompletionRequest{}, ch); err != nil {
		t.Fatalf("Complete turn 2: %v", err)
	}
	events = events[:0]
	for ev := range ch {
		events = append(events, ev)
	}
	if len(events) != 2 || events[0].TextDelta != "done" || events[1].StopReason != StopReasonEndTurn {
		t.Fatalf("turn 2 events = %+v", events)
	}

	ch = make(chan CompletionEvent, 1)
	if err := p.Complete(context.Background(), CompletionRequest{}, ch); err == nil {
		t.Fatal("expected error on extra Complete call")
	}
}

func TestStubProvider_disabled(t *testing.T) {
	t.Setenv(envEnableStubProvider, "false")
	_, err := newStubProvider(`{"turns":[[{"type":"completed","stop_reason":"end_turn"}]]}`)
	if err == nil {
		t.Fatal("expected error when stub provider disabled")
	}
}

func TestStubProvider_toolCallArgsDefault(t *testing.T) {
	t.Setenv(envEnableStubProvider, "true")
	p, err := newStubProvider(`{"turns":[[{"type":"tool_call","name":"t"},{"type":"completed","stop_reason":"tool_use"}]]}`)
	if err != nil {
		t.Fatalf("newStubProvider: %v", err)
	}
	ch := make(chan CompletionEvent, 4)
	if err := p.Complete(context.Background(), CompletionRequest{}, ch); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	ev := <-ch
	if ev.ToolCall == nil || !json.Valid(ev.ToolCall.Args) {
		t.Fatalf("tool args = %s", ev.ToolCall.Args)
	}
}
