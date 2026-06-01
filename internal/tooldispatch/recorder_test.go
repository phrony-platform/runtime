package tooldispatch_test

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"github.com/phrony-platform/runtime/internal/tooldispatch"
)

type fakeRecorder struct {
	mu          sync.Mutex
	dispatched  []tooldispatch.DispatchProvenance
	completed   int
	pending     int
}

func (f *fakeRecorder) RecordPending(_ context.Context, _ tooldispatch.ToolCall, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pending++
	return nil
}

func (f *fakeRecorder) RecordQueued(_ context.Context, _ tooldispatch.ToolCall) error {
	return nil
}

func (f *fakeRecorder) RecordDispatched(_ context.Context, prov tooldispatch.DispatchProvenance) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.dispatched = append(f.dispatched, prov)
	return nil
}

func (f *fakeRecorder) RecordCompleted(_ context.Context, _ tooldispatch.ToolCall, _ tooldispatch.ToolResult, _ error) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.completed++
	return nil
}

func (f *fakeRecorder) RecordIndeterminate(_ context.Context, _ tooldispatch.ToolCall, _ string) error {
	return nil
}

func (f *fakeRecorder) LookupCompleted(_ context.Context, _ string) (tooldispatch.ToolResult, bool, error) {
	return tooldispatch.ToolResult{}, false, nil
}

func TestWorkerRegistry_recordsProvenance(t *testing.T) {
	rec := &fakeRecorder{}
	reg := tooldispatch.NewWorkerRegistry(tooldispatch.DefaultRegistryConfig())
	reg.SetInvocationRecorder(rec)

	invokeCh := make(chan struct{}, 1)
	_, err := reg.RegisterWorker("w1", "spiffe://w", "sha256:img", []tooldispatch.HandlerAdvertisement{{
		Tool: "echo", Version: "default", DescriptorHash: "desc-1",
	}}, nil, func(msg any) error {
		select {
		case invokeCh <- struct{}{}:
		default:
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	go func() {
		<-invokeCh
		_ = reg.CompleteResult("w1", tooldispatch.ToolResult{
			CallID:  "call-1",
			Payload: json.RawMessage(`{"ok":true}`),
		})
	}()

	res, err := reg.Dispatch(context.Background(), tooldispatch.ToolCall{
		CallID:  "call-1",
		Tool:    "echo",
		Version: "default",
	})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if len(res.Payload) == 0 {
		t.Fatal("expected payload")
	}

	rec.mu.Lock()
	defer rec.mu.Unlock()
	if len(rec.dispatched) != 1 {
		t.Fatalf("dispatched records = %d, want 1", len(rec.dispatched))
	}
	if rec.dispatched[0].DescriptorHash != "desc-1" {
		t.Fatalf("descriptor_hash = %q", rec.dispatched[0].DescriptorHash)
	}
	if rec.completed != 1 {
		t.Fatalf("completed = %d, want 1", rec.completed)
	}
}
