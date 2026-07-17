//go:build integration

package harness

import (
	"context"
	"testing"
	"time"

	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health/grpc_health_v1"
)

// RuntimeClient dials the runtime gRPC API.
func RuntimeClient(t *testing.T) runtimev1.RuntimeClient {
	t.Helper()
	cc, err := grpc.NewClient(
		RuntimeAddr(),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("grpc.NewClient: %v", err)
	}
	t.Cleanup(func() { _ = cc.Close() })
	return runtimev1.NewRuntimeClient(cc)
}

// RuntimeHealthy returns true when the runtime health check succeeds.
func RuntimeHealthy(addr string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	cc, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return false
	}
	defer cc.Close()
	resp, err := grpc_health_v1.NewHealthClient(cc).Check(ctx, &grpc_health_v1.HealthCheckRequest{})
	if err != nil {
		return false
	}
	return resp.GetStatus() == grpc_health_v1.HealthCheckResponse_SERVING
}

// SkipIfNoRuntime skips the test when the runtime is unreachable.
func SkipIfNoRuntime(t *testing.T) {
	t.Helper()
	addr := RuntimeAddr()
	if !RuntimeHealthy(addr) {
		t.Skipf("runtime not available at %s; run make dev-up in runtime and set RUNTIME_ENABLE_STUB_PROVIDER=true", addr)
	}
	Note(t, "runtime healthy at %s", addr)
}
