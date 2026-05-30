package provider

import (
	"context"

	"github.com/phrony-platform/runtime/internal/manifest"
)

const (
	RoleSystem    = "system"
	RoleUser      = "user"
	RoleAssistant = "assistant"

	IDAnthropic = "anthropic"
	IDOpenAI    = "openai"
)

// Provider streams model completions for a single vendor.
type Provider interface {
	ID() string
	Complete(ctx context.Context, req CompletionRequest, ch chan<- CompletionEvent) error
}

// Message is one turn in a completion request.
type Message struct {
	Role    string
	Content string
}

// CompletionRequest is a single model call for an agent version's configured model.
type CompletionRequest struct {
	Model           string
	Messages        []Message
	Parameters      *manifest.ModelParameters
	Reasoning       *manifest.ReasoningConfig
	ProviderOptions map[string]any
}

// CompletionEventType classifies streaming completion events.
type CompletionEventType string

const (
	EventTextDelta  CompletionEventType = "text_delta"
	EventCompleted  CompletionEventType = "completed"
	EventFailed     CompletionEventType = "failed"
)

// CompletionEvent is emitted on the channel passed to Complete.
// Complete closes the channel when it returns.
type CompletionEvent struct {
	Type       CompletionEventType
	TextDelta  string
	StopReason string
	Err        error
}
