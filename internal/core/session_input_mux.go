package core

import (
	"context"
	"io"
	"sync"

	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
	"google.golang.org/grpc/metadata"
)

// sessionInputMux merges inbound user_message and tool_approval messages from
// any attach client into the single session driver loop.
type sessionInputMux struct {
	ctx context.Context

	mu     sync.Mutex
	closed bool
	ch     chan *runtimev1.RunSessionInteractiveClientMsg
}

func newSessionInputMux(ctx context.Context) *sessionInputMux {
	return &sessionInputMux{
		ctx: ctx,
		ch:  make(chan *runtimev1.RunSessionInteractiveClientMsg, 8),
	}
}

func (m *sessionInputMux) Context() context.Context { return m.ctx }

func (m *sessionInputMux) Recv() (*runtimev1.RunSessionInteractiveClientMsg, error) {
	for {
		select {
		case <-m.ctx.Done():
			return nil, m.ctx.Err()
		case msg, ok := <-m.recvChan():
			if !ok {
				return nil, io.EOF
			}
			return msg, nil
		}
	}
}

func (m *sessionInputMux) recvChan() <-chan *runtimev1.RunSessionInteractiveClientMsg {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		ch := make(chan *runtimev1.RunSessionInteractiveClientMsg)
		close(ch)
		return ch
	}
	return m.ch
}

// deliver enqueues a client message for the driver loop. It returns false when
// the mux is closed or the driver context has ended.
func (m *sessionInputMux) deliver(msg *runtimev1.RunSessionInteractiveClientMsg) bool {
	if msg == nil || m.ctx.Err() != nil {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return false
	}
	select {
	case m.ch <- msg:
		return true
	case <-m.ctx.Done():
		return false
	}
}

func (m *sessionInputMux) close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return
	}
	m.closed = true
	close(m.ch)
}

func (m *sessionInputMux) Send(*runtimev1.RunSessionInteractiveServerMsg) error { return nil }

func (m *sessionInputMux) RecvMsg(msg interface{}) error {
	in, err := m.Recv()
	if err != nil {
		return err
	}
	out, ok := msg.(*runtimev1.RunSessionInteractiveClientMsg)
	if !ok {
		return io.EOF
	}
	*out = *in
	return nil
}

func (m *sessionInputMux) SendMsg(interface{}) error { return nil }

func (m *sessionInputMux) SetHeader(metadata.MD) error  { return nil }
func (m *sessionInputMux) SendHeader(metadata.MD) error { return nil }
func (m *sessionInputMux) SetTrailer(metadata.MD)       {}

var _ runtimev1.Runtime_RunSessionInteractiveServer = (*sessionInputMux)(nil)
