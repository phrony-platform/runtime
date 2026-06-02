package core

import (
	"testing"

	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
)

func TestSessionEventHub_fanOutToSubscribers(t *testing.T) {
	hub := newSessionEventHub()
	ch1, unsub1 := hub.Subscribe()
	defer unsub1()
	ch2, unsub2 := hub.Subscribe()
	defer unsub2()

	msg := &runtimev1.RunSessionInteractiveServerMsg{
		Body: &runtimev1.RunSessionInteractiveServerMsg_TextDelta{
			TextDelta: &runtimev1.RunSessionInteractiveTextDelta{Delta: "hi"},
		},
	}
	if err := hub.Send(msg); err != nil {
		t.Fatalf("Send: %v", err)
	}

	got1 := <-ch1
	got2 := <-ch2
	if got1 != msg || got2 != msg {
		t.Fatalf("subscribers received different messages")
	}
}

func TestSessionEventHub_dropsWhenSubscriberBufferFull(t *testing.T) {
	hub := newSessionEventHub()
	ch, unsub := hub.Subscribe()
	defer unsub()

	for i := 0; i < sessionEventSubscriberBuffer+4; i++ {
		_ = hub.Send(&runtimev1.RunSessionInteractiveServerMsg{
			Body: &runtimev1.RunSessionInteractiveServerMsg_TextDelta{
				TextDelta: &runtimev1.RunSessionInteractiveTextDelta{Delta: "x"},
			},
		})
	}

	received := 0
	for {
		select {
		case <-ch:
			received++
		default:
			if received != sessionEventSubscriberBuffer {
				t.Fatalf("received %d events, want %d (buffer capacity)", received, sessionEventSubscriberBuffer)
			}
			return
		}
	}
}

func TestSessionEventHub_unsubscribeClosesChannel(t *testing.T) {
	hub := newSessionEventHub()
	ch, unsub := hub.Subscribe()
	unsub()
	_, open := <-ch
	if open {
		t.Fatal("expected subscriber channel to be closed after unsubscribe")
	}
}
