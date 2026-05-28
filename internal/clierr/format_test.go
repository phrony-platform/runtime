package clierr

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestFormat_nil(t *testing.T) {
	if Format(nil) != "" {
		t.Fatalf("Format(nil) = %q, want empty", Format(nil))
	}
}

func TestFormat_singleMessage(t *testing.T) {
	got := Format(errors.New("something went wrong"))
	if got != "Error: something went wrong" {
		t.Fatalf("Format() = %q", got)
	}
}

func TestFormat_wrappedChain(t *testing.T) {
	inner := &os.PathError{Op: "open", Path: "/tmp/missing.json", Err: os.ErrNotExist}
	err := fmt.Errorf("read manifest: %w", inner)
	got := Format(err)
	want := "Error: read manifest\n  open /tmp/missing.json: file does not exist"
	if got != want {
		t.Fatalf("Format() =\n%q\nwant\n%q", got, want)
	}
}

func TestWrapRPC_unimplemented(t *testing.T) {
	err := WrapRPC("deploy", status.Error(codes.Unimplemented, "Deploy is not implemented yet"))
	got := Format(err)
	if !strings.Contains(got, "Error: deploy: Deploy is not implemented yet (not implemented on this runtime yet)") {
		t.Fatalf("Format() = %q", got)
	}
}

func TestWrapRPC_otherCode(t *testing.T) {
	err := WrapRPC("deploy", status.Error(codes.Internal, "boom"))
	got := Format(err)
	if got != "Error: deploy: boom (Internal)" {
		t.Fatalf("Format() = %q", got)
	}
}

func TestWrapRPC_nonGRPC(t *testing.T) {
	err := WrapRPC("deploy", errors.New("connection refused"))
	got := Format(err)
	if !strings.Contains(got, "Error: deploy") || !strings.Contains(got, "connection refused") {
		t.Fatalf("Format() = %q", got)
	}
}
