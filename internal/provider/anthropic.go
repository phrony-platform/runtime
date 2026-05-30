package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/phrony-platform/runtime/internal/manifest"
)

const defaultAnthropicMaxTokens = 4096

type anthropicProvider struct {
	client anthropic.Client
}

func newAnthropicProvider(apiKey string) Provider {
	return &anthropicProvider{
		client: anthropic.NewClient(option.WithAPIKey(apiKey)),
	}
}

func (p *anthropicProvider) ID() string { return IDAnthropic }

func (p *anthropicProvider) Complete(ctx context.Context, req CompletionRequest, ch chan<- CompletionEvent) error {
	defer close(ch)

	params, err := anthropicParams(req)
	if err != nil {
		emitFailed(ch, err)
		return err
	}

	stream := p.client.Messages.NewStreaming(ctx, params)
	stopReason := ""
	for stream.Next() {
		event := stream.Current()
		switch ev := event.AsAny().(type) {
		case anthropic.ContentBlockDeltaEvent:
			switch d := ev.Delta.AsAny().(type) {
			case anthropic.TextDelta:
				if d.Text != "" {
					ch <- CompletionEvent{Type: EventTextDelta, TextDelta: d.Text}
				}
			}
		case anthropic.MessageDeltaEvent:
			if ev.Delta.StopReason != "" {
				stopReason = string(ev.Delta.StopReason)
			}
		}
	}
	if err := stream.Err(); err != nil {
		emitFailed(ch, err)
		return err
	}

	ch <- CompletionEvent{Type: EventCompleted, StopReason: stopReason}
	return nil
}

func anthropicParams(req CompletionRequest) (anthropic.MessageNewParams, error) {
	model := strings.TrimSpace(req.Model)
	if model == "" {
		return anthropic.MessageNewParams{}, fmt.Errorf("model name is required")
	}

	var system []anthropic.TextBlockParam
	var messages []anthropic.MessageParam
	for _, m := range req.Messages {
		switch strings.TrimSpace(m.Role) {
		case RoleSystem:
			if strings.TrimSpace(m.Content) != "" {
				system = append(system, anthropic.TextBlockParam{Text: m.Content})
			}
		case RoleUser:
			messages = append(messages, anthropic.NewUserMessage(anthropic.NewTextBlock(m.Content)))
		case RoleAssistant:
			messages = append(messages, anthropic.NewAssistantMessage(anthropic.NewTextBlock(m.Content)))
		default:
			return anthropic.MessageNewParams{}, fmt.Errorf("unsupported message role %q", m.Role)
		}
	}
	if len(messages) == 0 {
		return anthropic.MessageNewParams{}, fmt.Errorf("at least one user or assistant message is required")
	}

	maxTokens := int64(defaultAnthropicMaxTokens)
	if req.Parameters != nil && req.Parameters.MaxOutputTokens != nil && *req.Parameters.MaxOutputTokens > 0 {
		maxTokens = int64(*req.Parameters.MaxOutputTokens)
	}

	params := anthropic.MessageNewParams{
		Model:     anthropic.Model(model),
		MaxTokens: maxTokens,
		Messages:  messages,
	}
	if len(system) > 0 {
		params.System = system
	}
	applyAnthropicParameters(&params, req.Parameters)
	applyAnthropicReasoning(&params, req.Reasoning)
	return params, nil
}

func applyAnthropicParameters(params *anthropic.MessageNewParams, p *manifest.ModelParameters) {
	if p == nil {
		return
	}
	if p.Temperature != nil {
		params.Temperature = anthropic.Float(*p.Temperature)
	}
	if p.TopP != nil {
		params.TopP = anthropic.Float(*p.TopP)
	}
	if len(p.StopSequences) > 0 {
		params.StopSequences = append([]string(nil), p.StopSequences...)
	}
}

func applyAnthropicReasoning(params *anthropic.MessageNewParams, r *manifest.ReasoningConfig) {
	if r == nil {
		return
	}
	effort := strings.TrimSpace(r.Effort)
	if effort == "" {
		return
	}
	switch effort {
	case "low", "medium", "high":
		params.Thinking = anthropic.ThinkingConfigParamOfEnabled(anthropicReasoningBudget(effort))
	}
}

func anthropicReasoningBudget(effort string) int64 {
	switch effort {
	case "low":
		return 1024
	case "high":
		return 16384
	default:
		return 8192
	}
}
