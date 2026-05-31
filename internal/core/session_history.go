package core

import (
	"encoding/json"
	"fmt"

	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
	"github.com/phrony-platform/runtime/internal/provider"
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

func historyToProto(messages []provider.Message) []*runtimev1.InteractiveConversationMessage {
	if len(messages) == 0 {
		return nil
	}
	out := make([]*runtimev1.InteractiveConversationMessage, len(messages))
	for i, m := range messages {
		msg := &runtimev1.InteractiveConversationMessage{
			Role:    m.Role,
			Content: m.Content,
		}
		if m.Role == provider.RoleAssistant {
			msg.StopReason = m.StopReason
			msg.TurnUsage = tokenUsageToProto(m.TurnUsage)
			msg.TurnDurationMs = m.TurnDurationMs
		}
		out[i] = msg
	}
	return out
}
