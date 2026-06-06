package core

import (
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
