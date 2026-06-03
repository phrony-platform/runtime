package executor

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/phrony-platform/runtime/internal/manifest"
	"github.com/phrony-platform/runtime/internal/provider"
	"github.com/phrony-platform/runtime/internal/tooldispatch"
)

func buildToolDefinitions(agent *manifest.Agent) ([]provider.ToolDefinition, error) {
	if agent == nil || len(agent.Spec.Tools) == 0 {
		return nil, nil
	}
	out := make([]provider.ToolDefinition, 0, len(agent.Spec.Tools))
	for _, tb := range agent.Spec.Tools {
		schema, err := toolInputSchema(tb.BindingSchema())
		if err != nil {
			return nil, fmt.Errorf("tool %q parameters: %w", tb.Ref, err)
		}
		out = append(out, provider.ToolDefinition{
			Name:        tb.ToolName(),
			Description: tb.Description,
			InputSchema: schema,
		})
	}
	return out, nil
}

func toolInputSchema(spec *manifest.SchemaSpec) (json.RawMessage, error) {
	if spec == nil || len(spec.Inline) == 0 {
		return json.RawMessage(`{"type":"object"}`), nil
	}
	raw, err := json.Marshal(spec.Inline)
	if err != nil {
		return nil, err
	}
	return raw, nil
}

func findToolBinding(agent *manifest.Agent, wireName string) (*manifest.ToolBinding, error) {
	if agent == nil {
		return nil, fmt.Errorf("agent manifest is required")
	}
	for i := range agent.Spec.Tools {
		tb := &agent.Spec.Tools[i]
		if tb.ToolName() == wireName {
			return tb, nil
		}
	}
	return nil, fmt.Errorf("unknown tool %q", wireName)
}

func formatToolResult(res tooldispatch.ToolResult) (content string, isError bool) {
	if res.Err != nil {
		return res.Err.Error(), true
	}
	if len(res.Payload) == 0 {
		return "{}", false
	}
	return string(res.Payload), false
}

func buildToolDispatchCall(
	agent *manifest.Agent,
	sessionID, agentVersionID string,
	turn, index int,
	call provider.ToolCall,
	deadline time.Time,
) (tooldispatch.ToolCall, error) {
	tb, err := findToolBinding(agent, call.Name)
	if err != nil {
		return tooldispatch.ToolCall{}, err
	}
	toolRef := tb.DispatchRef()
	version := strings.TrimSpace(tb.Version)
	if version == "" {
		version = "default"
	}
	agentKey := strings.TrimSpace(agent.Metadata.Namespace) + "/" + strings.TrimSpace(agent.Metadata.Name)
	return tooldispatch.ToolCall{
		CallID:          tooldispatch.DeriveCallID(sessionID, agentVersionID, turn, index),
		SessionID:       sessionID,
		AgentVersionID:  agentVersionID,
		AgentKey:        agentKey,
		Turn:            turn,
		Tool:            toolRef,
		Version:         version,
		Args:            call.Args,
		SideEffectClass: tb.SideEffectClass,
		Deadline:        deadline,
	}, nil
}
