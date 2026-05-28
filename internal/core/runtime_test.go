package core

import (
	"context"
	"testing"

	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestRuntime_GetVersion(t *testing.T) {
	srv := &runtimeServer{}
	resp, err := srv.GetVersion(context.Background(), &runtimev1.GetVersionRequest{})
	if err != nil {
		t.Fatalf("GetVersion: %v", err)
	}
	if resp.GetVersion() != RuntimeVersion {
		t.Fatalf("version = %q, want %q", resp.GetVersion(), RuntimeVersion)
	}
}

func TestRuntime_RunSession_unimplemented(t *testing.T) {
	srv := &runtimeServer{}
	_, err := srv.RunSession(context.Background(), &runtimev1.RunSessionRequest{SessionId: "sess-1"})
	assertGRPCCode(t, err, codes.Unimplemented)
}

func TestRuntime_Deploy_unimplemented(t *testing.T) {
	srv := &runtimeServer{}
	_, err := srv.Deploy(context.Background(), &runtimev1.DeployRequest{Manifest: []byte("{}")})
	assertGRPCCode(t, err, codes.Unimplemented)
}

func assertGRPCCode(t *testing.T, err error, code codes.Code) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error with code %v, got nil", code)
	}
	st, ok := status.FromError(err)
	if !ok {
		t.Fatalf("expected gRPC status, got %v", err)
	}
	if st.Code() != code {
		t.Fatalf("code = %v, want %v", st.Code(), code)
	}
}
