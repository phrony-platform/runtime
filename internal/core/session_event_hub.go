package core

import (
	"sync"
	"sync/atomic"

	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
)

const sessionEventSubscriberBuffer = 64

// sessionEventHub fans out session events to multiple attach subscribers.
// Send is non-blocking: slow subscribers drop events rather than blocking the driver.
type sessionEventHub struct {
	mu          sync.RWMutex
	subscribers map[uint64]*sessionEventSubscriber
	nextID      atomic.Uint64
}

type sessionEventSubscriber struct {
	ch chan *runtimev1.RunSessionInteractiveServerMsg
}

func newSessionEventHub() *sessionEventHub {
	return &sessionEventHub{
		subscribers: make(map[uint64]*sessionEventSubscriber),
	}
}

func (h *sessionEventHub) Send(msg *runtimev1.RunSessionInteractiveServerMsg) error {
	if h == nil || msg == nil {
		return nil
	}
	h.mu.RLock()
	subs := make([]*sessionEventSubscriber, 0, len(h.subscribers))
	for _, sub := range h.subscribers {
		subs = append(subs, sub)
	}
	h.mu.RUnlock()
	for _, sub := range subs {
		select {
		case sub.ch <- msg:
		default:
		}
	}
	return nil
}

// Subscribe registers a new attach subscriber. The returned channel receives
// live events; call unsubscribe when the attach stream ends.
func (h *sessionEventHub) Subscribe() (events <-chan *runtimev1.RunSessionInteractiveServerMsg, unsubscribe func()) {
	id := h.nextID.Add(1)
	sub := &sessionEventSubscriber{
		ch: make(chan *runtimev1.RunSessionInteractiveServerMsg, sessionEventSubscriberBuffer),
	}
	h.mu.Lock()
	h.subscribers[id] = sub
	h.mu.Unlock()
	return sub.ch, func() {
		h.mu.Lock()
		delete(h.subscribers, id)
		h.mu.Unlock()
		close(sub.ch)
	}
}
