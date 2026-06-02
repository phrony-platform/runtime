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
	noopCancel := func() {}
	entry := activeSessionEntry{cancel: noopCancel}
	if err := srv.registerActiveSession("sess-1", entry); err != nil {
		t.Fatalf("registerActiveSession: %v", err)
	}
	err := srv.registerActiveSession("sess-1", entry)
	assertGRPCCode(t, err, codes.FailedPrecondition)
	srv.unregisterActiveSession("sess-1")
}
