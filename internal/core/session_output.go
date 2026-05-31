// Session output and history JSON shapes persisted in the store.
// Domain usage values use provider.TokenUsage; wire types use runtimev1 (see convert_proto.go).
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

type sessionTurnRecord struct {
	StopReason      string              `json:"stop_reason,omitempty"`
	TurnUsage       *sessionOutputUsage `json:"turn_usage,omitempty"`
	TurnDurationMs  int64               `json:"turn_duration_ms,omitempty"`
}

type sessionOutput struct {
	Message      string              `json:"message"`
	StopReason   string              `json:"stop_reason"`
	TurnUsage    *sessionOutputUsage `json:"turn_usage,omitempty"`
	SessionUsage *sessionOutputUsage `json:"session_usage,omitempty"`
	// Turns stores per-completed-turn stats for re-attach when history rows lack turn_usage.
	Turns []sessionTurnRecord `json:"turns,omitempty"`
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

func turnRecordsFromHistory(messages []provider.Message) []sessionTurnRecord {
	var turns []sessionTurnRecord
	for _, m := range messages {
		if m.Role != provider.RoleAssistant {
			continue
		}
		turns = append(turns, sessionTurnRecord{
			StopReason:     m.StopReason,
			TurnUsage:      usageToSessionOutput(m.TurnUsage),
			TurnDurationMs: m.TurnDurationMs,
		})
	}
	return turns
}

// enrichHistoryFromSessionOutput fills missing assistant turn_usage from session output turns.
func enrichHistoryFromSessionOutput(messages []provider.Message, output json.RawMessage) []provider.Message {
	if len(messages) == 0 || len(output) == 0 {
		return messages
	}
	var obj sessionOutput
	if err := json.Unmarshal(output, &obj); err != nil {
		return messages
	}
	assistantCount := 0
	for _, m := range messages {
		if m.Role == provider.RoleAssistant {
			assistantCount++
		}
	}
	turnIdx := 0
	for i := range messages {
		if messages[i].Role != provider.RoleAssistant {
			continue
		}
		if turnIdx < len(obj.Turns) {
			rec := obj.Turns[turnIdx]
			if messages[i].TurnUsage.IsZero() {
				messages[i].StopReason = rec.StopReason
				messages[i].TurnUsage = usageFromSessionOutput(rec.TurnUsage)
			}
			if messages[i].TurnDurationMs == 0 && rec.TurnDurationMs > 0 {
				messages[i].TurnDurationMs = rec.TurnDurationMs
			}
		} else if len(obj.Turns) == 0 && obj.TurnUsage != nil && turnIdx == assistantCount-1 {
			if messages[i].TurnUsage.IsZero() {
				messages[i].StopReason = obj.StopReason
				messages[i].TurnUsage = usageFromSessionOutput(obj.TurnUsage)
			}
		}
		turnIdx++
	}
	return messages
}

func marshalSessionOutput(message, stopReason string, turnUsage, sessionUsage provider.TokenUsage, history []provider.Message) (json.RawMessage, error) {
	out := sessionOutput{
		Message:      message,
		StopReason:   stopReason,
		TurnUsage:    usageToSessionOutput(turnUsage),
		SessionUsage: usageToSessionOutput(sessionUsage),
		Turns:        turnRecordsFromHistory(history),
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
