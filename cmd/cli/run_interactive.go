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

// sessionControls carries out-of-band session actions wired from the runtime
// client. Each field is nil when the corresponding action is not available
// (e.g. tests or the plain stream renderer), and controls itself may be nil.
type sessionControls struct {
	// cancel aborts a running session via the unary CancelSession RPC.
	cancel func(ctx context.Context, sessionID string) error
	// complete finalizes a running session via the unary CompleteSession RPC.
	complete func(ctx context.Context, sessionID string) error
}

func runInteractiveSession(
	ctx context.Context,
	stream interactiveStream,
	start *runtimev1.RunSessionInteractiveStart,
	stdin io.Reader,
	stdout, stderr io.Writer,
	controls *sessionControls,
	rt runtimev1.RuntimeClient,
) error {
	if interactiveUseTUI(stdin, stdout) {
		return runInteractiveSessionTUI(ctx, stream, start, controls, rt)
	}
	return runInteractiveSessionPlain(ctx, stream, start, stdin, stdout, stderr)
}
