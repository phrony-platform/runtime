package tooldispatch_test

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
	"github.com/phrony-platform/runtime/internal/tooldispatch"
)

type ledgerRecorder struct {
	mu      sync.Mutex
	lookup  map[string]tooldispatch.ToolResult
}

func (l *ledgerRecorder) RecordPending(context.Context, tooldispatch.ToolCall, string) error {
	return nil
}
func (l *ledgerRecorder) RecordQueued(context.Context, tooldispatch.ToolCall) error { return nil }
func (l *ledgerRecorder) RecordDispatched(context.Context, tooldispatch.DispatchProvenance) error {
	return nil
}
func (l *ledgerRecorder) RecordCompleted(context.Context, tooldispatch.ToolCall, tooldispatch.ToolResult, error) error {
	return nil
}
func (l *ledgerRecorder) RecordIndeterminate(context.Context, tooldispatch.ToolCall, string) error {
	return nil
}
func (l *ledgerRecorder) LookupCompleted(_ context.Context, callID string) (tooldispatch.ToolResult, bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	res, ok := l.lookup[callID]
	return res, ok, nil
}

func TestWorkerRegistry_reconnectAcksCompletedFromLedger(t *testing.T) {
	rec := &ledgerRecorder{
		lookup: map[string]tooldispatch.ToolResult{
			"call-done": {CallID: "call-done", Payload: json.RawMessage(`{"ok":true}`)},
		},
	}
	reg := tooldispatch.NewWorkerRegistry(tooldispatch.DefaultRegistryConfig())
	reg.SetInvocationRecorder(rec)

	var acks []string
	_, err := reg.RegisterWorker("w1", "", "", []tooldispatch.HandlerAdvertisement{{
		Tool: "t", Version: "v1", MaxConcurrency: 1,
	}}, []string{"call-done"}, func(msg any) error {
		m := msg.(*runtimev1.WorkServerMsg)
		if ack := m.GetResultAck(); ack != nil {
			acks = append(acks, ack.GetCallId())
		}
		return nil
	})
	if err != nil {
		t.Fatalf("RegisterWorker: %v", err)
	}
	if len(acks) != 1 || acks[0] != "call-done" {
		t.Fatalf("acks = %v, want [call-done]", acks)
	}
}

func TestWorkerRegistry_reconnectReattachesInFlightCall(t *testing.T) {
	reg := tooldispatch.NewWorkerRegistry(tooldispatch.DefaultRegistryConfig())

	invokeCh := make(chan string, 1)
	_, err := reg.RegisterWorker("w1", "", "", []tooldispatch.HandlerAdvertisement{{
		Tool: "t", Version: "v1", MaxConcurrency: 1,
	}}, nil, func(msg any) error {
		m := msg.(*runtimev1.WorkServerMsg)
		if inv := m.GetInvoke(); inv != nil {
			select {
			case invokeCh <- inv.GetCallId():
			default:
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("RegisterWorker w1: %v", err)
	}

	done := make(chan struct{})
	var dispatchErr error
	go func() {
		_, dispatchErr = reg.Dispatch(context.Background(), tooldispatch.ToolCall{
			CallID:   "call-inflight",
			SessionID: "sess-1",
			Tool:     "t",
			Version:  "v1",
			Deadline: time.Now().Add(time.Minute),
		})
		close(done)
	}()

	select {
	case id := <-invokeCh:
		if id != "call-inflight" {
			t.Fatalf("invoke call_id = %q", id)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for invoke")
	}

	_, err = reg.RegisterWorker("w2", "", "", []tooldispatch.HandlerAdvertisement{{
		Tool: "t", Version: "v1", MaxConcurrency: 1,
	}}, []string{"call-inflight"}, func(msg any) error { return nil })
	if err != nil {
		t.Fatalf("RegisterWorker w2: %v", err)
	}

	if err := reg.CompleteResult("w1", tooldispatch.ToolResult{
		CallID:  "call-inflight",
		Payload: json.RawMessage(`{"from":"w1"}`),
	}); err == nil {
		t.Fatal("CompleteResult from w1 expected error after reattach")
	}

	if err := reg.CompleteResult("w2", tooldispatch.ToolResult{
		CallID:  "call-inflight",
		Payload: json.RawMessage(`{"from":"w2"}`),
	}); err != nil {
		t.Fatalf("CompleteResult w2: %v", err)
	}

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for dispatch")
	}
	if dispatchErr != nil {
		t.Fatalf("Dispatch: %v", dispatchErr)
	}
}
