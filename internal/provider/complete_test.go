package provider

import (
	"context"
	"errors"
	"testing"

	"github.com/phrony-platform/runtime/internal/manifest"
)

func TestAnthropicProvider_Complete_invalidBeforeAPI(t *testing.T) {
	p := newAnthropicProvider("sk-test-not-called")
	ch := make(chan CompletionEvent, 4)
	err := p.Complete(context.Background(), CompletionRequest{
		Model: "",
		Messages: []Message{
			{Role: RoleUser, Content: "hi"},
		},
	}, ch)
	if err == nil {
		t.Fatal("Complete() = nil, want error")
	}
}

func TestAnthropicProvider_Complete_unsupportedRole(t *testing.T) {
	p := newAnthropicProvider("sk-test")
	ch := make(chan CompletionEvent, 4)
	err := p.Complete(context.Background(), CompletionRequest{
		Model: "claude-sonnet-4-5",
		Messages: []Message{
			{Role: "tool", Content: "data"},
		},
	}, ch)
	if err == nil {
		t.Fatal("Complete() = nil, want error")
	}
	var failed bool
	for ev := range ch {
		if ev.Type == EventFailed {
			failed = true
		}
	}
	if !failed {
		t.Fatal("want failed event on channel")
	}
}

func TestOpenAIProvider_Complete_invalidBeforeAPI(t *testing.T) {
	p := newOpenAIProvider("sk-test-not-called")
	ch := make(chan CompletionEvent, 4)
	err := p.Complete(context.Background(), CompletionRequest{
		Model:    "",
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	}, ch)
	if err == nil {
		t.Fatal("Complete() = nil, want error")
	}
}

func TestAnthropicParams_noMessages(t *testing.T) {
	_, err := anthropicParams(CompletionRequest{Model: "claude-sonnet-4-5"})
	if err == nil {
		t.Fatal("anthropicParams() = nil, want error")
	}
}

func TestOpenAIParams_noMessages(t *testing.T) {
	_, err := openAIParams(CompletionRequest{Model: "gpt-4o"})
	if err == nil {
		t.Fatal("openAIParams() = nil, want error")
	}
}

func TestAnthropicReasoningBudget(t *testing.T) {
	if anthropicReasoningBudget("low") != 1024 {
		t.Fatal("low budget")
	}
	if anthropicReasoningBudget("high") != 16384 {
		t.Fatal("high budget")
	}
	if anthropicReasoningBudget("medium") != 8192 {
		t.Fatal("medium budget")
	}
	if anthropicReasoningBudget("unknown") != 8192 {
		t.Fatal("default budget")
	}
}

func TestEmitFailed(t *testing.T) {
	ch := make(chan CompletionEvent, 2)
	emitFailed(ch, errors.New("boom"))
	emitFailed(ch, nil)
	var ev CompletionEvent
	select {
	case ev = <-ch:
	default:
		t.Fatal("want failed event on channel")
	}
	if ev.Err == nil {
		t.Fatal("want error on failed event")
	}
}

func TestApplyAnthropicReasoning_invalidEffortIgnored(t *testing.T) {
	_, err := anthropicParams(CompletionRequest{
		Model:     "claude-sonnet-4-5",
		Messages:  []Message{{Role: RoleUser, Content: "hi"}},
		Reasoning: &manifest.ReasoningConfig{Effort: "invalid"},
	})
	if err != nil {
		t.Fatalf("anthropicParams: %v", err)
	}
}
