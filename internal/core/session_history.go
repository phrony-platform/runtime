package core

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/phrony-platform/runtime/internal/manifest"
	"github.com/phrony-platform/runtime/internal/provider"
	"github.com/phrony-platform/runtime/internal/store"
)

type historyMessage struct {
	Role           string              `json:"role"`
	Content        string              `json:"content"`
	StopReason     string              `json:"stop_reason,omitempty"`
	TurnUsage      *sessionOutputUsage `json:"turn_usage,omitempty"`
	TurnDurationMs int64               `json:"turn_duration_ms,omitempty"`
}

func encodeHistory(messages []provider.Message) (json.RawMessage, error) {
	if len(messages) == 0 {
		return json.RawMessage("[]"), nil
	}
	out := make([]historyMessage, len(messages))
	for i, m := range messages {
		item := historyMessage{Role: m.Role, Content: m.Content}
		if m.Role == provider.RoleAssistant {
			item.StopReason = m.StopReason
			item.TurnUsage = usageToSessionOutput(m.TurnUsage)
			item.TurnDurationMs = m.TurnDurationMs
		}
		out[i] = item
	}
	b, err := json.Marshal(out)
	if err != nil {
		return nil, fmt.Errorf("encode history: %w", err)
	}
	return b, nil
}

func decodeHistory(raw json.RawMessage) ([]provider.Message, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var items []historyMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, fmt.Errorf("decode history: %w", err)
	}
	out := make([]provider.Message, len(items))
	for i, item := range items {
		msg := provider.Message{Role: item.Role, Content: item.Content}
		if item.Role == provider.RoleAssistant {
			msg.StopReason = item.StopReason
			msg.TurnUsage = usageFromSessionOutput(item.TurnUsage)
			msg.TurnDurationMs = item.TurnDurationMs
		}
		out[i] = msg
	}
	return out, nil
}

type parsedToolRequest struct {
	callID   string
	toolName string
	args     json.RawMessage
}

func parseToolRequestEvent(ev store.Event) (parsedToolRequest, bool) {
	req := parsedToolRequest{}
	if ev.CallID != nil {
		req.callID = strings.TrimSpace(*ev.CallID)
	}
	if msg, err := serverMsgFromSessionEvent(ev.Payload); err == nil {
		if tc := msg.GetToolCall(); tc != nil {
			if req.callID == "" {
				req.callID = strings.TrimSpace(tc.GetCallId())
			}
			if wireName := strings.TrimSpace(tc.GetWireName()); wireName != "" {
				req.toolName = wireName
			} else {
				req.toolName = manifest.ReplayToolName(strings.TrimSpace(tc.GetTool()))
			}
			req.args = tc.GetArgs()
			if len(req.args) == 0 {
				req.args = json.RawMessage("{}")
			}
			return req, req.callID != "" && req.toolName != ""
		}
	}
	var body struct {
		Tool     string          `json:"tool"`
		Args     json.RawMessage `json:"args"`
		WireName string          `json:"wire_name"`
	}
	if err := json.Unmarshal(ev.Payload, &body); err != nil {
		return parsedToolRequest{}, false
	}
	if req.callID == "" {
		return parsedToolRequest{}, false
	}
	if wireName := strings.TrimSpace(body.WireName); wireName != "" {
		req.toolName = wireName
	} else {
		req.toolName = manifest.ReplayToolName(body.Tool)
	}
	req.args = body.Args
	if len(req.args) == 0 {
		req.args = json.RawMessage("{}")
	}
	return req, req.toolName != ""
}

func parseToolResultEvent(ev store.Event) (callID, content string, denied bool, ok bool) {
	if ev.CallID != nil {
		callID = strings.TrimSpace(*ev.CallID)
	}
	denied = ev.Type == EventToolPolicyDenied
	if msg, err := serverMsgFromSessionEvent(ev.Payload); err == nil {
		if tr := msg.GetToolResult(); tr != nil {
			if callID == "" {
				callID = strings.TrimSpace(tr.GetCallId())
			}
			if tr.GetErrorMessage() != "" {
				content = tr.GetErrorMessage()
			} else if len(tr.GetPayload()) > 0 {
				content = string(tr.GetPayload())
			}
			return callID, content, denied, callID != ""
		}
	}
	var body struct {
		Result       json.RawMessage `json:"result"`
		ErrorCode    string          `json:"error_code"`
		ErrorMessage string          `json:"error_message"`
		Error        string          `json:"error"`
	}
	if err := json.Unmarshal(ev.Payload, &body); err != nil {
		return "", "", denied, false
	}
	if body.ErrorMessage != "" {
		content = body.ErrorMessage
	} else if body.Error != "" {
		content = body.Error
	} else {
		content = string(body.Result)
	}
	return callID, content, denied, callID != ""
}

// parseApprovalToolRequest extracts a synthetic tool_use from approval.required so
// folded history still pairs tool results when tool.requested was never recorded
// (e.g. HITL before emitToolCall, or resume paths that only persist completion).
func parseApprovalToolRequest(ev store.Event) (parsedToolRequest, bool) {
	req := parsedToolRequest{}
	if ev.CallID != nil {
		req.callID = strings.TrimSpace(*ev.CallID)
	}
	if msg, err := serverMsgFromSessionEvent(ev.Payload); err == nil {
		if ar := msg.GetApprovalRequired(); ar != nil {
			if req.callID == "" {
				req.callID = strings.TrimSpace(ar.GetCallId())
			}
			req.toolName = strings.TrimSpace(ar.GetTool())
			req.args = ar.GetArgs()
			if len(req.args) == 0 {
				req.args = json.RawMessage("{}")
			}
			if req.toolName != "" {
				req.toolName = manifest.ReplayToolName(req.toolName)
			}
			return req, req.callID != "" && req.toolName != ""
		}
	}
	var open struct {
		CallID  string          `json:"call_id"`
		Tool    string          `json:"tool"`
		Args    json.RawMessage `json:"args"`
		Version string          `json:"version"`
	}
	if err := json.Unmarshal(ev.Payload, &open); err != nil {
		return parsedToolRequest{}, false
	}
	if req.callID == "" {
		req.callID = strings.TrimSpace(open.CallID)
	}
	req.toolName = manifest.ReplayToolName(open.Tool)
	req.args = open.Args
	if len(req.args) == 0 {
		req.args = json.RawMessage("{}")
	}
	return req, req.callID != "" && req.toolName != ""
}

func noteToolUse(seen map[string]struct{}, out *[]provider.Message, req parsedToolRequest) {
	if _, dup := seen[req.callID]; dup {
		return
	}
	seen[req.callID] = struct{}{}
	appendAssistantToolUse(out, provider.WireToolCallID(req.callID), req.toolName, req.args)
}

func appendAssistantToolUse(out *[]provider.Message, wireID, toolName string, args json.RawMessage) {
	block := provider.ToolUseBlock(wireID, manifest.ReplayToolName(toolName), args)
	if n := len(*out); n > 0 {
		last := &(*out)[n-1]
		if last.Role == provider.RoleAssistant {
			last.Blocks = append(last.Blocks, block)
			return
		}
	}
	*out = append(*out, provider.Message{
		Role:   provider.RoleAssistant,
		Blocks: []provider.ContentBlock{block},
	})
}

func appendToolResultMessage(out *[]provider.Message, callID, content string, denied bool) {
	if len(content) == 0 {
		content = "{}"
	}
	*out = append(*out, provider.Message{
		Role: provider.RoleUser,
		Blocks: []provider.ContentBlock{
			provider.ToolResultBlock(provider.WireToolCallID(callID), content, denied),
		},
	})
}

// buildProviderContext folds conversation and tool-result events into the LLM message list.
func buildProviderContext(events []store.Event) []provider.Message {
	var out []provider.Message
	seenToolRequests := make(map[string]struct{})
	seenToolResults := make(map[string]struct{})
	for _, ev := range events {
		switch ev.Type {
		case EventMessageUser, EventMessageAssistant:
			msg, err := conversationMessageFromSessionEvent(ev.Payload)
			if err != nil {
				continue
			}
			pm := provider.Message{Role: msg.GetRole(), Content: msg.GetContent()}
			if msg.GetRole() == provider.RoleAssistant {
				pm.StopReason = msg.GetStopReason()
				pm.TurnUsage = tokenUsageFromProto(msg.GetTurnUsage())
				pm.TurnDurationMs = msg.GetTurnDurationMs()
			}
			out = append(out, pm)
		case EventToolRequested:
			req, ok := parseToolRequestEvent(ev)
			if !ok {
				continue
			}
			noteToolUse(seenToolRequests, &out, req)
		case EventApprovalRequired:
			req, ok := parseApprovalToolRequest(ev)
			if !ok {
				continue
			}
			noteToolUse(seenToolRequests, &out, req)
		case EventToolCompleted, EventToolPolicyDenied:
			callID, content, denied, ok := parseToolResultEvent(ev)
			if !ok {
				continue
			}
			if _, dup := seenToolResults[callID]; dup {
				continue
			}
			if _, hasUse := seenToolRequests[callID]; !hasUse {
				continue
			}
			seenToolResults[callID] = struct{}{}
			appendToolResultMessage(&out, callID, content, denied)
		}
	}
	return out
}

// loadProviderContext loads the folded provider message list for a session.
func loadProviderContext(ctx context.Context, q *store.Queries, sessionID string) ([]provider.Message, error) {
	events, err := q.ListEventsBySession(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	return buildProviderContext(events), nil
}
