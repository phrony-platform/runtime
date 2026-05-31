package providertest

import (
	"context"

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
			{Type: provider.EventCompleted, StopReason: "end_turn"},
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
			{Type: provider.EventCompleted, StopReason: "end_turn", Usage: usage},
		},
	}
}

// EmptyDeltaCompleted returns a stub that emits an empty delta, "ok", and completed.
func EmptyDeltaCompleted() *Stub {
	return &Stub{
		Events: []provider.CompletionEvent{
			{Type: provider.EventTextDelta, TextDelta: ""},
			{Type: provider.EventTextDelta, TextDelta: "ok"},
			{Type: provider.EventCompleted, StopReason: "end_turn"},
		},
	}
}
