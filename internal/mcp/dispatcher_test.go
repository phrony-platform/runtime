package mcp_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"

	"github.com/phrony-platform/runtime/internal/mcp"
	"github.com/phrony-platform/runtime/internal/tooldispatch"
)

type recorderEvent struct {
	kind   string
	callID string
	err    error
	result tooldispatch.ToolResult
}

type fakeRecorder struct {
	mu        sync.Mutex
	events    []recorderEvent
	completed map[string]tooldispatch.ToolResult
}

func newFakeRecorder() *fakeRecorder {
	return &fakeRecorder{completed: make(map[string]tooldispatch.ToolResult)}
}

func (f *fakeRecorder) RecordPending(_ context.Context, call tooldispatch.ToolCall, _ string) error {
	f.add(recorderEvent{kind: "pending", callID: call.CallID})
	return nil
}

func (f *fakeRecorder) RecordQueued(_ context.Context, call tooldispatch.ToolCall) error {
	f.add(recorderEvent{kind: "queued", callID: call.CallID})
	return nil
}

func (f *fakeRecorder) RecordDispatched(_ context.Context, prov tooldispatch.DispatchProvenance) error {
	f.add(recorderEvent{kind: "dispatched", callID: prov.Call.CallID})
	return nil
}

func (f *fakeRecorder) RecordCompleted(_ context.Context, call tooldispatch.ToolCall, res tooldispatch.ToolResult, dispatchErr error) error {
	f.add(recorderEvent{kind: "completed", callID: call.CallID, result: res, err: dispatchErr})
	return nil
}

func (f *fakeRecorder) RecordIndeterminate(_ context.Context, call tooldispatch.ToolCall, _ string) error {
	f.add(recorderEvent{kind: "indeterminate", callID: call.CallID})
	return nil
}

func (f *fakeRecorder) LookupCompleted(_ context.Context, callID string) (tooldispatch.ToolResult, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	res, ok := f.completed[callID]
	return res, ok, nil
}

func (f *fakeRecorder) add(ev recorderEvent) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, ev)
}

func (f *fakeRecorder) kinds() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.events))
	for i, ev := range f.events {
		out[i] = ev.kind
	}
	return out
}

func newDispatcher(t *testing.T, url string, rec tooldispatch.InvocationRecorder) *mcp.Dispatcher {
	t.Helper()
	client := mcp.NewClient(mcp.ServerConfig{Name: "fake", URL: url})
	t.Cleanup(func() { _ = client.Close() })
	d := mcp.NewDispatcher(
		map[string]*mcp.Client{"fake": client},
		map[string]mcp.Binding{
			"demo.echo": {Server: "fake", RemoteTool: "echo"},
			"demo.boom": {Server: "fake", RemoteTool: "boom"},
		},
	)
	if rec != nil {
		d.SetInvocationRecorder(rec)
	}
	return d
}

func TestDispatcherSuccessRecordsLedger(t *testing.T) {
	srv := newFakeMCPServer(t, nil)
	rec := newFakeRecorder()
	d := newDispatcher(t, srv.URL, rec)

	res, err := d.Dispatch(context.Background(), tooldispatch.ToolCall{
		CallID: "call-1",
		Tool:   "demo.echo",
		Args:   json.RawMessage(`{"value":"hi"}`),
	})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if res.Err != nil {
		t.Fatalf("unexpected tool error: %v", res.Err)
	}
	if string(res.Payload) != `{"echo":"hi"}` {
		t.Fatalf("payload = %s, want {\"echo\":\"hi\"}", res.Payload)
	}

	want := []string{"pending", "dispatched", "completed"}
	got := rec.kinds()
	if len(got) != len(want) {
		t.Fatalf("ledger events = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("ledger events = %v, want %v", got, want)
		}
	}
}

func TestDispatcherToolErrorMapsToToolResult(t *testing.T) {
	srv := newFakeMCPServer(t, nil)
	rec := newFakeRecorder()
	d := newDispatcher(t, srv.URL, rec)

	res, err := d.Dispatch(context.Background(), tooldispatch.ToolCall{
		CallID: "call-boom",
		Tool:   "demo.boom",
		Args:   json.RawMessage(`{}`),
	})
	if err != nil {
		t.Fatalf("Dispatch returned dispatch error: %v", err)
	}
	if res.Err == nil {
		t.Fatal("expected tool error in result")
	}
	if res.Err.Message != "kaboom" {
		t.Fatalf("error message = %q, want %q", res.Err.Message, "kaboom")
	}

	// Tool errors are a completed (failed) outcome, not indeterminate.
	for _, k := range rec.kinds() {
		if k == "indeterminate" {
			t.Fatal("tool error should not be recorded as indeterminate")
		}
	}
}

func TestDispatcherTransportErrorIsIndeterminate(t *testing.T) {
	srv := newFakeMCPServer(t, nil)
	srv.Close() // force a connection failure
	rec := newFakeRecorder()
	d := newDispatcher(t, srv.URL, rec)

	_, err := d.Dispatch(context.Background(), tooldispatch.ToolCall{
		CallID: "call-down",
		Tool:   "demo.echo",
		Args:   json.RawMessage(`{"value":"hi"}`),
	})
	if !errors.Is(err, tooldispatch.ErrIndeterminate) {
		t.Fatalf("error = %v, want ErrIndeterminate", err)
	}

	var sawIndeterminate bool
	for _, k := range rec.kinds() {
		if k == "indeterminate" {
			sawIndeterminate = true
		}
	}
	if !sawIndeterminate {
		t.Fatal("expected an indeterminate ledger event")
	}
}

func TestDispatcherUnknownToolIsNoHandler(t *testing.T) {
	srv := newFakeMCPServer(t, nil)
	d := newDispatcher(t, srv.URL, nil)

	_, err := d.Dispatch(context.Background(), tooldispatch.ToolCall{
		CallID: "call-x",
		Tool:   "demo.unknown",
		Args:   json.RawMessage(`{}`),
	})
	if !errors.Is(err, tooldispatch.ErrNoHandler) {
		t.Fatalf("error = %v, want ErrNoHandler", err)
	}
}

func TestDispatcherIdempotentReplay(t *testing.T) {
	srv := newFakeMCPServer(t, nil)
	rec := newFakeRecorder()
	stored := tooldispatch.ToolResult{CallID: "call-1", Payload: json.RawMessage(`{"echo":"cached"}`)}
	rec.completed["call-1"] = stored
	d := newDispatcher(t, srv.URL, rec)

	res, err := d.Dispatch(context.Background(), tooldispatch.ToolCall{
		CallID: "call-1",
		Tool:   "demo.echo",
		Args:   json.RawMessage(`{"value":"hi"}`),
	})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if string(res.Payload) != `{"echo":"cached"}` {
		t.Fatalf("payload = %s, want cached result", res.Payload)
	}
	// A replayed call must not re-record or re-dispatch.
	if len(rec.kinds()) != 0 {
		t.Fatalf("expected no ledger events on replay, got %v", rec.kinds())
	}
}

func TestDispatcherRequiresCallID(t *testing.T) {
	srv := newFakeMCPServer(t, nil)
	d := newDispatcher(t, srv.URL, nil)

	if _, err := d.Dispatch(context.Background(), tooldispatch.ToolCall{Tool: "demo.echo"}); err == nil {
		t.Fatal("expected error when call_id is empty")
	}
}
