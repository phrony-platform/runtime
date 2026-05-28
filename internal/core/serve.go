package core

import (
	"context"
	"fmt"
	"log/slog"
	"net"

	"google.golang.org/grpc"
)

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

	<-ctx.Done()
	slog.Info("shutting down runtime gRPC", "addr", addr)
	s.grpc.GracefulStop()
	<-serveDone
	return ctx.Err()
}
