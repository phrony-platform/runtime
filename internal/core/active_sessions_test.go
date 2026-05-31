package core

import (
	"testing"

	"google.golang.org/grpc/codes"
)

func TestRuntime_unregisterActiveSession_nilMap(t *testing.T) {
	srv := &runtimeServer{}
	srv.unregisterActiveSession("sess-1")
}

func TestRuntime_registerActiveSession_duplicate(t *testing.T) {
	srv := &runtimeServer{}
	if err := srv.registerActiveSession("sess-1"); err != nil {
		t.Fatalf("registerActiveSession: %v", err)
	}
	err := srv.registerActiveSession("sess-1")
	assertGRPCCode(t, err, codes.FailedPrecondition)
	srv.unregisterActiveSession("sess-1")
}
