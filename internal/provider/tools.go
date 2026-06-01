package provider

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/shared"
)

func toolInputSchema(raw json.RawMessage) (anthropic.ToolInputSchemaParam, error) {
	schema := anthropic.ToolInputSchemaParam{Properties: map[string]any{}}
	if len(raw) == 0 {
		return schema, nil
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(raw, &doc); err != nil {
		return anthropic.ToolInputSchemaParam{}, fmt.Errorf("tool input_schema: %w", err)
	}
	if props, ok := doc["properties"]; ok {
		if err := json.Unmarshal(props, &schema.Properties); err != nil {
			return anthropic.ToolInputSchemaParam{}, fmt.Errorf("tool input_schema.properties: %w", err)
		}
	}
	if req, ok := doc["required"]; ok {
		if err := json.Unmarshal(req, &schema.Required); err != nil {
			return anthropic.ToolInputSchemaParam{}, fmt.Errorf("tool input_schema.required: %w", err)
		}
	}
	return schema, nil
}

func anthropicTools(tools []ToolDefinition) ([]anthropic.ToolUnionParam, error) {
	if len(tools) == 0 {
		return nil, nil
	}
	out := make([]anthropic.ToolUnionParam, 0, len(tools))
	for _, t := range tools {
		name := strings.TrimSpace(t.Name)
		if name == "" {
			return nil, fmt.Errorf("tool name is required")
		}
		schema, err := toolInputSchema(t.InputSchema)
		if err != nil {
			return nil, err
		}
		desc := strings.TrimSpace(t.Description)
		tool := anthropic.ToolParam{
			Name:        name,
			InputSchema: schema,
		}
		if desc != "" {
			tool.Description = anthropic.String(desc)
		}
		out = append(out, anthropic.ToolUnionParam{OfTool: &tool})
	}
	return out, nil
}

func openAITools(tools []ToolDefinition) ([]openai.ChatCompletionToolUnionParam, error) {
	if len(tools) == 0 {
		return nil, nil
	}
	out := make([]openai.ChatCompletionToolUnionParam, 0, len(tools))
	for _, t := range tools {
		name := strings.TrimSpace(t.Name)
		if name == "" {
			return nil, fmt.Errorf("tool name is required")
		}
		params, err := openAIFunctionParameters(t.InputSchema)
		if err != nil {
			return nil, err
		}
		fn := shared.FunctionDefinitionParam{
			Name:       name,
			Parameters: params,
		}
		if desc := strings.TrimSpace(t.Description); desc != "" {
			fn.Description = openai.String(desc)
		}
		out = append(out, openai.ChatCompletionFunctionTool(fn))
	}
	return out, nil
}

func openAIFunctionParameters(raw json.RawMessage) (shared.FunctionParameters, error) {
	if len(raw) == 0 {
		return shared.FunctionParameters{"type": "object"}, nil
	}
	var params shared.FunctionParameters
	if err := json.Unmarshal(raw, &params); err != nil {
		return nil, fmt.Errorf("tool input_schema: %w", err)
	}
	return params, nil
}

func toolUseInputJSON(input json.RawMessage) (any, error) {
	if len(input) == 0 {
		return map[string]any{}, nil
	}
	var v any
	if err := json.Unmarshal(input, &v); err != nil {
		return nil, fmt.Errorf("tool_use input: %w", err)
	}
	return v, nil
}

func anthropicContentBlocks(m Message) ([]anthropic.ContentBlockParamUnion, error) {
	blocks := MessageBlocks(m)
	if len(blocks) == 0 {
		return nil, fmt.Errorf("message has no content")
	}
	out := make([]anthropic.ContentBlockParamUnion, 0, len(blocks))
	for _, b := range blocks {
		switch b.Type {
		case BlockText:
			if strings.TrimSpace(b.Text) != "" {
				out = append(out, anthropic.NewTextBlock(b.Text))
			}
		case BlockToolUse:
			if strings.TrimSpace(b.ToolUseID) == "" || strings.TrimSpace(b.ToolName) == "" {
				return nil, fmt.Errorf("tool_use block requires id and name")
			}
			input, err := toolUseInputJSON(b.Input)
			if err != nil {
				return nil, err
			}
			out = append(out, anthropic.NewToolUseBlock(b.ToolUseID, input, b.ToolName))
		case BlockToolResult:
			if strings.TrimSpace(b.ToolUseID) == "" {
				return nil, fmt.Errorf("tool_result block requires tool_use_id")
			}
			out = append(out, anthropic.NewToolResultBlock(b.ToolUseID, b.ToolResultContent, b.IsError))
		default:
			return nil, fmt.Errorf("unsupported content block type %q", b.Type)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("message has no content")
	}
	return out, nil
}

func openAIAssistantMessage(m Message) (openai.ChatCompletionMessageParamUnion, error) {
	blocks := MessageBlocks(m)
	var text string
	var toolCalls []openai.ChatCompletionMessageToolCallUnionParam
	for _, b := range blocks {
		switch b.Type {
		case BlockText:
			text += b.Text
		case BlockToolUse:
			if strings.TrimSpace(b.ToolUseID) == "" || strings.TrimSpace(b.ToolName) == "" {
				return openai.ChatCompletionMessageParamUnion{}, fmt.Errorf("tool_use block requires id and name")
			}
			args := string(b.Input)
			if args == "" {
				args = "{}"
			}
			toolCalls = append(toolCalls, openai.ChatCompletionMessageToolCallUnionParam{
				OfFunction: &openai.ChatCompletionMessageFunctionToolCallParam{
					ID: b.ToolUseID,
					Function: openai.ChatCompletionMessageFunctionToolCallFunctionParam{
						Name:      b.ToolName,
						Arguments: args,
					},
				},
			})
		default:
			return openai.ChatCompletionMessageParamUnion{}, fmt.Errorf("assistant message cannot contain %q block", b.Type)
		}
	}
	if len(toolCalls) == 0 {
		return openai.AssistantMessage(text), nil
	}
	assistant := openai.ChatCompletionAssistantMessageParam{
		ToolCalls: toolCalls,
	}
	if strings.TrimSpace(text) != "" {
		assistant.Content.OfString = openai.String(text)
	}
	return openai.ChatCompletionMessageParamUnion{OfAssistant: &assistant}, nil
}

func openAIToolResultMessage(m Message) (openai.ChatCompletionMessageParamUnion, error) {
	toolCallID := strings.TrimSpace(m.ToolCallID)
	content := strings.TrimSpace(m.Content)
	isError := false
	for _, b := range m.Blocks {
		if b.Type != BlockToolResult {
			return openai.ChatCompletionMessageParamUnion{}, fmt.Errorf("tool message can only contain tool_result blocks")
		}
		if toolCallID == "" {
			toolCallID = strings.TrimSpace(b.ToolUseID)
		}
		content = b.ToolResultContent
		isError = b.IsError
	}
	if toolCallID == "" {
		return openai.ChatCompletionMessageParamUnion{}, fmt.Errorf("tool message requires tool_call_id")
	}
	if content == "" && !isError {
		return openai.ChatCompletionMessageParamUnion{}, fmt.Errorf("tool message requires content")
	}
	return openai.ToolMessage(content, toolCallID), nil
}

func emitAnthropicToolCalls(ch chan<- CompletionEvent, acc anthropic.Message) {
	for _, block := range acc.Content {
		tu, ok := block.AsAny().(anthropic.ToolUseBlock)
		if !ok {
			continue
		}
		args := json.RawMessage(tu.Input)
		if len(args) == 0 {
			args = json.RawMessage("{}")
		}
		ch <- CompletionEvent{
			Type: EventToolCall,
			ToolCall: &ToolCall{
				ID:   tu.ID,
				Name: tu.Name,
				Args: args,
			},
		}
	}
}

func emitOpenAIToolCalls(ch chan<- CompletionEvent, acc openai.ChatCompletion) {
	if len(acc.Choices) == 0 {
		return
	}
	for _, tc := range acc.Choices[0].Message.ToolCalls {
		id, name, argStr := openAIToolCallFields(tc)
		if name == "" && id == "" {
			continue
		}
		args := json.RawMessage(argStr)
		if len(args) == 0 {
			args = json.RawMessage("{}")
		}
		ch <- CompletionEvent{
			Type: EventToolCall,
			ToolCall: &ToolCall{
				ID:   id,
				Name: name,
				Args: args,
			},
		}
	}
}

// openAIToolCallFields reads tool call data from a union. The streaming accumulator
// populates flat ID/Function fields; AsAny() only works when Type is set.
func openAIToolCallFields(tc openai.ChatCompletionMessageToolCallUnion) (id, name, args string) {
	id = tc.ID
	name = tc.Function.Name
	args = tc.Function.Arguments
	if name != "" || id != "" {
		return id, name, args
	}
	switch v := tc.AsAny().(type) {
	case openai.ChatCompletionMessageFunctionToolCall:
		return v.ID, v.Function.Name, v.Function.Arguments
	case openai.ChatCompletionMessageCustomToolCall:
		return v.ID, v.Custom.Name, v.Custom.Input
	default:
		return "", "", ""
	}
}
