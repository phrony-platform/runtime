package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"

	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
	"github.com/phrony-platform/runtime/internal/clierr"
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
	if err := stream.Send(&runtimev1.RunSessionInteractiveClientMsg{
		Body: &runtimev1.RunSessionInteractiveClientMsg_Start{Start: start},
	}); err != nil {
		return clierr.WrapRPC("run session", err)
	}

	sendClosed := false
	closeSend := func() error {
		if sendClosed {
			return nil
		}
		sendClosed = true
		if err := stream.CloseSend(); err != nil {
			return clierr.WrapRPC("run session", err)
		}
		return nil
	}

	lineReader := bufio.NewReader(stdin)
	completionOut := newCompletionWriter(stdout)
	var completionPrinted bool

	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		msg, err := stream.Recv()
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return clierr.WrapRPC("run session", err)
		}

		switch {
		case msg.GetSessionStarted() != nil:
			fmt.Fprintf(stdout, "session %s started\n", msg.GetSessionStarted().GetSessionId())
		case msg.GetTextDelta() != nil:
			if err := completionOut.WriteDelta(msg.GetTextDelta().GetDelta()); err != nil {
				return err
			}
		case msg.GetAwaitingInput() != nil:
			if err := completionOut.Flush(); err != nil {
				return err
			}
			completionPrinted = true
			if _, err := io.WriteString(stdout, "\n"); err != nil {
				return err
			}
			if _, err := io.WriteString(stderr, "\n> "); err != nil {
				return err
			}
			line, readErr := lineReader.ReadString('\n')
			if readErr == io.EOF && strings.TrimSpace(line) == "" {
				if err := closeSend(); err != nil {
					return err
				}
				continue
			}
			if readErr != nil && readErr != io.EOF {
				return readErr
			}
			text := strings.TrimSpace(line)
			if text == "" {
				return fmt.Errorf("empty message; enter text or end input with Ctrl-D")
			}
			if err := stream.Send(&runtimev1.RunSessionInteractiveClientMsg{
				Body: &runtimev1.RunSessionInteractiveClientMsg_UserMessage{
					UserMessage: &runtimev1.RunSessionInteractiveUserMessage{Text: text},
				},
			}); err != nil {
				return clierr.WrapRPC("run session", err)
			}
		case msg.GetCompleted() != nil:
			if err := completionOut.Flush(); err != nil {
				return err
			}
			if !completionPrinted {
				if out := msg.GetCompleted().GetOutput(); len(out) > 0 {
					pretty, err := prettifySessionOutput(out)
					if err != nil {
						return err
					}
					if _, err := io.WriteString(stdout, "\n"); err != nil {
						return err
					}
					if _, err := stdout.Write(pretty); err != nil {
						return err
					}
				}
			}
			if _, err := io.WriteString(stdout, "\n"); err != nil {
				return err
			}
			return nil
		case msg.GetFailed() != nil:
			return fmt.Errorf("session failed: %s", msg.GetFailed().GetMessage())
		default:
			return fmt.Errorf("run session: unexpected server message")
		}
	}
}
