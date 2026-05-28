package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/phrony-platform/runtime/internal/core"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestStatusCommand_success(t *testing.T) {
	addr := startTestRuntimeAddr(t)

	var out bytes.Buffer
	root := NewRootCommand()
	root.SetOut(&out)
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"status", "--runtime-addr", addr})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out.String(), "version: "+core.RuntimeVersion) {
		t.Fatalf("output = %q, want version line", out.String())
	}
	if !strings.Contains(out.String(), "health: SERVING") {
		t.Fatalf("output = %q, want SERVING health", out.String())
	}
}

func TestRunCommand_unimplemented(t *testing.T) {
	addr := startTestRuntimeAddr(t)

	root := NewRootCommand()
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"run", "sess-1", "--runtime-addr", addr})

	err := root.Execute()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "not implemented on this runtime yet") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDeployCommand_unimplemented(t *testing.T) {
	addr := startTestRuntimeAddr(t)
	manifest := filepath.Join(t.TempDir(), "manifest.json")
	if err := os.WriteFile(manifest, []byte("{}"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	root := NewRootCommand()
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"deploy", "--file", manifest, "--runtime-addr", addr})

	err := root.Execute()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "not implemented on this runtime yet") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDeployCommand_readManifestFailed(t *testing.T) {
	root := NewRootCommand()
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"deploy", "--file", filepath.Join(t.TempDir(), "missing.json")})

	err := root.Execute()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "read manifest") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestFormatRPCError(t *testing.T) {
	unimplemented := status.Error(codes.Unimplemented, "Deploy is not implemented yet")
	err := formatRPCError("deploy", unimplemented)
	if !strings.Contains(err.Error(), "not implemented on this runtime yet") {
		t.Fatalf("unexpected error: %v", err)
	}

	other := status.Error(codes.Internal, "boom")
	err = formatRPCError("deploy", other)
	if !strings.Contains(err.Error(), "deploy:") {
		t.Fatalf("unexpected error: %v", err)
	}
}
