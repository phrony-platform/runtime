package core

import (
	"encoding/json"
	"fmt"

	"github.com/phrony-platform/runtime/internal/provider"
)

type historyMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

func encodeHistory(messages []provider.Message) (json.RawMessage, error) {
	if len(messages) == 0 {
		return json.RawMessage("[]"), nil
	}
	out := make([]historyMessage, len(messages))
	for i, m := range messages {
		out[i] = historyMessage{Role: m.Role, Content: m.Content}
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
		out[i] = provider.Message{Role: item.Role, Content: item.Content}
	}
	return out, nil
}
