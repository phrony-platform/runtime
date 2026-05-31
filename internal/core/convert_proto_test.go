package core

import (
	"testing"

	"github.com/phrony-platform/runtime/internal/provider"
)

func TestTokenUsageToProto(t *testing.T) {
	if tokenUsageToProto(provider.TokenUsage{}) != nil {
		t.Fatal("zero usage should not produce proto message")
	}
	p := tokenUsageToProto(provider.TokenUsage{InputTokens: 3, OutputTokens: 7, Estimated: true})
	if p.GetTotalTokens() != 10 {
		t.Fatalf("total = %d, want 10", p.GetTotalTokens())
	}
	if !p.GetEstimated() {
		t.Fatal("expected estimated flag")
	}
}
