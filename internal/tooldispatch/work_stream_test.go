package tooldispatch_test

import (
	"context"
	"encoding/json"
	"net"
	"sync"
	"testing"
	"time"

	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
	"github.com/phrony-platform/runtime/internal/tooldispatch"
	"github.com/phrony-platform/runtime/internal/tooldispatch/testworker"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

type workTestRuntime struct {
	runtimev1.UnimplementedRuntimeServer
	reg *tooldispatch.WorkerRegistry
}

func (s *workTestRuntime) Work(stream runtimev1.Runtime_WorkServer) error {
	return (&tooldispatch.WorkStream{Registry: s.reg}).ServeWork(stream)
}

func startWorkTestRuntime(t *testing.T) (*grpc.ClientConn, *tooldispatch.WorkerRegistry) {
	t.Helper()

	reg := tooldispatch.NewWorkerRegistry(tooldispatch.RegistryConfig{
		LeaseTTL:        time.Minute,
		MaxQueuePerTool: 8,
	})
	lis := bufconn.Listen(1024 * 1024)
	srv := grpc.NewServer()
	runtimev1.RegisterRuntimeServer(srv, &workTestRuntime{reg: reg})
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(func() { srv.Stop() })

	dialer := func(context.Context, string) (net.Conn, error) {
		return lis.Dial()
	}
	cc, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(dialer),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("grpc.NewClient: %v", err)
	}
	t.Cleanup(func() { _ = cc.Close() })
	return cc, reg
}

func TestWorkStream_dispatchRoundTrip(t *testing.T) {
	cc, reg := startWorkTestRuntime(t)
	disp := &tooldispatch.StreamDispatcher{Registry: reg}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = testworker.Run(ctx, cc, testworker.Options{
			WorkerID: "w1",
			Handlers: []tooldispatch.HandlerAdvertisement{
				{Tool: "search", Version: "v1", MaxConcurrency: 2},
			},
			Handler: func(_ context.Context, inv *runtimev1.WorkInvoke) (json.RawMessage, *tooldispatch.ToolError) {
				return json.RawMessage(`{"hits":[]}`), nil
			},
		})
	}()
	time.Sleep(50 * time.Millisecond)

	res, err := disp.Dispatch(context.Background(), tooldispatch.ToolCall{
		CallID:         tooldispatch.DeriveCallID("sess", "av", 0, 0),
		SessionID:      "sess",
		AgentVersionID: "av",
		Tool:           "search",
		Version:        "v1",
		Args:           json.RawMessage(`{"q":"hi"}`),
		Deadline:       time.Now().Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if string(res.Payload) != `{"hits":[]}` {
		t.Fatalf("Payload = %s", res.Payload)
	}

	cancel()
	wg.Wait()
}

func TestWorkStream_queueUntilWorkerRegisters(t *testing.T) {
	cc, reg := startWorkTestRuntime(t)
	disp := &tooldispatch.StreamDispatcher{Registry: reg}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ready := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		_, err := disp.Dispatch(context.Background(), tooldispatch.ToolCall{
			CallID:   "c1",
			Tool:     "search",
			Version:  "v1",
			Deadline: time.Now().Add(time.Minute),
		})
		done <- err
	}()
	time.Sleep(30 * time.Millisecond)

	if reg.QueuedCount("search", "v1") != 1 {
		t.Fatalf("queued = %d, want 1", reg.QueuedCount("search", "v1"))
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = testworker.Run(ctx, cc, testworker.Options{
			WorkerID: "w1",
			Handlers: []tooldispatch.HandlerAdvertisement{
				{Tool: "search", Version: "v1", MaxConcurrency: 2},
			},
			Handler: func(_ context.Context, inv *runtimev1.WorkInvoke) (json.RawMessage, *tooldispatch.ToolError) {
				close(ready)
				return json.RawMessage(`{"hits":[]}`), nil
			},
		})
	}()

	select {
	case <-ready:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for worker invoke")
	}

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Dispatch: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for dispatch")
	}

	cancel()
	wg.Wait()
}

func TestWorkStream_capacityQueue(t *testing.T) {
	cc, reg := startWorkTestRuntime(t)
	disp := &tooldispatch.StreamDispatcher{Registry: reg}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	block := make(chan struct{})
	ready := make(chan struct{})
	var once sync.Once

	go func() {
		_ = testworker.Run(ctx, cc, testworker.Options{
			WorkerID: "w1",
			Handlers: []tooldispatch.HandlerAdvertisement{
				{Tool: "slow", Version: "v1", MaxConcurrency: 1},
			},
			Handler: func(_ context.Context, inv *runtimev1.WorkInvoke) (json.RawMessage, *tooldispatch.ToolError) {
				once.Do(func() { close(ready) })
				<-block
				return json.RawMessage(`{"done":true}`), nil
			},
		})
	}()

	time.Sleep(50 * time.Millisecond)

	firstDone := make(chan struct{})
	go func() {
		_, _ = disp.Dispatch(context.Background(), tooldispatch.ToolCall{
			CallID:    "call-1",
			SessionID: "s",
			Tool:      "slow",
			Version:   "v1",
			Deadline:  time.Now().Add(time.Minute),
		})
		close(firstDone)
	}()

	select {
	case <-ready:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for worker to accept invoke")
	}

	secondDone := make(chan struct{})
	go func() {
		_, _ = disp.Dispatch(context.Background(), tooldispatch.ToolCall{
			CallID:    "call-2",
			SessionID: "s",
			Tool:      "slow",
			Version:   "v1",
			Deadline:  time.Now().Add(time.Minute),
		})
		close(secondDone)
	}()
	time.Sleep(30 * time.Millisecond)

	if reg.QueuedCount("slow", "v1") != 1 {
		t.Fatalf("queued = %d, want 1", reg.QueuedCount("slow", "v1"))
	}

	close(block)
	<-firstDone
	<-secondDone

	cancel()
}
