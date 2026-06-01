package providertest

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/phrony-platform/runtime/internal/provider"
)

// Stub implements provider.Provider for tests.
type Stub struct {
	IDVal  string
	Events []provider.CompletionEvent
	Err    error // if set, return before emitting events
}

func (s *Stub) ID() string {
	if s.IDVal != "" {
		return s.IDVal
	}
	return provider.IDAnthropic
}

func (s *Stub) Complete(ctx context.Context, req provider.CompletionRequest, ch chan<- provider.CompletionEvent) error {
	defer close(ch)
	if s.Err != nil {
		return s.Err
	}
	for _, ev := range s.Events {
		ch <- ev
	}
	return nil
}

// DeltaCompleted returns a stub that emits two text deltas and a completed event.
func DeltaCompleted() *Stub {
	return &Stub{
		Events: []provider.CompletionEvent{
			{Type: provider.EventTextDelta, TextDelta: "Hi "},
			{Type: provider.EventTextDelta, TextDelta: "there"},
			{Type: provider.EventCompleted, StopReason: provider.StopReasonEndTurn},
		},
	}
}

// Fail returns a stub that emits a failed event with err.
func Fail(err error) *Stub {
	return &Stub{
		Events: []provider.CompletionEvent{
			{Type: provider.EventFailed, Err: err},
		},
	}
}

// UsageCompleted returns a stub that emits one delta and a completed event with usage.
func UsageCompleted(usage provider.TokenUsage) *Stub {
	return &Stub{
		Events: []provider.CompletionEvent{
			{Type: provider.EventTextDelta, TextDelta: "ok"},
			{Type: provider.EventCompleted, StopReason: provider.StopReasonEndTurn, Usage: usage},
		},
	}
}

// EmptyDeltaCompleted returns a stub that emits an empty delta, "ok", and completed.
func EmptyDeltaCompleted() *Stub {
	return &Stub{
		Events: []provider.CompletionEvent{
			{Type: provider.EventTextDelta, TextDelta: ""},
			{Type: provider.EventTextDelta, TextDelta: "ok"},
			{Type: provider.EventCompleted, StopReason: provider.StopReasonEndTurn},
		},
	}
}

// ToolUseCompleted returns a stub that emits tool calls then completes with tool_use.
func ToolUseCompleted(calls ...provider.ToolCall) *Stub {
	events := make([]provider.CompletionEvent, 0, len(calls)+1)
	for i := range calls {
		tc := calls[i]
		call := tc
		events = append(events, provider.CompletionEvent{
			Type:     provider.EventToolCall,
			ToolCall: &call,
		})
	}
	events = append(events, provider.CompletionEvent{
		Type:       provider.EventCompleted,
		StopReason: provider.StopReasonToolUse,
	})
	return &Stub{Events: events}
}

// Sequence returns a stub that plays a new event script on each Complete call.
func Sequence(scripts ...[]provider.CompletionEvent) *SequenceStub {
	cp := make([][]provider.CompletionEvent, len(scripts))
	for i, s := range scripts {
		cp[i] = append([]provider.CompletionEvent(nil), s...)
	}
	return &SequenceStub{Scripts: cp}
}

// SequenceStub implements provider.Provider with per-call scripts.
type SequenceStub struct {
	Scripts [][]provider.CompletionEvent
	Calls   int
	Err     error
}

func (s *SequenceStub) ID() string { return provider.IDAnthropic }

func (s *SequenceStub) Complete(ctx context.Context, req provider.CompletionRequest, ch chan<- provider.CompletionEvent) error {
	defer close(ch)
	if s.Err != nil {
		return s.Err
	}
	if s.Calls >= len(s.Scripts) {
		return fmt.Errorf("unexpected completion call %d", s.Calls+1)
	}
	for _, ev := range s.Scripts[s.Calls] {
		ch <- ev
	}
	s.Calls++
	return nil
}

// ToolUseWithText emits text, one tool call, and tool_use completion.
func ToolUseWithText(text string, call provider.ToolCall) *Stub {
	if len(call.Args) == 0 {
		call.Args = json.RawMessage("{}")
	}
	return &Stub{
		Events: []provider.CompletionEvent{
			{Type: provider.EventTextDelta, TextDelta: text},
			{Type: provider.EventToolCall, ToolCall: &call},
			{Type: provider.EventCompleted, StopReason: provider.StopReasonToolUse},
		},
	}
}
