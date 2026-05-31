package executor

import (
	"github.com/phrony-platform/runtime/internal/provider"
)

type usageEstimator struct {
	inputTokens  int
	outputTokens int
}

func newUsageEstimator() *usageEstimator {
	return &usageEstimator{}
}

func (e *usageEstimator) addInput(text string) {
	if e == nil {
		return
	}
	e.inputTokens += estimateTokens(text)
}

func (e *usageEstimator) addOutput(text string) {
	if e == nil {
		return
	}
	e.outputTokens += estimateTokens(text)
}

func (e *usageEstimator) usage() provider.TokenUsage {
	if e == nil {
		return provider.TokenUsage{Estimated: true}
	}
	return provider.TokenUsage{
		InputTokens:  e.inputTokens,
		OutputTokens: e.outputTokens,
		Estimated:    true,
	}
}
