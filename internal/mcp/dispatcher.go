package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/phrony-platform/runtime/internal/tooldispatch"
)

// Error codes surfaced to the model as tool_result content for MCP failures.
const (
	errCodeToolError    = "mcp_tool_error"
	errCodeResultEncode = "mcp_result_encode_error"
)

// Binding maps a Phrony logical tool name to a remote MCP server and tool.
// RemoteTool defaults to the logical tool name when empty.
type Binding struct {
	Server     string
	RemoteTool string
}

// Dispatcher implements tooldispatch.Dispatcher for MCP-backed tools. It routes
// each call to the configured server's Client, records the same pending /
// dispatched / completed ledger rows as worker dispatch, and maps MCP outcomes
// to tooldispatch results: tool-level errors become ToolResult.Err, while
// transport or timeout failures become tooldispatch.ErrIndeterminate so the
// existing side-effect-class recovery routing applies.
type Dispatcher struct {
	clients  map[string]*Client
	bindings map[string]Binding
	recorder tooldispatch.InvocationRecorder
}

// NewDispatcher builds a Dispatcher from per-server clients (keyed by server
// name) and logical-tool bindings (keyed by ToolCall.Tool).
func NewDispatcher(clients map[string]*Client, bindings map[string]Binding) *Dispatcher {
	return &Dispatcher{clients: clients, bindings: bindings}
}

// SetInvocationRecorder attaches durable ledger persistence (optional).
func (d *Dispatcher) SetInvocationRecorder(rec tooldispatch.InvocationRecorder) {
	d.recorder = rec
}

// Handles reports whether the dispatcher has a binding for the logical tool.
// A routing dispatcher uses this to decide between MCP and worker backends.
func (d *Dispatcher) Handles(tool string) bool {
	_, ok := d.bindings[tool]
	return ok
}

func (d *Dispatcher) Dispatch(ctx context.Context, call tooldispatch.ToolCall) (tooldispatch.ToolResult, error) {
	if call.CallID == "" {
		return tooldispatch.ToolResult{}, fmt.Errorf("call_id is required")
	}
	binding, ok := d.bindings[call.Tool]
	if !ok {
		return tooldispatch.ToolResult{}, fmt.Errorf("%w: tool %q is not MCP-backed", tooldispatch.ErrNoHandler, call.Tool)
	}
	client, ok := d.clients[binding.Server]
	if !ok {
		return tooldispatch.ToolResult{}, fmt.Errorf("%w: mcp server %q is not configured", tooldispatch.ErrNoHandler, binding.Server)
	}

	rec := d.recorder
	if rec != nil {
		if stored, found, err := rec.LookupCompleted(ctx, call.CallID); err != nil {
			return tooldispatch.ToolResult{}, err
		} else if found {
			return stored, nil
		}
		if err := rec.RecordPending(ctx, call, ""); err != nil {
			return tooldispatch.ToolResult{}, fmt.Errorf("record tool invocation: %w", err)
		}
		if err := rec.RecordDispatched(ctx, tooldispatch.DispatchProvenance{Call: call}); err != nil {
			if rec.RecordCompleted(ctx, call, tooldispatch.ToolResult{}, err) != nil {
				slog.Error("rollback tool invocation", "call_id", call.CallID, "error", err)
			}
			return tooldispatch.ToolResult{}, fmt.Errorf("record tool dispatch: %w", err)
		}
	}

	remoteTool := binding.RemoteTool
	if remoteTool == "" {
		remoteTool = call.Tool
	}

	mcpRes, err := client.CallTool(ctx, remoteTool, call.Args)
	if err != nil {
		// Transport/timeout failure: the server may or may not have executed
		// the call, so the outcome is unknown. Map to ErrIndeterminate so
		// recovery routes by side-effect class rather than silently retrying.
		indErr := fmt.Errorf("%w: %v", tooldispatch.ErrIndeterminate, err)
		if rec != nil {
			_ = rec.RecordIndeterminate(ctx, call, indErr.Error())
		}
		return tooldispatch.ToolResult{}, indErr
	}

	res := mapResult(call.CallID, mcpRes)
	if rec != nil {
		if err := rec.RecordCompleted(ctx, call, res, nil); err != nil {
			return tooldispatch.ToolResult{}, fmt.Errorf("record tool result: %w", err)
		}
	}
	return res, nil
}

// Close terminates every per-server session.
func (d *Dispatcher) Close() error {
	var errs []error
	for _, c := range d.clients {
		if err := c.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// mapResult converts an MCP CallToolResult into a tooldispatch.ToolResult.
// Tool-level errors (IsError) become ToolResult.Err; otherwise the result is
// encoded as a JSON payload (structured content when present, else the content
// blocks) so it is valid JSON for the ledger and the model.
func mapResult(callID string, res *mcpsdk.CallToolResult) tooldispatch.ToolResult {
	out := tooldispatch.ToolResult{CallID: callID}
	if res == nil {
		out.Payload = json.RawMessage("{}")
		return out
	}
	if res.IsError {
		out.Err = &tooldispatch.ToolError{
			Code:    errCodeToolError,
			Message: textContent(res.Content),
		}
		return out
	}
	payload, err := resultPayload(res)
	if err != nil {
		out.Err = &tooldispatch.ToolError{Code: errCodeResultEncode, Message: err.Error()}
		return out
	}
	out.Payload = payload
	return out
}

func resultPayload(res *mcpsdk.CallToolResult) (json.RawMessage, error) {
	if res.StructuredContent != nil {
		return json.Marshal(res.StructuredContent)
	}
	if len(res.Content) == 0 {
		return json.RawMessage("{}"), nil
	}
	return json.Marshal(res.Content)
}

// textContent joins the text blocks of an MCP result, used for error messages.
func textContent(content []mcpsdk.Content) string {
	var b strings.Builder
	for _, c := range content {
		tc, ok := c.(*mcpsdk.TextContent)
		if !ok {
			continue
		}
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(tc.Text)
	}
	return b.String()
}

var _ tooldispatch.Dispatcher = (*Dispatcher)(nil)
