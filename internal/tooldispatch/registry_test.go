package tooldispatch_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
	"github.com/phrony-platform/runtime/internal/tooldispatch"
)

func TestWorkerRegistry_queueUntilWorkerRegisters(t *testing.T) {
	reg := tooldispatch.NewWorkerRegistry(tooldispatch.DefaultRegistryConfig())

	invokeCh := make(chan string, 1)
	done := make(chan error, 1)
	go func() {
		_, err := reg.Dispatch(context.Background(), tooldispatch.ToolCall{
			CallID:   "c1",
			Tool:     "t",
			Version:  "v1",
			Deadline: time.Now().Add(time.Minute),
		})
		done <- err
	}()
	time.Sleep(20 * time.Millisecond)

	if reg.QueuedCount("t", "v1") != 1 {
		t.Fatalf("queued = %d, want 1", reg.QueuedCount("t", "v1"))
	}
	if reg.WorkerCount("t", "v1") != 0 {
		t.Fatalf("worker count = %d, want 0", reg.WorkerCount("t", "v1"))
	}

	_, err := reg.RegisterWorker("w1", "", "", []tooldispatch.HandlerAdvertisement{
		{Tool: "t", Version: "v1", MaxConcurrency: 1},
	}, nil, func(msg any) error {
		m := msg.(*runtimev1.WorkServerMsg)
		if inv := m.GetInvoke(); inv != nil {
			invokeCh <- inv.GetCallId()
		}
		return nil
	})
	if err != nil {
		t.Fatalf("RegisterWorker: %v", err)
	}

	select {
	case id := <-invokeCh:
		if id != "c1" {
			t.Fatalf("call_id = %q", id)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for invoke")
	}

	if err := reg.CompleteResult("w1", tooldispatch.ToolResult{
		CallID:  "c1",
		Payload: json.RawMessage(`{}`),
	}); err != nil {
		t.Fatalf("CompleteResult: %v", err)
	}

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Dispatch: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for dispatch")
	}
}

func TestWorkerRegistry_queueTimeoutNoHandler(t *testing.T) {
	reg := tooldispatch.NewWorkerRegistry(tooldispatch.DefaultRegistryConfig())

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()

	_, err := reg.Dispatch(ctx, tooldispatch.ToolCall{
		CallID:   "c1",
		Tool:     "t",
		Version:  "v1",
		Deadline: time.Now().Add(time.Minute),
	})
	if !tooldispatch.IsNoHandler(err) {
		t.Fatalf("err = %v, want ErrNoHandler", err)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want deadline exceeded", err)
	}
}

func TestWorkerRegistry_queueTimeoutCapacityExhausted(t *testing.T) {
	reg := tooldispatch.NewWorkerRegistry(tooldispatch.DefaultRegistryConfig())
	_, err := reg.RegisterWorker("w1", "", "", []tooldispatch.HandlerAdvertisement{
		{Tool: "t", Version: "v1", MaxConcurrency: 1},
	}, nil, func(msg any) error { return nil })
	if err != nil {
		t.Fatalf("RegisterWorker: %v", err)
	}

	go func() {
		_, _ = reg.Dispatch(context.Background(), tooldispatch.ToolCall{
			CallID:   "busy",
			Tool:     "t",
			Version:  "v1",
			Deadline: time.Now().Add(time.Minute),
		})
	}()
	time.Sleep(20 * time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()

	_, err = reg.Dispatch(ctx, tooldispatch.ToolCall{
		CallID:   "queued",
		Tool:     "t",
		Version:  "v1",
		Deadline: time.Now().Add(time.Minute),
	})
	if !tooldispatch.IsCapacityExhausted(err) {
		t.Fatalf("err = %v, want ErrCapacityExhausted", err)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want deadline exceeded", err)
	}
}

func TestWorkerRegistry_integrityReject(t *testing.T) {
	reg := tooldispatch.NewWorkerRegistry(tooldispatch.RegistryConfig{
		IntegrityCheck: func(call tooldispatch.ToolCall, w *tooldispatch.WorkerInfo) error {
			return &tooldispatch.IntegrityError{
				Violation: tooldispatch.IntegrityImageDigest,
				Tool:      call.Tool,
				Version:   call.Version,
			}
		},
	})

	var sent []*runtimev1.WorkServerMsg
	_, err := reg.RegisterWorker("w1", "id", "digest", []tooldispatch.HandlerAdvertisement{
		{Tool: "t", Version: "v1", MaxConcurrency: 1},
	}, nil, func(msg any) error {
		m := msg.(*runtimev1.WorkServerMsg)
		sent = append(sent, m)
		return nil
	})
	if err != nil {
		t.Fatalf("RegisterWorker: %v", err)
	}

	_, err = reg.Dispatch(context.Background(), tooldispatch.ToolCall{
		CallID:   "c1",
		Tool:     "t",
		Version:  "v1",
		Deadline: time.Now().Add(time.Minute),
	})
	if !tooldispatch.IsIntegrityError(err) {
		t.Fatalf("err = %v", err)
	}
	if len(sent) != 0 {
		t.Fatalf("expected no invoke, got %d messages", len(sent))
	}
}

func TestWorkerRegistry_cancelledCtxBeforeDispatch_noInvoke(t *testing.T) {
	reg := tooldispatch.NewWorkerRegistry(tooldispatch.DefaultRegistryConfig())

	var sent []*runtimev1.WorkServerMsg
	_, err := reg.RegisterWorker("w1", "", "", []tooldispatch.HandlerAdvertisement{
		{Tool: "t", Version: "v1", MaxConcurrency: 1},
	}, nil, func(msg any) error {
		m := msg.(*runtimev1.WorkServerMsg)
		sent = append(sent, m)
		return nil
	})
	if err != nil {
		t.Fatalf("RegisterWorker: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = reg.Dispatch(ctx, tooldispatch.ToolCall{
		CallID:    "c1",
		SessionID: "sess-1",
		Tool:      "t",
		Version:   "v1",
		Deadline:  time.Now().Add(time.Minute),
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if len(sent) != 0 {
		t.Fatalf("expected no WorkInvoke, got %d messages", len(sent))
	}
}

func TestWorkerRegistry_cancelledQueuedCtxOnDrain_noInvoke(t *testing.T) {
	var queuedCancel context.CancelFunc
	reg := tooldispatch.NewWorkerRegistry(tooldispatch.RegistryConfig{
		IntegrityCheck: func(call tooldispatch.ToolCall, _ *tooldispatch.WorkerInfo) error {
			// Cancel while leasing the drained waiter so the pre-send ctx.Err()
			// guard rejects WorkInvoke without racing cancelCall.
			if call.CallID == "queued" && queuedCancel != nil {
				queuedCancel()
			}
			return nil
		},
	})

	invokeCh := make(chan string, 2)
	_, err := reg.RegisterWorker("w1", "", "", []tooldispatch.HandlerAdvertisement{
		{Tool: "t", Version: "v1", MaxConcurrency: 1},
	}, nil, func(msg any) error {
		m := msg.(*runtimev1.WorkServerMsg)
		if inv := m.GetInvoke(); inv != nil {
			invokeCh <- inv.GetCallId()
		}
		return nil
	})
	if err != nil {
		t.Fatalf("RegisterWorker: %v", err)
	}

	busyDone := make(chan error, 1)
	go func() {
		_, err := reg.Dispatch(context.Background(), tooldispatch.ToolCall{
			CallID:   "busy",
			Tool:     "t",
			Version:  "v1",
			Deadline: time.Now().Add(time.Minute),
		})
		busyDone <- err
	}()
	select {
	case id := <-invokeCh:
		if id != "busy" {
			t.Fatalf("first invoke = %q, want busy", id)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for busy invoke")
	}

	ctx, cancel := context.WithCancel(context.Background())
	queuedCancel = cancel
	queuedDone := make(chan error, 1)
	go func() {
		_, err := reg.Dispatch(ctx, tooldispatch.ToolCall{
			CallID:   "queued",
			Tool:     "t",
			Version:  "v1",
			Deadline: time.Now().Add(time.Minute),
		})
		queuedDone <- err
	}()
	deadline := time.Now().Add(time.Second)
	for reg.QueuedCount("t", "v1") != 1 {
		if time.Now().After(deadline) {
			t.Fatalf("queued = %d, want 1", reg.QueuedCount("t", "v1"))
		}
		time.Sleep(5 * time.Millisecond)
	}

	if err := reg.CompleteResult("w1", tooldispatch.ToolResult{
		CallID:  "busy",
		Payload: json.RawMessage(`{}`),
	}); err != nil {
		t.Fatalf("CompleteResult busy: %v", err)
	}

	select {
	case err := <-busyDone:
		if err != nil {
			t.Fatalf("busy dispatch: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for busy dispatch")
	}
	select {
	case err := <-queuedDone:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("queued dispatch err = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for queued dispatch cancel")
	}

	select {
	case id := <-invokeCh:
		t.Fatalf("unexpected WorkInvoke for %q after cancelled queued drain", id)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestWorkerRegistry_cancelSession(t *testing.T) {
	reg := tooldispatch.NewWorkerRegistry(tooldispatch.DefaultRegistryConfig())

	invokeCh := make(chan string, 1)
	_, err := reg.RegisterWorker("w1", "", "", []tooldispatch.HandlerAdvertisement{
		{Tool: "t", Version: "v1", MaxConcurrency: 1},
	}, nil, func(msg any) error {
		m := msg.(*runtimev1.WorkServerMsg)
		if inv := m.GetInvoke(); inv != nil {
			invokeCh <- inv.GetCallId()
		}
		return nil
	})
	if err != nil {
		t.Fatalf("RegisterWorker: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := reg.Dispatch(ctx, tooldispatch.ToolCall{
			CallID:    "call-1",
			SessionID: "sess-1",
			Tool:      "t",
			Version:   "v1",
			Deadline:  time.Now().Add(time.Minute),
		})
		done <- err
	}()

	select {
	case id := <-invokeCh:
		if id != "call-1" {
			t.Fatalf("call_id = %q", id)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for invoke")
	}

	reg.CancelSession("sess-1")
	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for dispatch")
	}
}

func TestWorkerRegistry_queueFull(t *testing.T) {
	reg := tooldispatch.NewWorkerRegistry(tooldispatch.RegistryConfig{
		MaxQueuePerTool: 1,
	})
	_, err := reg.RegisterWorker("w1", "", "", []tooldispatch.HandlerAdvertisement{
		{Tool: "t", Version: "v1", MaxConcurrency: 1},
	}, nil, func(msg any) error { return nil })
	if err != nil {
		t.Fatalf("RegisterWorker: %v", err)
	}

	go func() {
		_, _ = reg.Dispatch(context.Background(), tooldispatch.ToolCall{
			CallID:   "busy",
			Tool:     "t",
			Version:  "v1",
			Deadline: time.Now().Add(time.Minute),
		})
	}()
	time.Sleep(20 * time.Millisecond)

	queued := make(chan error, 1)
	go func() {
		_, err := reg.Dispatch(context.Background(), tooldispatch.ToolCall{
			CallID:   "q1",
			Tool:     "t",
			Version:  "v1",
			Deadline: time.Now().Add(time.Minute),
		})
		queued <- err
	}()
	time.Sleep(20 * time.Millisecond)

	_, err = reg.Dispatch(context.Background(), tooldispatch.ToolCall{
		CallID:   "q2",
		Tool:     "t",
		Version:  "v1",
		Deadline: time.Now().Add(time.Minute),
	})
	if !tooldispatch.IsQueueFull(err) {
		t.Fatalf("err = %v, want ErrQueueFull", err)
	}

	_ = reg.CompleteResult("w1", tooldispatch.ToolResult{CallID: "busy", Payload: json.RawMessage(`{}`)})
	_ = reg.CompleteResult("w1", tooldispatch.ToolResult{CallID: "q1", Payload: json.RawMessage(`{}`)})

	select {
	case err := <-queued:
		if err != nil {
			t.Fatalf("queued dispatch: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out on queued dispatch")
	}
}

func TestWorkerRegistry_handlerError(t *testing.T) {
	reg := tooldispatch.NewWorkerRegistry(tooldispatch.DefaultRegistryConfig())
	_, err := reg.RegisterWorker("w1", "", "", []tooldispatch.HandlerAdvertisement{
		{Tool: "t", Version: "v1", MaxConcurrency: 2},
	}, nil, func(msg any) error { return nil })
	if err != nil {
		t.Fatalf("RegisterWorker: %v", err)
	}

	go func() {
		time.Sleep(10 * time.Millisecond)
		_ = reg.CompleteResult("w1", tooldispatch.ToolResult{
			CallID: "c1",
			Err:    &tooldispatch.ToolError{Code: "bad", Message: "nope"},
		})
	}()

	res, err := reg.Dispatch(context.Background(), tooldispatch.ToolCall{
		CallID:   "c1",
		Tool:     "t",
		Version:  "v1",
		Args:     json.RawMessage(`{}`),
		Deadline: time.Now().Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if res.Err == nil || res.Err.Code != "bad" {
		t.Fatalf("res.Err = %v", res.Err)
	}
}

func TestWorkerRegistry_shutdown(t *testing.T) {
	reg := tooldispatch.NewWorkerRegistry(tooldispatch.RegistryConfig{MaxQueuePerTool: 2})

	invokeCh := make(chan string, 1)
	_, err := reg.RegisterWorker("w1", "", "", []tooldispatch.HandlerAdvertisement{
		{Tool: "t", Version: "v1", MaxConcurrency: 1},
	}, nil, func(msg any) error {
		m := msg.(*runtimev1.WorkServerMsg)
		if inv := m.GetInvoke(); inv != nil {
			invokeCh <- inv.GetCallId()
		}
		return nil
	})
	if err != nil {
		t.Fatalf("RegisterWorker: %v", err)
	}

	queuedDone := make(chan error, 1)
	go func() {
		_, err := reg.Dispatch(context.Background(), tooldispatch.ToolCall{
			CallID:   "in-flight",
			Tool:     "t",
			Version:  "v1",
			Deadline: time.Now().Add(time.Minute),
		})
		queuedDone <- err
	}()
	select {
	case <-invokeCh:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for in-flight invoke")
	}

	go func() {
		_, err := reg.Dispatch(context.Background(), tooldispatch.ToolCall{
			CallID:   "queued",
			Tool:     "t",
			Version:  "v1",
			Deadline: time.Now().Add(time.Minute),
		})
		queuedDone <- err
	}()
	time.Sleep(20 * time.Millisecond)
	if reg.QueuedCount("t", "v1") != 1 {
		t.Fatalf("queued = %d, want 1", reg.QueuedCount("t", "v1"))
	}

	reg.Shutdown()

	if reg.WorkerCount("t", "v1") != 0 {
		t.Fatalf("worker count = %d, want 0", reg.WorkerCount("t", "v1"))
	}
	_, err = reg.RegisterWorker("w2", "", "", []tooldispatch.HandlerAdvertisement{
		{Tool: "t", Version: "v1", MaxConcurrency: 1},
	}, nil, func(msg any) error { return nil })
	if err == nil {
		t.Fatal("expected RegisterWorker to fail after shutdown")
	}

	_, err = reg.Dispatch(context.Background(), tooldispatch.ToolCall{
		CallID:   "after",
		Tool:     "t",
		Version:  "v1",
		Deadline: time.Now().Add(time.Minute),
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("dispatch after shutdown err = %v", err)
	}

	for i := 0; i < 2; i++ {
		select {
		case err := <-queuedDone:
			if !errors.Is(err, context.Canceled) && !tooldispatch.IsIndeterminate(err) {
				t.Fatalf("waiter %d err = %v", i, err)
			}
		case <-time.After(time.Second):
			t.Fatalf("timed out waiting for waiter %d", i)
		}
	}
}
