package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/phrony-platform/runtime/e2e/internal/handlers"
	"github.com/phrony-platform/runtime/e2e/internal/workclient"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})))

	addr := envOr("PHRONY_RUNTIME_ADDR", "127.0.0.1:7777")
	dispatch := handlers.Dispatch
	switch os.Getenv("PLAYGROUND_WORKER_MODE") {
	case "nodispatch":
		dispatch = handlers.NoDispatchDispatch
	case "indeterminate":
		dispatch = handlers.IndeterminateDispatch
	}
	opts := workclient.Options{
		WorkerID:         envOr("WORKER_ID", "playground-worker-1"),
		WorkloadIdentity: envOr("WORKER_WORKLOAD_IDENTITY", "playground/local"),
		ImageDigest:      envOr("WORKER_IMAGE_DIGEST", "sha256:playground-dev"),
		Handlers:         handlers.DefaultAdvertisements(),
		Dispatch:         dispatch,
	}

	cc, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		slog.Error("grpc dial failed", "addr", addr, "error", err)
		os.Exit(1)
	}
	defer cc.Close()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	slog.Info("connecting to runtime", "addr", addr, "worker_id", opts.WorkerID)
	if err := workclient.Run(ctx, cc, opts); err != nil && ctx.Err() == nil {
		slog.Error("worker stopped", "error", err)
		os.Exit(1)
	}
	slog.Info("worker shutdown")
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
