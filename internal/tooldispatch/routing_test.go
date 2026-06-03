package tooldispatch

import (
	"context"
	"encoding/json"
	"testing"
)

// routableFake is a Routable test double: it records dispatched calls and
// reports Handles for a fixed set of tool refs. Close marks it closed.
type routableFake struct {
	handles    map[string]bool
	dispatched []string
	closed     bool
}

func (r *routableFake) Dispatch(_ context.Context, call ToolCall) (ToolResult, error) {
	r.dispatched = append(r.dispatched, call.Tool)
	return ToolResult{CallID: call.CallID, Payload: json.RawMessage(`{"from":"primary"}`)}, nil
}

func (r *routableFake) Handles(tool string) bool { return r.handles[tool] }

func (r *routableFake) Close() error {
	r.closed = true
	return nil
}

func TestRoutingDispatcher_routesToPrimaryWhenHandled(t *testing.T) {
	primary := &routableFake{handles: map[string]bool{"mcp.search": true}}
	var fallbackCalls []string
	fallback := &FakeDispatcher{DispatchFn: func(_ context.Context, call ToolCall) (ToolResult, error) {
		fallbackCalls = append(fallbackCalls, call.Tool)
		return ToolResult{CallID: call.CallID}, nil
	}}
	r := &RoutingDispatcher{Primary: primary, Fallback: fallback}

	res, err := r.Dispatch(context.Background(), ToolCall{CallID: "c1", Tool: "mcp.search"})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if string(res.Payload) != `{"from":"primary"}` {
		t.Fatalf("payload = %s, want primary result", res.Payload)
	}
	if len(primary.dispatched) != 1 || primary.dispatched[0] != "mcp.search" {
		t.Fatalf("primary dispatched = %v", primary.dispatched)
	}
	if len(fallbackCalls) != 0 {
		t.Fatalf("fallback should not be called, got %v", fallbackCalls)
	}
}

func TestRoutingDispatcher_routesToFallbackWhenNotHandled(t *testing.T) {
	primary := &routableFake{handles: map[string]bool{"mcp.search": true}}
	var fallbackCalls []string
	fallback := &FakeDispatcher{DispatchFn: func(_ context.Context, call ToolCall) (ToolResult, error) {
		fallbackCalls = append(fallbackCalls, call.Tool)
		return ToolResult{CallID: call.CallID, Payload: json.RawMessage(`{"from":"worker"}`)}, nil
	}}
	r := &RoutingDispatcher{Primary: primary, Fallback: fallback}

	res, err := r.Dispatch(context.Background(), ToolCall{CallID: "c1", Tool: "weather.get-forecast"})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if string(res.Payload) != `{"from":"worker"}` {
		t.Fatalf("payload = %s, want worker result", res.Payload)
	}
	if len(primary.dispatched) != 0 {
		t.Fatalf("primary should not be called, got %v", primary.dispatched)
	}
	if len(fallbackCalls) != 1 || fallbackCalls[0] != "weather.get-forecast" {
		t.Fatalf("fallback dispatched = %v", fallbackCalls)
	}
}

func TestRoutingDispatcher_noFallbackReturnsNoHandler(t *testing.T) {
	r := &RoutingDispatcher{Primary: &routableFake{}}
	_, err := r.Dispatch(context.Background(), ToolCall{CallID: "c1", Tool: "weather.get-forecast"})
	if !IsNoHandler(err) {
		t.Fatalf("err = %v, want ErrNoHandler", err)
	}
}

func TestRoutingDispatcher_closeClosesPrimaryNotFallback(t *testing.T) {
	primary := &routableFake{}
	fallback := &FakeDispatcher{}
	r := &RoutingDispatcher{Primary: primary, Fallback: fallback}
	if err := r.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if !primary.closed {
		t.Fatal("primary should be closed")
	}
}

func TestRoutingDispatcher_closeNoopWhenPrimaryNotCloser(t *testing.T) {
	// FakeDispatcher is not a Routable/Closer; ensure Close is a safe no-op.
	r := &RoutingDispatcher{}
	if err := r.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}
