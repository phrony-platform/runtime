package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
	"github.com/phrony-platform/runtime/e2e/internal/workclient"
)

type toolHandler func(ctx context.Context, args json.RawMessage) (json.RawMessage, *workclient.ToolError)

// DefaultAdvertisements match agent.yaml tool bindings.
func DefaultAdvertisements() []workclient.HandlerAdvertisement {
	return []workclient.HandlerAdvertisement{
		{Tool: "payments.process-payment", Version: "1.0.0", MaxConcurrency: 4},
	}
}

// Dispatch routes a WorkInvoke to the playground handler for its tool@version.
func Dispatch(ctx context.Context, inv *runtimev1.WorkInvoke) (json.RawMessage, *workclient.ToolError) {
	key := toolKey(inv.GetTool(), inv.GetVersion())
	fn, ok := byKey[key]
	if !ok {
		return nil, &workclient.ToolError{
			Code:    "unknown_tool",
			Message: fmt.Sprintf("no handler for %s", key),
		}
	}
	return fn(ctx, inv.GetArgs())
}

// NoDispatchDispatch is used by e2e (PLAYGROUND_WORKER_MODE=nodispatch): the worker is
// connected but declines tool calls without printing a payment receipt.
func NoDispatchDispatch(ctx context.Context, inv *runtimev1.WorkInvoke) (json.RawMessage, *workclient.ToolError) {
	key := toolKey(inv.GetTool(), inv.GetVersion())
	if _, ok := byKey[key]; !ok {
		return nil, &workclient.ToolError{
			Code:    "unknown_tool",
			Message: fmt.Sprintf("no handler for %s", key),
		}
	}
	return nil, &workclient.ToolError{
		Code:    "unavailable",
		Message: "handler declined dispatch (playground nodispatch mode)",
	}
}

// IndeterminateDispatch is used by e2e (PLAYGROUND_WORKER_MODE=indeterminate): accept the
// invoke, then exit without sending a WorkToolResult so the runtime records indeterminate.
func IndeterminateDispatch(ctx context.Context, inv *runtimev1.WorkInvoke) (json.RawMessage, *workclient.ToolError) {
	key := toolKey(inv.GetTool(), inv.GetVersion())
	if _, ok := byKey[key]; !ok {
		return nil, &workclient.ToolError{
			Code:    "unknown_tool",
			Message: fmt.Sprintf("no handler for %s", key),
		}
	}
	go func() {
		time.Sleep(100 * time.Millisecond)
		os.Exit(0)
	}()
	<-ctx.Done()
	return nil, nil
}

var byKey = map[string]toolHandler{
	toolKey("payments.process-payment", "1.0.0"): processPayment,
}

func toolKey(tool, version string) string {
	v := strings.TrimSpace(version)
	if v == "" {
		v = "default"
	}
	return strings.TrimSpace(tool) + "@" + v
}
