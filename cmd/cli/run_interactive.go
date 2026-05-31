package main

import (
	"context"
	"io"

	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
)

// interactiveStream is the client side of RunSessionInteractive.
type interactiveStream interface {
	Send(*runtimev1.RunSessionInteractiveClientMsg) error
	Recv() (*runtimev1.RunSessionInteractiveServerMsg, error)
	CloseSend() error
}

func runInteractiveSession(
	ctx context.Context,
	stream interactiveStream,
	start *runtimev1.RunSessionInteractiveStart,
	stdin io.Reader,
	stdout, stderr io.Writer,
) error {
	if interactiveUseTUI(stdin, stdout) {
		return runInteractiveSessionTUI(ctx, stream, start)
	}
	return runInteractiveSessionPlain(ctx, stream, start, stdin, stdout, stderr)
}
