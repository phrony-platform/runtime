package core

import (
	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
)

// sessionEventSink delivers outbound RunSessionInteractive server events.
type sessionEventSink interface {
	Send(*runtimev1.RunSessionInteractiveServerMsg) error
}

// grpcStreamEventSink forwards events to a single interactive gRPC stream.
type grpcStreamEventSink struct {
	stream runtimev1.Runtime_RunSessionInteractiveServer
}

func (s grpcStreamEventSink) Send(msg *runtimev1.RunSessionInteractiveServerMsg) error {
	if s.stream == nil {
		return nil
	}
	return s.stream.Send(msg)
}

func sessionEventsFromStream(stream runtimev1.Runtime_RunSessionInteractiveServer) sessionEventSink {
	if stream == nil {
		return noopSessionEventSink{}
	}
	return grpcStreamEventSink{stream: stream}
}

// noopSessionEventSink discards outbound events (zero attach subscribers).
type noopSessionEventSink struct{}

func (noopSessionEventSink) Send(*runtimev1.RunSessionInteractiveServerMsg) error { return nil }
