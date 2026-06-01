package executor

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/phrony-platform/runtime/internal/manifest"
	"github.com/phrony-platform/runtime/internal/provider"
)

func systemInstructions(agent *manifest.Agent) (string, error) {
	if agent == nil {
		return "", fmt.Errorf("agent manifest is required")
	}
	in := agent.Spec.Instructions
	if text := strings.TrimSpace(in.Text); text != "" {
		return text, nil
	}
	if strings.TrimSpace(in.Ref) != "" {
		return "", fmt.Errorf("spec.instructions.ref is not resolved; deploy a bundle-resolved manifest with inline instructions text")
	}
	return "", nil
}

func userMessageFromInput(input json.RawMessage) (string, error) {
	if len(input) == 0 {
		return "", nil
	}
	trimmed := strings.TrimSpace(string(input))
	if trimmed == "" || trimmed == "{}" || trimmed == "null" {
		return "", nil
	}

	var obj map[string]json.RawMessage
	if err := json.Unmarshal(input, &obj); err != nil {
		return "", fmt.Errorf("session input must be a JSON object: %w", err)
	}

	if raw, ok := obj["message"]; ok {
		return decodeInputString(raw, "message")
	}
	if raw, ok := obj["text"]; ok {
		return decodeInputString(raw, "text")
	}

	encoded, err := json.Marshal(obj)
	if err != nil {
		return "", fmt.Errorf("encode session input: %w", err)
	}
	return string(encoded), nil
}

func decodeInputString(raw json.RawMessage, field string) (string, error) {
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return "", fmt.Errorf("session input.%s must be a string", field)
	}
	return strings.TrimSpace(s), nil
}

func messageTextForTokens(m provider.Message) string {
	if strings.TrimSpace(m.Content) != "" {
		return m.Content
	}
	var b strings.Builder
	for _, bl := range provider.MessageBlocks(m) {
		switch bl.Type {
		case provider.BlockText:
			b.WriteString(bl.Text)
		case provider.BlockToolUse:
			b.WriteString(string(bl.Input))
		case provider.BlockToolResult:
			b.WriteString(bl.ToolResultContent)
		}
	}
	return b.String()
}

func buildMessages(agent *manifest.Agent, input json.RawMessage) ([]provider.Message, error) {
	system, err := systemInstructions(agent)
	if err != nil {
		return nil, err
	}
	user, err := userMessageFromInput(input)
	if err != nil {
		return nil, err
	}

	var messages []provider.Message
	if system != "" {
		messages = append(messages, provider.Message{Role: provider.RoleSystem, Content: system})
	}
	if user != "" {
		messages = append(messages, provider.Message{Role: provider.RoleUser, Content: user})
	}
	if len(messages) == 0 {
		return nil, fmt.Errorf("session input must include a user message or fields to send to the model")
	}
	return messages, nil
}
