package tooldispatch

import "context"

// FakeDispatcher is an in-memory Dispatcher for tests. When DispatchFn is nil,
// Dispatch returns a zero ToolResult and nil error.
type FakeDispatcher struct {
	DispatchFn func(ctx context.Context, call ToolCall) (ToolResult, error)
}

func (f *FakeDispatcher) Dispatch(ctx context.Context, call ToolCall) (ToolResult, error) {
	if f == nil || f.DispatchFn == nil {
		return ToolResult{CallID: call.CallID}, nil
	}
	return f.DispatchFn(ctx, call)
}
