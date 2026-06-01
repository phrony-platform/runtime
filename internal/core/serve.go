package core

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"time"

	"google.golang.org/grpc"
)

// grpcShutdownGrace is how long to wait for ordinary RPCs before force-closing
// long-lived Work worker streams so process exit is not blocked.
const grpcShutdownGrace = 5 * time.Second

var listenTCP = func(addr string) (net.Listener, error) {
	return net.Listen("tcp", addr)
}

// Serve listens on addr until ctx is canceled, then stops gracefully.
func (s *Server) Serve(ctx context.Context, addr string) error {
	lis, err := listenTCP(addr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", addr, err)
	}

	serveDone := make(chan struct{})
	go func() {
		defer close(serveDone)
		if err := s.grpc.Serve(lis); err != nil && err != grpc.ErrServerStopped {
			slog.Error("gRPC serve failed", "addr", addr, "error", err)
		}
	}()

	slog.Info("runtime gRPC listening", "addr", addr)

	if s.runtime != nil {
		go s.runtime.reconcileSessionsOnStartup(ctx)
	}

	<-ctx.Done()
	slog.Info("shutting down runtime gRPC", "addr", addr)
	if s.runtime != nil && s.runtime.toolRegistry != nil {
		s.runtime.toolRegistry.Shutdown()
	}

	stopDone := make(chan struct{})
	go func() {
		s.grpc.GracefulStop()
		close(stopDone)
	}()
	select {
	case <-stopDone:
	case <-time.After(grpcShutdownGrace):
		slog.Info("forcing gRPC stop; dropping tool worker connections", "grace", grpcShutdownGrace)
		s.grpc.Stop()
	}
	<-serveDone
	return ctx.Err()
}
