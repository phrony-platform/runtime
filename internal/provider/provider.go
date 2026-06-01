package provider

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/phrony-platform/runtime/internal/manifest"
)

const (
	RoleSystem    = "system"
	RoleUser      = "user"
	RoleAssistant = "assistant"
	// RoleTool is the OpenAI wire role for tool results. Anthropic expects tool_result
	// blocks on user messages; openai.go maps RoleTool to tool messages and anthropic.go
	// converts them to user messages with tool_result blocks.
	RoleTool = "tool"

	IDAnthropic = "anthropic"
	IDOpenAI    = "openai"

	StopReasonEndTurn   = "end_turn"
	StopReasonToolUse   = "tool_use"
	StopReasonMaxTokens = "max_tokens"
)

// Provider streams model completions for a single vendor.
type Provider interface {
	ID() string
	Complete(ctx context.Context, req CompletionRequest, ch chan<- CompletionEvent) error
}

// ContentBlockType classifies structured message content.
type ContentBlockType string

const (
	BlockText       ContentBlockType = "text"
	BlockToolUse    ContentBlockType = "tool_use"
	BlockToolResult ContentBlockType = "tool_result"
)

// ContentBlock is one piece of structured message content (text, tool_use, or tool_result).
type ContentBlock struct {
	Type ContentBlockType

	// BlockText
	Text string

	// BlockToolUse
	ToolUseID string
	ToolName  string
	Input     json.RawMessage

	// BlockToolResult
	ToolResultContent string
	IsError           bool
}

// Message is one turn in a completion request.
type Message struct {
	Role    string
	Content string
	// Blocks holds structured content when non-empty. Plain Content is treated as a
	// single text block when Blocks is empty.
	Blocks []ContentBlock
	// ToolCallID is set on RoleTool messages (OpenAI tool result wire format).
	ToolCallID string
	// StopReason, TurnUsage, and TurnDurationMs are populated on assistant rows in persisted session history.
	StopReason     string
	TurnUsage      TokenUsage
	TurnDurationMs int64
}

// ToolDefinition is one tool contract presented to the model for a completion.
type ToolDefinition struct {
	Name        string
	Description string
	InputSchema json.RawMessage
}

// CompletionRequest is a single model call for an agent version's configured model.
type CompletionRequest struct {
	Model           string
	Messages        []Message
	Tools           []ToolDefinition
	Parameters      *manifest.ModelParameters
	Reasoning       *manifest.ReasoningConfig
	ProviderOptions map[string]any
}

// CompletionEventType classifies streaming completion events.
type CompletionEventType string

const (
	EventTextDelta      CompletionEventType = "text_delta"
	EventToolInputDelta CompletionEventType = "tool_input_delta"
	EventToolCall       CompletionEventType = "tool_call"
	EventCompleted      CompletionEventType = "completed"
	EventFailed         CompletionEventType = "failed"
)

// ToolCall is one model-emitted tool invocation (complete arguments).
type ToolCall struct {
	ID   string
	Name string
	Args json.RawMessage
}

// CompletionEvent is emitted on the channel passed to Complete.
// Complete closes the channel when it returns.
type CompletionEvent struct {
	Type           CompletionEventType
	TextDelta      string
	ToolCall       *ToolCall
	ToolCallID     string
	ToolInputDelta string
	StopReason     string
	Usage          TokenUsage
	Err            error
}

// TextBlock returns a text content block.
func TextBlock(text string) ContentBlock {
	return ContentBlock{Type: BlockText, Text: text}
}

// ToolUseBlock returns a tool_use content block from a prior assistant turn.
func ToolUseBlock(id, name string, input json.RawMessage) ContentBlock {
	return ContentBlock{
		Type:      BlockToolUse,
		ToolUseID: id,
		ToolName:  name,
		Input:     input,
	}
}

// ToolResultBlock returns a tool_result content block.
func ToolResultBlock(toolUseID, content string, isError bool) ContentBlock {
	return ContentBlock{
		Type:              BlockToolResult,
		ToolUseID:         toolUseID,
		ToolResultContent: content,
		IsError:           isError,
	}
}

// MessageBlocks returns structured blocks for m, synthesizing a text block from Content when needed.
func MessageBlocks(m Message) []ContentBlock {
	if len(m.Blocks) > 0 {
		return m.Blocks
	}
	if strings.TrimSpace(m.Content) != "" {
		return []ContentBlock{{Type: BlockText, Text: m.Content}}
	}
	return nil
}

// NormalizeStopReason maps vendor-specific stop/finish reasons to runtime values.
func NormalizeStopReason(reason string) string {
	switch reason {
	case "end_turn", "stop":
		return StopReasonEndTurn
	case "tool_use", "tool_calls":
		return StopReasonToolUse
	case "max_tokens", "length":
		return StopReasonMaxTokens
	default:
		return reason
	}
}
