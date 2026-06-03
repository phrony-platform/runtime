package tooldispatch

import (
	"context"
	"fmt"
	"io"
)

// Routable is a Dispatcher that reports which logical tools it backs, letting a
// RoutingDispatcher pick a backend per call without inspecting the call itself.
type Routable interface {
	Dispatcher
	// Handles reports whether this dispatcher backs the given logical tool ref
	// (ToolCall.Tool), i.e. the value the executor derives from the binding.
	Handles(tool string) bool
}

// RoutingDispatcher sends a tool call to Primary when Primary.Handles reports
// the tool, otherwise to Fallback. It lets MCP-backed tools and worker tools
// share a single dispatch entrypoint: policy evaluation, HITL approvals, the
// invocation ledger, and recovery all run upstream on the logical ref and stay
// backend-agnostic.
type RoutingDispatcher struct {
	Primary  Routable
	Fallback Dispatcher
}

func (r *RoutingDispatcher) Dispatch(ctx context.Context, call ToolCall) (ToolResult, error) {
	if r.Primary != nil && r.Primary.Handles(call.Tool) {
		return r.Primary.Dispatch(ctx, call)
	}
	if r.Fallback == nil {
		return ToolResult{}, fmt.Errorf("%w: %q", ErrNoHandler, call.Tool)
	}
	return r.Fallback.Dispatch(ctx, call)
}

// Close releases the primary backend's resources (e.g. MCP sessions). The
// fallback worker dispatcher is shared across sessions and is left untouched.
func (r *RoutingDispatcher) Close() error {
	if closer, ok := r.Primary.(io.Closer); ok {
		return closer.Close()
	}
	return nil
}

var _ Dispatcher = (*RoutingDispatcher)(nil)
