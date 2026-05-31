package main

import (
	"context"
	"io"
	"strings"

	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
)

// isInteractiveAttachReplay reports whether the client is attaching to an existing session.
func isInteractiveAttachReplay(start *runtimev1.RunSessionInteractiveStart) bool {
	return start != nil && strings.TrimSpace(start.GetSessionId()) != ""
}

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
