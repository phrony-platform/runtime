package tooldispatch

import (
	"context"
	"errors"
)

// StreamDispatcher implements Dispatcher using a WorkerRegistry.
type StreamDispatcher struct {
	Registry *WorkerRegistry
}

func (d *StreamDispatcher) Dispatch(ctx context.Context, call ToolCall) (ToolResult, error) {
	if d == nil || d.Registry == nil {
		return ToolResult{}, errors.New("tool dispatch registry is not configured")
	}
	return d.Registry.Dispatch(ctx, call)
}
