package executor

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/phrony-platform/runtime/internal/manifest"
	"github.com/phrony-platform/runtime/internal/provider"
)

func TestUsageEstimator_countsInputAndOutput(t *testing.T) {
	est := newUsageEstimator()
	est.addInput(stringsRepeat("abcd", 6)) // 24 chars -> 6 tokens
	est.addOutput("hello world")

	u := est.usage()
	if !u.Estimated {
		t.Fatal("want estimated usage")
	}
	if u.InputTokens != 6 {
		t.Fatalf("input tokens = %d, want 6", u.InputTokens)
	}
	if u.OutputTokens < 1 {
		t.Fatalf("output tokens = %d, want >= 1", u.OutputTokens)
	}
}

func TestUsageEstimator_nilReceiver(t *testing.T) {
	var est *usageEstimator
	u := est.usage()
	if !u.Estimated {
		t.Fatal("nil estimator should return estimated marker")
	}
}

func TestStreamCompletion_usesProviderUsage(t *testing.T) {
	v := NewVersionWithProvider("v", testAgentForUsage(), &usageRecordingProvider{})
	ch := make(chan Event, 8)
	if err := v.StreamCompletion(context.Background(), RunParams{
		Input: json.RawMessage(`{"message":"hi"}`),
	}, ch); err != nil {
		t.Fatalf("StreamCompletion: %v", err)
	}
	var completed Event
	for ev := range ch {
		if ev.Type == EventCompleted {
			completed = ev
		}
	}
	if completed.Usage.InputTokens != 100 || completed.Usage.OutputTokens != 25 {
		t.Fatalf("usage = %+v, want provider-reported counts", completed.Usage)
	}
}

func stringsRepeat(s string, n int) string {
	out := make([]byte, 0, len(s)*n)
	for range n {
		out = append(out, s...)
	}
	return string(out)
}

func testAgentForUsage() *manifest.Agent {
	return &manifest.Agent{
		Spec: manifest.AgentSpec{
			Instructions: manifest.InstructionsSpec{Text: "System."},
			Model:        manifest.ModelConfig{Provider: provider.IDAnthropic, Name: "m"},
		},
	}
}

type usageRecordingProvider struct{}

func (u *usageRecordingProvider) ID() string { return provider.IDAnthropic }

func (u *usageRecordingProvider) Complete(ctx context.Context, req provider.CompletionRequest, ch chan<- provider.CompletionEvent) error {
	defer close(ch)
	ch <- provider.CompletionEvent{Type: provider.EventCompleted, StopReason: "end_turn", Usage: provider.TokenUsage{
		InputTokens:  100,
		OutputTokens: 25,
	}}
	return nil
}
