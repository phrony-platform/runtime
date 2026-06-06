// Proto conversion helpers for the runtime core package.
//
// Layer boundaries (types are intentionally separate; do not merge):
//   - provider.TokenUsage — domain model from model providers and the executor
//   - sessionOutputUsage — JSON persisted on sessions (see session_output.go)
//   - runtimev1.TokenUsage — gRPC wire messages
package core

import (
	"encoding/json"

	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
	"github.com/phrony-platform/runtime/internal/provider"
)

func tokenUsageToProto(u provider.TokenUsage) *runtimev1.TokenUsage {
	if u.IsZero() && !u.Estimated {
		return nil
	}
	total := u.Total()
	return &runtimev1.TokenUsage{
		InputTokens:  int32(u.InputTokens),
		OutputTokens: int32(u.OutputTokens),
		TotalTokens:  int32(total),
		Estimated:    u.Estimated,
	}
}

func tokenUsageFromProto(u *runtimev1.TokenUsage) provider.TokenUsage {
	if u == nil {
		return provider.TokenUsage{}
	}
	return provider.TokenUsage{
		InputTokens:  int(u.GetInputTokens()),
		OutputTokens: int(u.GetOutputTokens()),
		Estimated:    u.GetEstimated(),
	}
}

func interactiveSessionStats(turn int, turnUsage, sessionUsage provider.TokenUsage) *runtimev1.InteractiveSessionStats {
	return &runtimev1.InteractiveSessionStats{
		Turn:         int32(turn),
		TurnUsage:    tokenUsageToProto(turnUsage),
		SessionUsage: tokenUsageToProto(sessionUsage),
	}
}

// interactiveStatsFromSessionOutput builds wire stats for completed-session attach replay.
func interactiveStatsFromSessionOutput(history []provider.Message, output json.RawMessage) *runtimev1.InteractiveSessionStats {
	turnUsage, sessionUsage := usageFromSessionOutputJSON(output)
	turnCount := 0
	for _, m := range history {
		if m.Role == provider.RoleAssistant {
			turnCount++
		}
	}
	return interactiveSessionStats(turnCount, turnUsage, sessionUsage)
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
