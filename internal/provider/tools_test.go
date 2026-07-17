package provider

import (
	"encoding/json"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/openai/openai-go/v3"
	"github.com/phrony-platform/runtime/internal/manifest"
)

func TestAnthropicParams_toolsAndStructuredMessages(t *testing.T) {
	schema := json.RawMessage(`{"type":"object","properties":{"q":{"type":"string"}},"required":["q"]}`)
	params, err := anthropicParams(CompletionRequest{
		Model: "claude-sonnet-4-5",
		Messages: []Message{
			{Role: RoleUser, Content: "Hello"},
			{
				Role: RoleAssistant,
				Blocks: []ContentBlock{
					ToolUseBlock("call_1", "search", json.RawMessage(`{"q":"weather"}`)),
				},
			},
			{
				Role: RoleUser,
				Blocks: []ContentBlock{
					ToolResultBlock("call_1", `{"temp":72}`, false),
				},
			},
		},
		Tools: []ToolDefinition{{
			Name:        "search",
			Description: "Search the web",
			InputSchema: schema,
		}},
	})
	if err != nil {
		t.Fatalf("anthropicParams: %v", err)
	}
	if len(params.Tools) != 1 {
		t.Fatalf("tools len = %d, want 1", len(params.Tools))
	}
	if name := params.Tools[0].GetName(); name == nil || *name != "search" {
		t.Fatalf("tool name = %v, want search", params.Tools[0].GetName())
	}
	if len(params.Messages) != 3 {
		t.Fatalf("messages len = %d, want 3", len(params.Messages))
	}
}

func TestOpenAIParams_toolsAndStructuredMessages(t *testing.T) {
	schema := json.RawMessage(`{"type":"object","properties":{"q":{"type":"string"}}}`)
	params, err := openAIParams(CompletionRequest{
		Model: "gpt-4o",
		Messages: []Message{
			{Role: RoleUser, Content: "Hello"},
			{
				Role: RoleAssistant,
				Blocks: []ContentBlock{
					ToolUseBlock("call_1", "search", json.RawMessage(`{"q":"weather"}`)),
				},
			},
			{Role: RoleTool, ToolCallID: "call_1", Content: `{"temp":72}`},
		},
		Tools: []ToolDefinition{{
			Name:        "search",
			InputSchema: schema,
		}},
	})
	if err != nil {
		t.Fatalf("openAIParams: %v", err)
	}
	if len(params.Tools) != 1 {
		t.Fatalf("tools len = %d, want 1", len(params.Tools))
	}
	if len(params.Messages) != 3 {
		t.Fatalf("messages len = %d, want 3", len(params.Messages))
	}
	assistant := params.Messages[1]
	if len(assistant.GetToolCalls()) != 1 {
		t.Fatalf("assistant tool_calls = %d, want 1", len(assistant.GetToolCalls()))
	}
}

func TestAnthropicParams_roleToolMapsToUserResult(t *testing.T) {
	params, err := anthropicParams(CompletionRequest{
		Model: "claude-sonnet-4-5",
		Messages: []Message{
			{Role: RoleUser, Content: "hi"},
			{Role: RoleTool, ToolCallID: "call_1", Content: "ok"},
		},
	})
	if err != nil {
		t.Fatalf("anthropicParams: %v", err)
	}
	if len(params.Messages) != 2 {
		t.Fatalf("messages len = %d, want 2", len(params.Messages))
	}
	if params.Messages[1].Role != anthropic.MessageParamRoleUser {
		t.Fatalf("tool result role = %q, want user", params.Messages[1].Role)
	}
}

func TestNormalizeStopReason(t *testing.T) {
	cases := map[string]string{
		"end_turn":   StopReasonEndTurn,
		"stop":       StopReasonEndTurn,
		"tool_use":   StopReasonToolUse,
		"tool_calls": StopReasonToolUse,
		"max_tokens": StopReasonMaxTokens,
		"length":     StopReasonMaxTokens,
		"custom":     "custom",
	}
	for in, want := range cases {
		if got := NormalizeStopReason(in); got != want {
			t.Fatalf("NormalizeStopReason(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestOpenAIAssistantMessage_sanitizesDottedToolName(t *testing.T) {
	msg, err := openAIAssistantMessage(Message{
		Role: RoleAssistant,
		Blocks: []ContentBlock{
			ToolUseBlock("id-1", "support.refund-agent", json.RawMessage(`{"a":1}`)),
		},
	})
	if err != nil {
		t.Fatalf("openAIAssistantMessage: %v", err)
	}
	got := msg.OfAssistant.ToolCalls[0].OfFunction.Function.Name
	want := manifest.ReplayToolName("support.refund-agent")
	if got != want {
		t.Fatalf("tool name = %q, want %q", got, want)
	}
}

func TestOpenAIAssistantMessage_toolCalls(t *testing.T) {
	msg, err := openAIAssistantMessage(Message{
		Role: RoleAssistant,
		Blocks: []ContentBlock{
			TextBlock("thinking"),
			ToolUseBlock("id-1", "fn", json.RawMessage(`{"a":1}`)),
		},
	})
	if err != nil {
		t.Fatalf("openAIAssistantMessage: %v", err)
	}
	if msg.OfAssistant == nil {
		t.Fatal("want assistant message")
	}
	if len(msg.OfAssistant.ToolCalls) != 1 {
		t.Fatalf("tool_calls = %d", len(msg.OfAssistant.ToolCalls))
	}
	if msg.OfAssistant.ToolCalls[0].OfFunction.Function.Name != "fn" {
		t.Fatalf("tool name = %q", msg.OfAssistant.ToolCalls[0].OfFunction.Function.Name)
	}
}

func TestOpenAIToolResultMessage_longCallID(t *testing.T) {
	longID := "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee:ffffffff-1111-2222-3333-444444444444:1:0"
	msg, err := openAIToolResultMessage(Message{
		Role:       RoleTool,
		ToolCallID: longID,
		Content:    "result",
	})
	if err != nil {
		t.Fatalf("openAIToolResultMessage: %v", err)
	}
	if len(msg.OfTool.ToolCallID) > MaxWireToolCallIDLen {
		t.Fatalf("tool_call_id len = %d, want <= %d", len(msg.OfTool.ToolCallID), MaxWireToolCallIDLen)
	}
	if msg.OfTool.ToolCallID != WireToolCallID(longID) {
		t.Fatalf("tool_call_id = %q, want %q", msg.OfTool.ToolCallID, WireToolCallID(longID))
	}
}

func TestOpenAIToolResultMessage(t *testing.T) {
	msg, err := openAIToolResultMessage(Message{
		Role:       RoleTool,
		ToolCallID: "call_1",
		Content:    "result",
	})
	if err != nil {
		t.Fatalf("openAIToolResultMessage: %v", err)
	}
	if msg.OfTool == nil {
		t.Fatal("want tool message")
	}
	if msg.OfTool.ToolCallID != "call_1" {
		t.Fatalf("tool_call_id = %q", msg.OfTool.ToolCallID)
	}
}

func TestAnthropicTools_requiresName(t *testing.T) {
	_, err := anthropicTools([]ToolDefinition{{Description: "x"}})
	if err == nil {
		t.Fatal("want error for missing tool name")
	}
}

func TestOpenAITools_requiresName(t *testing.T) {
	_, err := openAITools([]ToolDefinition{{Description: "x"}})
	if err == nil {
		t.Fatal("want error for missing tool name")
	}
}

func TestEmitOpenAIToolCalls_accumulatedUnionWithoutType(t *testing.T) {
	// ChatCompletionAccumulator sets flat Function/ID fields; Type may be unset on later chunks.
	acc := openai.ChatCompletion{
		Choices: []openai.ChatCompletionChoice{{
			Message: openai.ChatCompletionMessage{
				ToolCalls: []openai.ChatCompletionMessageToolCallUnion{{
					ID: "call_1",
					Function: openai.ChatCompletionMessageFunctionToolCallFunction{
						Name:      "weather_get_forecast",
						Arguments: `{"city":"NYC"}`,
					},
				}},
			},
		}},
	}
	ch := make(chan CompletionEvent, 2)
	emitOpenAIToolCalls(ch, acc)
	close(ch)
	var n int
	for range ch {
		n++
	}
	if n != 1 {
		t.Fatalf("emitted %d EventToolCall, want 1 (type=%q asany=%v)", n, acc.Choices[0].Message.ToolCalls[0].Type, acc.Choices[0].Message.ToolCalls[0].AsAny())
	}
}

// Ensure SDK helper types compile with our mappings.
var (
	_ = anthropic.NewToolUseBlock
	_ = openai.ChatCompletionFunctionTool
)
