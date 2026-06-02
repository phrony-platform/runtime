package core

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
)

func TestSessionInputMux_deliverAndRecv(t *testing.T) {
	ctx := context.Background()
	mux := newSessionInputMux(ctx)

	go func() {
		if !mux.deliver(&runtimev1.RunSessionInteractiveClientMsg{
			Body: &runtimev1.RunSessionInteractiveClientMsg_UserMessage{
				UserMessage: &runtimev1.RunSessionInteractiveUserMessage{Text: "hi"},
			},
		}) {
			t.Error("deliver failed")
		}
	}()

	msg, err := mux.Recv()
	if err != nil {
		t.Fatalf("Recv: %v", err)
	}
	if msg.GetUserMessage().GetText() != "hi" {
		t.Fatalf("text = %q, want hi", msg.GetUserMessage().GetText())
	}
}

func TestSessionInputMux_blocksUntilDeliver(t *testing.T) {
	ctx := context.Background()
	mux := newSessionInputMux(ctx)

	recvDone := make(chan struct{})
	go func() {
		_, err := mux.Recv()
		if err != nil {
			t.Errorf("Recv: %v", err)
		}
		close(recvDone)
	}()

	select {
	case <-recvDone:
		t.Fatal("Recv returned before deliver")
	case <-time.After(20 * time.Millisecond):
	}

	if !mux.deliver(&runtimev1.RunSessionInteractiveClientMsg{
		Body: &runtimev1.RunSessionInteractiveClientMsg_UserMessage{
			UserMessage: &runtimev1.RunSessionInteractiveUserMessage{Text: "later"},
		},
	}) {
		t.Fatal("deliver failed")
	}

	select {
	case <-recvDone:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for Recv")
	}
}

func TestSessionInputMux_closeReturnsEOF(t *testing.T) {
	ctx := context.Background()
	mux := newSessionInputMux(ctx)
	mux.close()
	_, err := mux.Recv()
	if !errors.Is(err, io.EOF) {
		t.Fatalf("Recv err = %v, want EOF", err)
	}
}

func TestSessionInputMux_contextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	mux := newSessionInputMux(ctx)

	recvDone := make(chan error, 1)
	go func() {
		_, err := mux.Recv()
		recvDone <- err
	}()

	cancel()
	select {
	case err := <-recvDone:
		if err != context.Canceled {
			t.Fatalf("Recv err = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for Recv after cancel")
	}
}
