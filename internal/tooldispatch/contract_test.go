package tooldispatch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestDeriveCallID_stable(t *testing.T) {
	got := DeriveCallID("sess-1", "av-1", 2, 3)
	want := "sess-1:av-1:2:3"
	if got != want {
		t.Fatalf("DeriveCallID() = %q, want %q", got, want)
	}
	if DeriveCallID("sess-1", "av-1", 2, 3) != got {
		t.Fatal("DeriveCallID should be deterministic")
	}
}

func TestToolCall_ToolKey(t *testing.T) {
	call := ToolCall{Tool: "search", Version: "v2"}
	if got, want := call.ToolKey(), "search@v2"; got != want {
		t.Fatalf("ToolKey() = %q, want %q", got, want)
	}
	if got := ToolKey("only", ""); got != "only" {
		t.Fatalf("ToolKey(tool, \"\") = %q, want tool name only", got)
	}
}

func TestToolError_Error(t *testing.T) {
	if (&ToolError{}).Error() != "" {
		t.Fatal("empty ToolError should format to empty string")
	}
	err := &ToolError{Code: "not_found", Message: "item missing"}
	if err.Error() != "not_found: item missing" {
		t.Fatalf("Error() = %q", err.Error())
	}
}

func TestFakeDispatcher_success(t *testing.T) {
	var dispatched ToolCall
	d := &FakeDispatcher{
		DispatchFn: func(ctx context.Context, call ToolCall) (ToolResult, error) {
			dispatched = call
			return ToolResult{
				CallID:  call.CallID,
				Payload: json.RawMessage(`{"ok":true}`),
			}, nil
		},
	}

	call := ToolCall{
		CallID:         DeriveCallID("s", "av", 0, 0),
		SessionID:      "s",
		AgentVersionID: "av",
		Tool:           "search",
		Version:        "v1",
		Args:           json.RawMessage(`{"q":"hi"}`),
		Deadline:       time.Now().Add(time.Minute),
	}
	res, err := d.Dispatch(context.Background(), call)
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if dispatched.CallID != call.CallID {
		t.Fatalf("dispatched CallID = %q, want %q", dispatched.CallID, call.CallID)
	}
	if string(res.Payload) != `{"ok":true}` {
		t.Fatalf("Payload = %s", res.Payload)
	}
	if res.Err != nil {
		t.Fatalf("Err = %v, want nil", res.Err)
	}
}

func TestFakeDispatcher_handlerError(t *testing.T) {
	d := &FakeDispatcher{
		DispatchFn: func(ctx context.Context, call ToolCall) (ToolResult, error) {
			return ToolResult{
				CallID: call.CallID,
				Err:    &ToolError{Code: "invalid_args", Message: "bad input"},
			}, nil
		},
	}
	res, err := d.Dispatch(context.Background(), ToolCall{CallID: "c1"})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if res.Err == nil || res.Err.Code != "invalid_args" {
		t.Fatalf("res.Err = %v", res.Err)
	}
}

func TestFakeDispatcher_dispatchError(t *testing.T) {
	d := &FakeDispatcher{
		DispatchFn: func(ctx context.Context, call ToolCall) (ToolResult, error) {
			return ToolResult{}, ErrNoHandler
		},
	}
	_, err := d.Dispatch(context.Background(), ToolCall{CallID: "c1"})
	if !IsNoHandler(err) {
		t.Fatalf("err = %v, want ErrNoHandler", err)
	}
}

func TestFakeDispatcher_nilFn(t *testing.T) {
	var d FakeDispatcher
	res, err := d.Dispatch(context.Background(), ToolCall{CallID: "c1"})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if res.CallID != "c1" {
		t.Fatalf("CallID = %q", res.CallID)
	}
}

func TestDispatchErrors_distinct(t *testing.T) {
	cases := []struct {
		name string
		err  error
		ok   func(error) bool
	}{
		{"no handler", ErrNoHandler, IsNoHandler},
		{"capacity", ErrCapacityExhausted, IsCapacityExhausted},
		{"queue full", ErrQueueFull, IsQueueFull},
		{"lease", ErrLeaseExpired, IsLeaseExpired},
		{"indeterminate", ErrIndeterminate, IsIndeterminate},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if !tc.ok(tc.err) {
				t.Fatalf("predicate failed for %v", tc.err)
			}
			wrapped := fmt.Errorf("dispatch: %w", tc.err)
			if !tc.ok(wrapped) {
				t.Fatalf("wrapped predicate failed for %v", wrapped)
			}
		})
	}

	// Errors must not alias each other.
	if errors.Is(ErrNoHandler, ErrCapacityExhausted) {
		t.Fatal("ErrNoHandler and ErrCapacityExhausted must be distinct")
	}
}

func TestIntegrityError(t *testing.T) {
	err := &IntegrityError{
		Violation: IntegrityImageDigest,
		Tool:      "pay",
		Version:   "v1",
		Detail:    "digest sha256:abc != sha256:def",
	}
	if !IsIntegrityError(err) {
		t.Fatal("IsIntegrityError should match *IntegrityError")
	}
	wrapped := fmt.Errorf("route: %w", err)
	if !IsIntegrityError(wrapped) {
		t.Fatal("IsIntegrityError should match wrapped *IntegrityError")
	}
	if !errors.Is(wrapped, &IntegrityError{}) {
		t.Fatal("errors.Is should match IntegrityError type")
	}
	if IsNoHandler(err) {
		t.Fatal("integrity error must not match ErrNoHandler")
	}
}
