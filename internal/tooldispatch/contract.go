package tooldispatch

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// Dispatcher authorizes and routes a tool call to an application worker over
// the inbound Work gRPC stream; callers only see ToolCall -> ToolResult.
type Dispatcher interface {
	Dispatch(ctx context.Context, call ToolCall) (ToolResult, error)
}

// ToolCall is one model-emitted invocation. CallID is the idempotency key and
// must be stable across restarts for the same logical call (see DeriveCallID).
type ToolCall struct {
	CallID         string
	SessionID      string
	AgentVersionID string
	// AgentKey is namespace/name for allowlist lookup (e.g. demo/echo).
	AgentKey        string
	Turn            int
	Tool            string
	// WireName is the model-facing tool name when it differs from Tool.
	WireName        string
	Version         string
	Args            json.RawMessage
	SideEffectClass string
	Deadline        time.Time
}

// ToolKey returns the dispatch routing key tool@version.
func (c ToolCall) ToolKey() string {
	return ToolKey(c.Tool, c.Version)
}

// ToolKey formats tool@version for registry routing and capacity queues.
func ToolKey(tool, version string) string {
	if version == "" {
		return tool
	}
	return tool + "@" + version
}

// DeriveCallID returns a deterministic idempotency key for a logical tool call.
// The same inputs after a restart must yield the same CallID.
func DeriveCallID(sessionID, agentVersionID string, turn, index int) string {
	return fmt.Sprintf("%s:%s:%d:%d", sessionID, agentVersionID, turn, index)
}

// ToolResult is the outcome of a dispatched call. Handler failures are carried
// in Err; infrastructure and routing failures are returned from Dispatch.
type ToolResult struct {
	CallID  string
	Payload json.RawMessage
	Err     *ToolError
	// Usage reports token consumption attributable to producing this result
	// (e.g. a nested agent run). It is nil for backends that do no model work,
	// letting the executor charge delegated work against the parent run budget.
	Usage *ToolUsage
}

// ToolUsage carries token counts a dispatcher attributes to a tool result so the
// caller can account delegated model work against run limits. It mirrors the
// provider usage shape without importing the provider package.
type ToolUsage struct {
	InputTokens  int
	OutputTokens int
	Estimated    bool
}

// Total returns input plus output tokens.
func (u *ToolUsage) Total() int {
	if u == nil {
		return 0
	}
	return u.InputTokens + u.OutputTokens
}

// ToolError is a handler-reported failure surfaced to the model as tool_result content.
type ToolError struct {
	Code    string
	Message string
}

func (e *ToolError) Error() string {
	if e == nil {
		return ""
	}
	if e.Code == "" {
		return e.Message
	}
	if e.Message == "" {
		return e.Code
	}
	return e.Code + ": " + e.Message
}
