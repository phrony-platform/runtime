package core

import (
	"encoding/json"

	"github.com/phrony-platform/runtime/internal/provider"
)

type sessionOutputUsage struct {
	InputTokens  int  `json:"input_tokens"`
	OutputTokens int  `json:"output_tokens"`
	Estimated    bool `json:"estimated,omitempty"`
}

type sessionOutput struct {
	Message      string              `json:"message"`
	StopReason   string              `json:"stop_reason"`
	TurnUsage    *sessionOutputUsage `json:"turn_usage,omitempty"`
	SessionUsage *sessionOutputUsage `json:"session_usage,omitempty"`
}

func usageToSessionOutput(u provider.TokenUsage) *sessionOutputUsage {
	if u.IsZero() {
		return nil
	}
	return &sessionOutputUsage{
		InputTokens:  u.InputTokens,
		OutputTokens: u.OutputTokens,
		Estimated:    u.Estimated,
	}
}

func usageFromSessionOutput(u *sessionOutputUsage) provider.TokenUsage {
	if u == nil {
		return provider.TokenUsage{}
	}
	return provider.TokenUsage{
		InputTokens:  u.InputTokens,
		OutputTokens: u.OutputTokens,
		Estimated:    u.Estimated,
	}
}

func marshalSessionOutput(message, stopReason string, turnUsage, sessionUsage provider.TokenUsage) (json.RawMessage, error) {
	out := sessionOutput{
		Message:      message,
		StopReason:   stopReason,
		TurnUsage:    usageToSessionOutput(turnUsage),
		SessionUsage: usageToSessionOutput(sessionUsage),
	}
	return json.Marshal(out)
}

func usageFromSessionOutputJSON(output json.RawMessage) (turnUsage, sessionUsage provider.TokenUsage) {
	if len(output) == 0 {
		return provider.TokenUsage{}, provider.TokenUsage{}
	}
	var obj sessionOutput
	if err := json.Unmarshal(output, &obj); err != nil {
		return provider.TokenUsage{}, provider.TokenUsage{}
	}
	return usageFromSessionOutput(obj.TurnUsage), usageFromSessionOutput(obj.SessionUsage)
}
