package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

const (
	IDStub = "stub"

	envEnableStubProvider = "RUNTIME_ENABLE_STUB_PROVIDER"
)

// StubEnabled reports whether the dev-only stub provider may be constructed.
func StubEnabled() bool {
	v := strings.TrimSpace(os.Getenv(envEnableStubProvider))
	return v == "1" || strings.EqualFold(v, "true") || strings.EqualFold(v, "yes")
}

type stubScript struct {
	Turns [][]stubScriptEvent `json:"turns"`
}

type stubScriptEvent struct {
	Type       string          `json:"type"`
	Text       string          `json:"text"`
	Name       string          `json:"name"`
	Args       json.RawMessage `json:"args"`
	StopReason string          `json:"stop_reason"`
	Err        string          `json:"error"`
}

type scriptedProvider struct {
	turns [][]CompletionEvent
	calls int
}

func newStubProvider(scriptJSON string) (Provider, error) {
	if !StubEnabled() {
		return nil, fmt.Errorf("stub provider is not enabled (set %s=true)", envEnableStubProvider)
	}
	scriptJSON = strings.TrimSpace(scriptJSON)
	if scriptJSON == "" {
		return nil, fmt.Errorf("stub script is required (compile agent with %s beside agent.yaml)", "stub-script.json")
	}
	var script stubScript
	if err := json.Unmarshal([]byte(scriptJSON), &script); err != nil {
		return nil, fmt.Errorf("parse stub script: %w", err)
	}
	if len(script.Turns) == 0 {
		return nil, fmt.Errorf("stub script must contain at least one turn")
	}
	turns := make([][]CompletionEvent, len(script.Turns))
	for i, turn := range script.Turns {
		events, err := eventsFromStubTurn(turn)
		if err != nil {
			return nil, fmt.Errorf("stub script turn %d: %w", i+1, err)
		}
		turns[i] = events
	}
	return &scriptedProvider{turns: turns}, nil
}

func eventsFromStubTurn(turn []stubScriptEvent) ([]CompletionEvent, error) {
	if len(turn) == 0 {
		return nil, fmt.Errorf("turn is empty")
	}
	out := make([]CompletionEvent, 0, len(turn))
	for j, ev := range turn {
		switch strings.TrimSpace(ev.Type) {
		case string(EventTextDelta):
			out = append(out, CompletionEvent{Type: EventTextDelta, TextDelta: ev.Text})
		case string(EventToolCall):
			name := strings.TrimSpace(ev.Name)
			if name == "" {
				return nil, fmt.Errorf("event %d: tool_call requires name", j+1)
			}
			args := ev.Args
			if len(args) == 0 {
				args = json.RawMessage("{}")
			}
			call := ToolCall{Name: name, Args: args}
			out = append(out, CompletionEvent{Type: EventToolCall, ToolCall: &call})
		case string(EventCompleted):
			reason := NormalizeStopReason(strings.TrimSpace(ev.StopReason))
			if reason == "" {
				reason = StopReasonEndTurn
			}
			out = append(out, CompletionEvent{Type: EventCompleted, StopReason: reason})
		case string(EventFailed):
			msg := strings.TrimSpace(ev.Err)
			if msg == "" {
				msg = "stub failed"
			}
			out = append(out, CompletionEvent{Type: EventFailed, Err: fmt.Errorf("%s", msg)})
		default:
			return nil, fmt.Errorf("event %d: unsupported type %q", j+1, ev.Type)
		}
	}
	return out, nil
}

func (p *scriptedProvider) ID() string { return IDStub }

func (p *scriptedProvider) Complete(ctx context.Context, req CompletionRequest, ch chan<- CompletionEvent) error {
	defer close(ch)
	if p.calls >= len(p.turns) {
		return fmt.Errorf("unexpected completion call %d (script has %d turns)", p.calls+1, len(p.turns))
	}
	for _, ev := range p.turns[p.calls] {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case ch <- ev:
		}
	}
	p.calls++
	return nil
}
