package core

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/phrony-platform/runtime/internal/provider"
	"github.com/phrony-platform/runtime/internal/store"
)

type historyMessage struct {
	Role           string              `json:"role"`
	Content        string              `json:"content"`
	StopReason     string              `json:"stop_reason,omitempty"`
	TurnUsage      *sessionOutputUsage `json:"turn_usage,omitempty"`
	TurnDurationMs int64               `json:"turn_duration_ms,omitempty"`
}

func encodeHistory(messages []provider.Message) (json.RawMessage, error) {
	if len(messages) == 0 {
		return json.RawMessage("[]"), nil
	}
	out := make([]historyMessage, len(messages))
	for i, m := range messages {
		item := historyMessage{Role: m.Role, Content: m.Content}
		if m.Role == provider.RoleAssistant {
			item.StopReason = m.StopReason
			item.TurnUsage = usageToSessionOutput(m.TurnUsage)
			item.TurnDurationMs = m.TurnDurationMs
		}
		out[i] = item
	}
	b, err := json.Marshal(out)
	if err != nil {
		return nil, fmt.Errorf("encode history: %w", err)
	}
	return b, nil
}

func decodeHistory(raw json.RawMessage) ([]provider.Message, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var items []historyMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, fmt.Errorf("decode history: %w", err)
	}
	out := make([]provider.Message, len(items))
	for i, item := range items {
		msg := provider.Message{Role: item.Role, Content: item.Content}
		if item.Role == provider.RoleAssistant {
			msg.StopReason = item.StopReason
			msg.TurnUsage = usageFromSessionOutput(item.TurnUsage)
			msg.TurnDurationMs = item.TurnDurationMs
		}
		out[i] = msg
	}
	return out, nil
}

// buildProviderContext folds conversation and tool-result events into the LLM message list.
func buildProviderContext(events []store.Event) []provider.Message {
	var out []provider.Message
	for _, ev := range events {
		switch ev.Type {
		case EventMessageUser, EventMessageAssistant:
			msg, err := conversationMessageFromSessionEvent(ev.Payload)
			if err != nil {
				continue
			}
			pm := provider.Message{Role: msg.GetRole(), Content: msg.GetContent()}
			if msg.GetRole() == provider.RoleAssistant {
				pm.StopReason = msg.GetStopReason()
				pm.TurnUsage = tokenUsageFromProto(msg.GetTurnUsage())
				pm.TurnDurationMs = msg.GetTurnDurationMs()
			}
			out = append(out, pm)
		case EventToolRequested:
			// tool calls are represented in assistant blocks at dispatch time.
		case EventToolCompleted, EventToolPolicyDenied:
			var body struct {
				Result       json.RawMessage `json:"result"`
				ErrorCode    string          `json:"error_code"`
				ErrorMessage string          `json:"error_message"`
				Error        string          `json:"error"`
			}
			if err := json.Unmarshal(ev.Payload, &body); err != nil {
				continue
			}
			callID := ""
			if ev.CallID != nil {
				callID = *ev.CallID
			}
			denied := ev.Type == EventToolPolicyDenied
			content := string(body.Result)
			if body.ErrorMessage != "" {
				content = body.ErrorMessage
			} else if body.Error != "" {
				content = body.Error
			}
			out = append(out, provider.Message{
				Role: provider.RoleUser,
				Blocks: []provider.ContentBlock{
					provider.ToolResultBlock(callID, content, denied),
				},
			})
		}
	}
	return out
}

// loadProviderContext loads the folded provider message list for a session.
func loadProviderContext(ctx context.Context, q *store.Queries, sessionID string) ([]provider.Message, error) {
	events, err := q.ListEventsBySession(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	return buildProviderContext(events), nil
}
