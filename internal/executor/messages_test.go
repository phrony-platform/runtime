package executor

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/phrony-platform/runtime/internal/manifest"
	"github.com/phrony-platform/runtime/internal/provider"
)

func TestBuildMessages_inlineInstructions(t *testing.T) {
	agent := &manifest.Agent{
		Spec: manifest.AgentSpec{
			Instructions: manifest.InstructionsSpec{Text: "You are helpful."},
		},
	}
	msgs, err := buildMessages(agent, json.RawMessage(`{"message":"hello"}`))
	if err != nil {
		t.Fatalf("buildMessages: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("messages len = %d, want 2", len(msgs))
	}
	if msgs[0].Role != provider.RoleSystem || msgs[0].Content != "You are helpful." {
		t.Fatalf("system = %+v", msgs[0])
	}
	if msgs[1].Role != provider.RoleUser || msgs[1].Content != "hello" {
		t.Fatalf("user = %+v", msgs[1])
	}
}

func TestBuildMessages_unresolvedRef(t *testing.T) {
	agent := &manifest.Agent{
		Spec: manifest.AgentSpec{
			Instructions: manifest.InstructionsSpec{Ref: "prompts/system"},
		},
	}
	_, err := buildMessages(agent, json.RawMessage(`{"message":"hi"}`))
	if err == nil {
		t.Fatal("buildMessages() = nil, want error")
	}
	if !strings.Contains(err.Error(), "not resolved") {
		t.Fatalf("err = %v", err)
	}
}

func TestUserMessageFromInput_messageField(t *testing.T) {
	msg, err := userMessageFromInput(json.RawMessage(`{"message":"hello"}`))
	if err != nil {
		t.Fatalf("userMessageFromInput: %v", err)
	}
	if msg != "hello" {
		t.Fatalf("message = %q", msg)
	}
}

func TestUserMessageFromInput_fallbackObject(t *testing.T) {
	msg, err := userMessageFromInput(json.RawMessage(`{"claim_id":"c-42"}`))
	if err != nil {
		t.Fatalf("userMessageFromInput: %v", err)
	}
	if !strings.Contains(msg, "claim_id") {
		t.Fatalf("message = %q, want encoded input", msg)
	}
}

func TestBuildMessages_userOnly(t *testing.T) {
	agent := &manifest.Agent{
		Spec: manifest.AgentSpec{
			Instructions: manifest.InstructionsSpec{},
		},
	}
	msgs, err := buildMessages(agent, json.RawMessage(`{"message":"hello"}`))
	if err != nil {
		t.Fatalf("buildMessages: %v", err)
	}
	if len(msgs) != 1 || msgs[0].Role != provider.RoleUser {
		t.Fatalf("messages = %+v", msgs)
	}
}

func TestUserMessageFromInput_emptyObject(t *testing.T) {
	msg, err := userMessageFromInput(json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("userMessageFromInput: %v", err)
	}
	if msg != "" {
		t.Fatalf("message = %q, want empty", msg)
	}
}
