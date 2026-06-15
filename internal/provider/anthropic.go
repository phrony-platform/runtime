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
		client: anthropic.NewClient(
			option.WithAPIKey(apiKey),
			option.WithMaxRetries(3),
		),
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
	var acc anthropic.Message
	stopReason := ""
	var toolInputBlockID string
	for stream.Next() {
		event := stream.Current()
		if err := acc.Accumulate(event); err != nil {
			emitFailed(ch, err)
			return err
		}
		switch ev := event.AsAny().(type) {
		case anthropic.ContentBlockStartEvent:
			if tu := ev.ContentBlock.AsToolUse(); tu.ID != "" {
				toolInputBlockID = tu.ID
			}
		case anthropic.ContentBlockDeltaEvent:
			switch d := ev.Delta.AsAny().(type) {
			case anthropic.TextDelta:
				if d.Text != "" {
					ch <- CompletionEvent{Type: EventTextDelta, TextDelta: d.Text}
				}
			case anthropic.InputJSONDelta:
				if d.PartialJSON != "" && toolInputBlockID != "" {
					ch <- CompletionEvent{
						Type:           EventToolInputDelta,
						ToolCallID:     toolInputBlockID,
						ToolInputDelta: d.PartialJSON,
					}
				}
			}
		case anthropic.ContentBlockStopEvent:
			toolInputBlockID = ""
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

	emitAnthropicToolCalls(ch, acc)

	usage := TokenUsage{
		InputTokens:  int(acc.Usage.InputTokens),
		OutputTokens: int(acc.Usage.OutputTokens),
	}
	if stopReason == "" {
		stopReason = string(acc.StopReason)
	}
	ch <- CompletionEvent{
		Type:       EventCompleted,
		StopReason: NormalizeStopReason(stopReason),
		Usage:      usage,
	}
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
		role := strings.TrimSpace(m.Role)
		switch role {
		case RoleSystem:
			if strings.TrimSpace(m.Content) != "" {
				system = append(system, anthropic.TextBlockParam{Text: m.Content})
			}
		case RoleUser:
			blocks, err := anthropicContentBlocks(m)
			if err != nil {
				return anthropic.MessageNewParams{}, err
			}
			messages = append(messages, anthropic.NewUserMessage(blocks...))
		case RoleAssistant:
			blocks, err := anthropicContentBlocks(m)
			if err != nil {
				return anthropic.MessageNewParams{}, err
			}
			messages = append(messages, anthropic.NewAssistantMessage(blocks...))
		case RoleTool:
			blocks, err := anthropicToolResultAsUser(m)
			if err != nil {
				return anthropic.MessageNewParams{}, err
			}
			messages = append(messages, anthropic.NewUserMessage(blocks...))
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
	tools, err := anthropicTools(req.Tools)
	if err != nil {
		return anthropic.MessageNewParams{}, err
	}
	if len(tools) > 0 {
		params.Tools = tools
	}
	applyAnthropicParameters(&params, req.Parameters)
	applyAnthropicReasoning(&params, req.Reasoning)
	return params, nil
}

func anthropicToolResultAsUser(m Message) ([]anthropic.ContentBlockParamUnion, error) {
	if strings.TrimSpace(m.ToolCallID) != "" && len(m.Blocks) == 0 && m.Content != "" {
		return anthropicContentBlocks(Message{
			Blocks: []ContentBlock{ToolResultBlock(m.ToolCallID, m.Content, false)},
		})
	}
	return anthropicContentBlocks(m)
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
