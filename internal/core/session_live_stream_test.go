package core

import (
	"encoding/json"
	"testing"

	"github.com/phrony-platform/runtime/internal/provider"
)

func TestLiveAssistantReplayDelta(t *testing.T) {
	history := []provider.Message{
		{Role: provider.RoleUser, Content: "hi"},
		{Role: provider.RoleAssistant, Content: "hello"},
	}
	if got := liveAssistantReplayDelta(history, "hello world"); got != " world" {
		t.Fatalf("suffix replay = %q, want %q", got, " world")
	}
	if got := liveAssistantReplayDelta(history, "partial"); got != "partial" {
		t.Fatalf("fresh turn replay = %q, want %q", got, "partial")
	}
}

func TestPatchHistoryLastAssistantFromOutput(t *testing.T) {
	history := []provider.Message{
		{Role: provider.RoleUser, Content: "q"},
		{Role: provider.RoleAssistant, Content: "short"},
	}
	out, err := json.Marshal(sessionOutput{Message: "longer reply", StopReason: "end_turn"})
	if err != nil {
		t.Fatal(err)
	}
	patched := patchHistoryLastAssistantFromOutput(history, out)
	if patched[1].Content != "longer reply" {
		t.Fatalf("content = %q, want longer reply", patched[1].Content)
	}
}
