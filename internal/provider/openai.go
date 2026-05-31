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
	for stream.Next() {
		chunk := stream.Current()
		if chunk.Usage.PromptTokens > 0 || chunk.Usage.CompletionTokens > 0 {
			usage = TokenUsage{
				InputTokens:  int(chunk.Usage.PromptTokens),
				OutputTokens: int(chunk.Usage.CompletionTokens),
			}
		}
		if len(chunk.Choices) == 0 {
			continue
		}
		choice := chunk.Choices[0]
		if choice.Delta.Content != "" {
			ch <- CompletionEvent{Type: EventTextDelta, TextDelta: choice.Delta.Content}
		}
		if choice.FinishReason != "" {
			stopReason = string(choice.FinishReason)
		}
	}
	if err := stream.Err(); err != nil {
		emitFailed(ch, err)
		return err
	}

	ch <- CompletionEvent{Type: EventCompleted, StopReason: stopReason, Usage: usage}
	return nil
}

func openAIParams(req CompletionRequest) (openai.ChatCompletionNewParams, error) {
	model := strings.TrimSpace(req.Model)
	if model == "" {
		return openai.ChatCompletionNewParams{}, fmt.Errorf("model name is required")
	}

	var messages []openai.ChatCompletionMessageParamUnion
	for _, m := range req.Messages {
		content := m.Content
		switch strings.TrimSpace(m.Role) {
		case RoleSystem:
			messages = append(messages, openai.SystemMessage(content))
		case RoleUser:
			messages = append(messages, openai.UserMessage(content))
		case RoleAssistant:
			messages = append(messages, openai.AssistantMessage(content))
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
	applyOpenAIParameters(&params, req.Parameters)
	applyOpenAIReasoning(&params, req.Reasoning)
	return params, nil
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
