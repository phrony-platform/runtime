package provider

import (
	"testing"

	"github.com/phrony-platform/runtime/internal/manifest"
)

func TestAnthropicParams_mapsMessages(t *testing.T) {
	temp := 0.2
	params, err := anthropicParams(CompletionRequest{
		Model: "claude-sonnet-4-5",
		Messages: []Message{
			{Role: RoleSystem, Content: "Be helpful."},
			{Role: RoleUser, Content: "Hello"},
		},
		Parameters: &manifest.ModelParameters{
			Temperature:     &temp,
			MaxOutputTokens: intPtr(512),
			StopSequences:   []string{"END"},
		},
		Reasoning: &manifest.ReasoningConfig{Effort: "low"},
	})
	if err != nil {
		t.Fatalf("anthropicParams: %v", err)
	}
	if params.Model != "claude-sonnet-4-5" {
		t.Fatalf("model = %q", params.Model)
	}
	if params.MaxTokens != 512 {
		t.Fatalf("max_tokens = %d, want 512", params.MaxTokens)
	}
	if len(params.Messages) != 1 {
		t.Fatalf("messages len = %d, want 1", len(params.Messages))
	}
	if len(params.System) != 1 || params.System[0].Text != "Be helpful." {
		t.Fatalf("system = %+v", params.System)
	}
	if len(params.StopSequences) != 1 || params.StopSequences[0] != "END" {
		t.Fatalf("stop_sequences = %v", params.StopSequences)
	}
}

func TestOpenAIParams_mapsMessages(t *testing.T) {
	topP := 0.9
	params, err := openAIParams(CompletionRequest{
		Model: "gpt-4o",
		Messages: []Message{
			{Role: RoleSystem, Content: "Be helpful."},
			{Role: RoleUser, Content: "Hello"},
		},
		Parameters: &manifest.ModelParameters{
			TopP:            &topP,
			MaxOutputTokens: intPtr(256),
		},
		Reasoning: &manifest.ReasoningConfig{Effort: "medium"},
	})
	if err != nil {
		t.Fatalf("openAIParams: %v", err)
	}
	if params.Model != "gpt-4o" {
		t.Fatalf("model = %q", params.Model)
	}
	if params.MaxTokens.Value != 256 {
		t.Fatalf("max_tokens = %d, want 256", params.MaxTokens.Value)
	}
	if len(params.Messages) != 2 {
		t.Fatalf("messages len = %d, want 2", len(params.Messages))
	}
}

func intPtr(v int) *int { return &v }
