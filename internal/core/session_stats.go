package core

import (
	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
	"github.com/phrony-platform/runtime/internal/provider"
)

func tokenUsageToProto(u provider.TokenUsage) *runtimev1.TokenUsage {
	total := u.Total()
	if total == 0 && !u.Estimated {
		return nil
	}
	return &runtimev1.TokenUsage{
		InputTokens:  int32(u.InputTokens),
		OutputTokens: int32(u.OutputTokens),
		TotalTokens:  int32(total),
		Estimated:    u.Estimated,
	}
}

func interactiveSessionStats(turn int, turnUsage, sessionUsage provider.TokenUsage) *runtimev1.InteractiveSessionStats {
	return &runtimev1.InteractiveSessionStats{
		Turn:         int32(turn),
		TurnUsage:    tokenUsageToProto(turnUsage),
		SessionUsage: tokenUsageToProto(sessionUsage),
	}
}
