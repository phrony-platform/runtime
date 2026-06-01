package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/phrony-platform/runtime/internal/manifest"
)

const defaultOpenAIMaxTokens = 4096

type openAIProvider struct {
	client openai.Client
}

func newOpenAIProvider(apiKey string) Provider {
	return &openAIProvider{
		client: openai.NewClient(option.WithAPIKey(apiKey)),
	}
}

func (p *openAIProvider) ID() string { return IDOpenAI }

func (p *openAIProvider) Complete(ctx context.Context, req CompletionRequest, ch chan<- CompletionEvent) error {
	defer close(ch)

	params, err := openAIParams(req)
	if err != nil {
		emitFailed(ch, err)
		return err
	}

	stream := p.client.Chat.Completions.NewStreaming(ctx, params)
	stopReason := ""
	var usage TokenUsage
	var acc openai.ChatCompletionAccumulator
	for stream.Next() {
		chunk := stream.Current()
		if chunk.Usage.PromptTokens > 0 || chunk.Usage.CompletionTokens > 0 {
			usage = TokenUsage{
				InputTokens:  int(chunk.Usage.PromptTokens),
				OutputTokens: int(chunk.Usage.CompletionTokens),
			}
		}
		if !acc.AddChunk(chunk) {
			err := fmt.Errorf("failed to accumulate streaming chunk")
			emitFailed(ch, err)
			return err
		}
		if len(chunk.Choices) == 0 {
			continue
		}
		choice := chunk.Choices[0]
		if choice.Delta.Content != "" {
			ch <- CompletionEvent{Type: EventTextDelta, TextDelta: choice.Delta.Content}
		}
		for _, tc := range choice.Delta.ToolCalls {
			if tc.ID != "" && tc.Function.Arguments != "" {
				ch <- CompletionEvent{
					Type:           EventToolInputDelta,
					ToolCallID:     tc.ID,
					ToolInputDelta: tc.Function.Arguments,
				}
			} else if tc.Function.Arguments != "" {
				ch <- CompletionEvent{
					Type:           EventToolInputDelta,
					ToolInputDelta: tc.Function.Arguments,
				}
			}
		}
		if choice.FinishReason != "" {
			stopReason = string(choice.FinishReason)
		}
	}
	if err := stream.Err(); err != nil {
		emitFailed(ch, err)
		return err
	}

	// Emit any tool calls not surfaced via JustFinishedToolCall during streaming.
	emitOpenAIToolCalls(ch, acc.ChatCompletion)

	ch <- CompletionEvent{
		Type:       EventCompleted,
		StopReason: NormalizeStopReason(stopReason),
		Usage:      usage,
	}
	return nil
}

func openAIParams(req CompletionRequest) (openai.ChatCompletionNewParams, error) {
	model := strings.TrimSpace(req.Model)
	if model == "" {
		return openai.ChatCompletionNewParams{}, fmt.Errorf("model name is required")
	}

	var messages []openai.ChatCompletionMessageParamUnion
	for _, m := range req.Messages {
		switch strings.TrimSpace(m.Role) {
		case RoleSystem:
			messages = append(messages, openai.SystemMessage(m.Content))
		case RoleUser:
			blocks := MessageBlocks(m)
			if len(blocks) == 1 && blocks[0].Type == BlockText {
				messages = append(messages, openai.UserMessage(blocks[0].Text))
				continue
			}
			if hasOnlyToolResults(blocks) {
				for _, b := range blocks {
					messages = append(messages, openai.ToolMessage(b.ToolResultContent, b.ToolUseID))
				}
				continue
			}
			return openai.ChatCompletionNewParams{}, fmt.Errorf("openai user messages with structured blocks must be plain text or tool_result only")
		case RoleAssistant:
			msg, err := openAIAssistantMessage(m)
			if err != nil {
				return openai.ChatCompletionNewParams{}, err
			}
			messages = append(messages, msg)
		case RoleTool:
			msg, err := openAIToolResultMessage(m)
			if err != nil {
				return openai.ChatCompletionNewParams{}, err
			}
			messages = append(messages, msg)
		default:
			return openai.ChatCompletionNewParams{}, fmt.Errorf("unsupported message role %q", m.Role)
		}
	}
	if len(messages) == 0 {
		return openai.ChatCompletionNewParams{}, fmt.Errorf("at least one message is required")
	}

	params := openai.ChatCompletionNewParams{
		Model:    openai.ChatModel(model),
		Messages: messages,
		StreamOptions: openai.ChatCompletionStreamOptionsParam{
			IncludeUsage: openai.Bool(true),
		},
	}
	tools, err := openAITools(req.Tools)
	if err != nil {
		return openai.ChatCompletionNewParams{}, err
	}
	if len(tools) > 0 {
		params.Tools = tools
	}
	applyOpenAIParameters(&params, req.Parameters)
	applyOpenAIReasoning(&params, req.Reasoning)
	return params, nil
}

func hasOnlyToolResults(blocks []ContentBlock) bool {
	if len(blocks) == 0 {
		return false
	}
	for _, b := range blocks {
		if b.Type != BlockToolResult {
			return false
		}
	}
	return true
}

func applyOpenAIParameters(params *openai.ChatCompletionNewParams, p *manifest.ModelParameters) {
	if p == nil {
		params.MaxTokens = openai.Int(defaultOpenAIMaxTokens)
		return
	}
	if p.MaxOutputTokens != nil && *p.MaxOutputTokens > 0 {
		params.MaxTokens = openai.Int(int64(*p.MaxOutputTokens))
	} else {
		params.MaxTokens = openai.Int(defaultOpenAIMaxTokens)
	}
	if p.Temperature != nil {
		params.Temperature = openai.Float(*p.Temperature)
	}
	if p.TopP != nil {
		params.TopP = openai.Float(*p.TopP)
	}
	if len(p.StopSequences) > 0 {
		params.Stop = openai.ChatCompletionNewParamsStopUnion{
			OfStringArray: append([]string(nil), p.StopSequences...),
		}
	}
}

func applyOpenAIReasoning(params *openai.ChatCompletionNewParams, r *manifest.ReasoningConfig) {
	if r == nil {
		return
	}
	effort := strings.TrimSpace(r.Effort)
	if effort == "" {
		return
	}
	switch effort {
	case "low", "medium", "high":
		params.ReasoningEffort = openai.ReasoningEffort(effort)
	}
}

func emitFailed(ch chan<- CompletionEvent, err error) {
	if err == nil {
		return
	}
	ch <- CompletionEvent{Type: EventFailed, Err: err}
}
