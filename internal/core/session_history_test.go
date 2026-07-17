package core

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/phrony-platform/runtime/internal/executor"
	"github.com/phrony-platform/runtime/internal/manifest"
	"github.com/phrony-platform/runtime/internal/policy"
	"github.com/phrony-platform/runtime/internal/provider"
	"github.com/phrony-platform/runtime/internal/store"
)

func TestBuildProviderContext_synthesizesToolUseFromApproval(t *testing.T) {
	callID := "run_sess:av:4:0"
	approvalMsg := approvalRequiredServerMsg(policy.ApprovalRequest{
		ApprovalID: "appr-1", CallID: callID, Tool: "payments.issue-refund", Version: "1.0.0",
		Args: json.RawMessage(`{"amount":10}`),
	})
	events := []store.Event{
		{Type: EventMessageUser, Payload: userMessagePayload("refund please")},
		{Type: EventApprovalRequired, CallID: &callID, Payload: marshalSessionEventProto(approvalMsg)},
		{Type: EventToolCompleted, CallID: &callID, Payload: json.RawMessage(`{"result":{"ok":true}}`)},
		{Type: EventMessageAssistant, Payload: assistantMessagePayload("done", "end_turn", provider.TokenUsage{}, 0)},
	}
	ctx := buildProviderContext(events)
	if len(ctx) != 4 {
		t.Fatalf("len(context) = %d, want 4; %+v", len(ctx), ctx)
	}
	wantName := manifest.ReplayToolName("payments.issue-refund")
	if ctx[1].Role != provider.RoleAssistant || len(ctx[1].Blocks) != 1 || ctx[1].Blocks[0].ToolName != wantName {
		t.Fatalf("expected tool_use from approval, got %+v", ctx[1])
	}
	if ctx[2].Role != provider.RoleUser || len(ctx[2].Blocks) != 1 || ctx[2].Blocks[0].Type != provider.BlockToolResult {
		t.Fatalf("expected tool_result, got %+v", ctx[2])
	}
	if ctx[1].Blocks[0].ToolUseID != ctx[2].Blocks[0].ToolUseID {
		t.Fatalf("tool_use id %q != tool_result id %q", ctx[1].Blocks[0].ToolUseID, ctx[2].Blocks[0].ToolUseID)
	}
}

func TestBuildProviderContext_skipsOrphanToolCompleted(t *testing.T) {
	callID := "orphan-call"
	events := []store.Event{
		{Type: EventMessageUser, Payload: userMessagePayload("hi")},
		{Type: EventToolCompleted, CallID: &callID, Payload: json.RawMessage(`{"result":{"ok":true}}`)},
	}
	ctx := buildProviderContext(events)
	if len(ctx) != 1 {
		t.Fatalf("len(context) = %d, want 1 (orphan result skipped); %+v", len(ctx), ctx)
	}
}

func TestBuildProviderContext_dedupesDuplicateToolCompleted(t *testing.T) {
	callID := "call-1"
	events := []store.Event{
		{Type: EventMessageUser, Payload: userMessagePayload("hi")},
		{Type: EventToolRequested, CallID: &callID, Payload: json.RawMessage(`{"tool":"weather.get-forecast","args":{},"wire_name":"weather_get_forecast"}`)},
		{Type: EventToolCompleted, CallID: &callID, Payload: json.RawMessage(`{"result":{"temp":72}}`)},
		{Type: EventToolCompleted, CallID: &callID, Payload: json.RawMessage(`{"result":{"temp":72}}`)},
		{Type: EventMessageAssistant, Payload: assistantMessagePayload("done", "end_turn", provider.TokenUsage{}, 0)},
	}
	ctx := buildProviderContext(events)
	toolResults := 0
	for _, m := range ctx {
		for _, b := range m.Blocks {
			if b.Type == provider.BlockToolResult {
				toolResults++
			}
		}
	}
	if toolResults != 1 {
		t.Fatalf("tool_result blocks = %d, want 1 (deduped); context=%+v", toolResults, ctx)
	}
}

func TestBuildProviderContext_protoToolCallSanitizesDottedName(t *testing.T) {
	callID := "call-1"
	callMsg := toolCallServerMsg(executor.ToolCallEvent{
		CallID: callID, Tool: "support.refund-agent", Version: "1.0.0", Args: json.RawMessage(`{}`),
	}, nil)
	events := []store.Event{
		{Type: EventMessageUser, Payload: userMessagePayload("hi")},
		{Type: EventToolRequested, CallID: &callID, Payload: marshalSessionEventProto(callMsg)},
		{Type: EventToolCompleted, CallID: &callID, Payload: json.RawMessage(`{"result":{"ok":true}}`)},
	}
	ctx := buildProviderContext(events)
	if len(ctx) != 3 {
		t.Fatalf("len(context) = %d, want 3", len(ctx))
	}
	wantName := manifest.ReplayToolName("support.refund-agent")
	if ctx[1].Blocks[0].ToolName != wantName {
		t.Fatalf("tool name = %q, want %q", ctx[1].Blocks[0].ToolName, wantName)
	}
}

func TestBuildProviderContext_protoToolCallPrefersWireName(t *testing.T) {
	callID := "call-1"
	callMsg := toolCallServerMsg(executor.ToolCallEvent{
		CallID: callID, Tool: "routing.assign-queue", WireName: "assign_queue",
		Version: "1.0.0", Args: json.RawMessage(`{}`),
	}, nil)
	events := []store.Event{
		{Type: EventToolRequested, CallID: &callID, Payload: marshalSessionEventProto(callMsg)},
	}
	ctx := buildProviderContext(events)
	if len(ctx) != 1 || ctx[0].Blocks[0].ToolName != "assign_queue" {
		t.Fatalf("tool_use = %+v, want wire_name assign_queue", ctx[0])
	}
}

func TestBuildProviderContext_foldsMessagesAndToolResults(t *testing.T) {
	callID := "call-1"
	events := []store.Event{
		{Type: EventMessageUser, Payload: userMessagePayload("hi")},
		{Type: EventMessageAssistant, Payload: assistantMessagePayload("thinking", "tool_use", provider.TokenUsage{}, 0)},
		{Type: EventToolRequested, CallID: &callID, Payload: json.RawMessage(`{"tool":"weather.get-forecast","version":"1.0.0","args":{},"wire_name":"weather_get_forecast"}`)},
		{Type: EventToolCompleted, CallID: &callID, Payload: json.RawMessage(`{"result":{"temp":72}}`)},
		{Type: EventMessageAssistant, Payload: assistantMessagePayload("done", "end_turn", provider.TokenUsage{InputTokens: 5, OutputTokens: 2}, 50)},
	}
	ctx := buildProviderContext(events)
	if len(ctx) != 4 {
		t.Fatalf("len(context) = %d, want 4", len(ctx))
	}
	if ctx[0].Role != provider.RoleUser || ctx[0].Content != "hi" {
		t.Fatalf("user = %+v", ctx[0])
	}
	if ctx[1].Role != provider.RoleAssistant || ctx[1].Content != "thinking" || len(ctx[1].Blocks) != 1 || ctx[1].Blocks[0].Type != provider.BlockToolUse {
		t.Fatalf("assistant tool_use = %+v", ctx[1])
	}
	if ctx[1].Blocks[0].ToolUseID != callID || ctx[1].Blocks[0].ToolName != "weather_get_forecast" {
		t.Fatalf("tool_use block = %+v", ctx[1].Blocks[0])
	}
	if ctx[2].Role != provider.RoleUser || len(ctx[2].Blocks) != 1 {
		t.Fatalf("tool result = %+v", ctx[2])
	}
	if ctx[2].Blocks[0].ToolUseID != callID {
		t.Fatalf("tool_result id = %q, want %q", ctx[2].Blocks[0].ToolUseID, callID)
	}
	if ctx[3].Content != "done" || ctx[3].TurnUsage.InputTokens != 5 {
		t.Fatalf("final assistant = %+v", ctx[3])
	}
}

func TestBuildProviderContext_longCallIDUsesWireSafeID(t *testing.T) {
	callID := "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee:ffffffff-1111-2222-3333-444444444444:1:0"
	if len(callID) <= provider.MaxWireToolCallIDLen {
		t.Fatalf("callID len = %d, want > %d", len(callID), provider.MaxWireToolCallIDLen)
	}
	events := []store.Event{
		{Type: EventMessageUser, Payload: userMessagePayload("hi")},
		{Type: EventToolRequested, CallID: &callID, Payload: json.RawMessage(`{"tool":"weather.get-forecast","args":{}}`)},
		{Type: EventToolCompleted, CallID: &callID, Payload: json.RawMessage(`{"result":{"temp":72}}`)},
	}
	ctx := buildProviderContext(events)
	if len(ctx) != 3 {
		t.Fatalf("len(context) = %d, want 3", len(ctx))
	}
	wantID := provider.WireToolCallID(callID)
	if ctx[1].Blocks[0].ToolUseID != wantID {
		t.Fatalf("tool_use id = %q, want %q", ctx[1].Blocks[0].ToolUseID, wantID)
	}
	if ctx[2].Blocks[0].ToolUseID != wantID {
		t.Fatalf("tool_result id = %q, want %q", ctx[2].Blocks[0].ToolUseID, wantID)
	}
	if len(wantID) > provider.MaxWireToolCallIDLen {
		t.Fatalf("wire id len = %d, want <= %d", len(wantID), provider.MaxWireToolCallIDLen)
	}
}

func TestEncodeDecodeHistory_roundTrip(t *testing.T) {
	in := []provider.Message{
		{Role: provider.RoleUser, Content: "hi"},
		{Role: provider.RoleAssistant, Content: "hello"},
	}
	encoded, err := encodeHistory(in)
	if err != nil {
		t.Fatalf("encodeHistory: %v", err)
	}
	want := `[{"role":"user","content":"hi"},{"role":"assistant","content":"hello"}]`
	if string(encoded) != want {
		t.Fatalf("encoded = %s, want %s", encoded, want)
	}
	decoded, err := decodeHistory(encoded)
	if err != nil {
		t.Fatalf("decodeHistory: %v", err)
	}
	if len(decoded) != len(in) {
		t.Fatalf("len(decoded) = %d, want %d", len(decoded), len(in))
	}
	for i := range in {
		if decoded[i].Role != in[i].Role || decoded[i].Content != in[i].Content {
			t.Fatalf("decoded[%d] = %+v, want %+v", i, decoded[i], in[i])
		}
	}
}

func TestEncodeHistory_empty(t *testing.T) {
	encoded, err := encodeHistory(nil)
	if err != nil {
		t.Fatalf("encodeHistory: %v", err)
	}
	if string(encoded) != "[]" {
		t.Fatalf("encoded = %s, want []", encoded)
	}
}

func TestDecodeHistory_emptyRaw(t *testing.T) {
	decoded, err := decodeHistory(json.RawMessage(nil))
	if err != nil {
		t.Fatalf("decodeHistory: %v", err)
	}
	if decoded != nil {
		t.Fatalf("decoded = %+v, want nil", decoded)
	}
}

func TestEncodeDecodeHistory_withTurnUsage(t *testing.T) {
	in := []provider.Message{
		{Role: provider.RoleUser, Content: "hi"},
		{
			Role:       provider.RoleAssistant,
			Content:    "hello",
			StopReason: "end_turn",
			TurnUsage:  provider.TokenUsage{InputTokens: 10, OutputTokens: 5},
		},
	}
	encoded, err := encodeHistory(in)
	if err != nil {
		t.Fatalf("encodeHistory: %v", err)
	}
	if !strings.Contains(string(encoded), `"turn_usage"`) {
		t.Fatalf("encoded = %s, want turn_usage", encoded)
	}
	decoded, err := decodeHistory(encoded)
	if err != nil {
		t.Fatalf("decodeHistory: %v", err)
	}
	if decoded[1].StopReason != "end_turn" || decoded[1].TurnUsage.InputTokens != 10 {
		t.Fatalf("decoded assistant = %+v", decoded[1])
	}
}

func TestEncodeDecodeHistory_withTurnDurationMs(t *testing.T) {
	in := []provider.Message{
		{Role: provider.RoleUser, Content: "hi"},
		{
			Role:           provider.RoleAssistant,
			Content:        "hello",
			StopReason:     "end_turn",
			TurnUsage:      provider.TokenUsage{InputTokens: 10, OutputTokens: 5},
			TurnDurationMs: 2500,
		},
	}
	encoded, err := encodeHistory(in)
	if err != nil {
		t.Fatalf("encodeHistory: %v", err)
	}
	if !strings.Contains(string(encoded), `"turn_duration_ms":2500`) {
		t.Fatalf("encoded = %s, want turn_duration_ms", encoded)
	}
	decoded, err := decodeHistory(encoded)
	if err != nil {
		t.Fatalf("decodeHistory: %v", err)
	}
	if decoded[1].TurnDurationMs != 2500 {
		t.Fatalf("decoded assistant = %+v", decoded[1])
	}
	proto := historyToProto(decoded)
	if proto[1].GetTurnDurationMs() != 2500 {
		t.Fatalf("proto turn_duration_ms = %d, want 2500", proto[1].GetTurnDurationMs())
	}
}

func TestDecodeHistory_invalidJSON(t *testing.T) {
	_, err := decodeHistory(json.RawMessage(`not-json`))
	if err == nil {
		t.Fatal("decodeHistory() = nil, want error")
	}
}
