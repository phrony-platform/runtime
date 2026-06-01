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

func printConversationHistory(stdout io.Writer, msgs []*runtimev1.InteractiveConversationMessage) error {
	var turn int32
	for _, msg := range msgs {
		switch msg.GetRole() {
		case "user":
			if _, err := fmt.Fprintf(stdout, "\nYOU\n%s\n", msg.GetContent()); err != nil {
				return err
			}
		case "assistant":
			turn++
			formatted, err := formatAssistantTranscript(msg.GetContent())
			if err != nil {
				return err
			}
			if _, err := io.WriteString(stdout, "\nAGENT\n"); err != nil {
				return err
			}
			if len(formatted) > 0 {
				if _, err := stdout.Write(formatted); err != nil {
					return err
				}
			}
			if line := formatTurnFooter(statsFromHistoryMessage(msg, turn), msg.GetStopReason(), durationFromHistoryMessage(msg)); line != "" {
				if _, err := fmt.Fprintf(stdout, "  %s\n", line); err != nil {
					return err
				}
			}
			if _, err := io.WriteString(stdout, "\n"); err != nil {
				return err
			}
		}
	}
	return nil
}

func runInteractiveSessionPlain(
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
			started := msg.GetSessionStarted()
			_, _ = fmt.Fprintf(stdout, "session %s started\n", started.GetSessionId())
			_, _ = fmt.Fprintf(stdout, "agent version %s · model %s\n",
				shortID(started.GetAgentVersionId()),
				formatModelLine(started.GetModelProvider(), started.GetModelName()),
			)
			if err := printConversationHistory(stdout, started.GetHistory()); err != nil {
				return err
			}
		case msg.GetTextDelta() != nil:
			if err := completionOut.WriteDelta(msg.GetTextDelta().GetDelta()); err != nil {
				return err
			}
		case msg.GetToolCall() != nil:
			if err := completionOut.Flush(); err != nil {
				return err
			}
			completionPrinted = true
			_, _ = fmt.Fprintf(stderr, "\n── %s ──\n", formatInteractiveToolCallLine(msg.GetToolCall()))
		case msg.GetToolResult() != nil:
			_, _ = fmt.Fprintf(stderr, "\n── %s ──\n", formatInteractiveToolResultLine(msg.GetToolResult()))
		case msg.GetApprovalRequired() != nil:
			if err := completionOut.Flush(); err != nil {
				return err
			}
			completionPrinted = true
			_, _ = fmt.Fprintf(stderr, "\n── %s ──\n", formatInteractiveApprovalLine(msg.GetApprovalRequired()))
			_, _ = io.WriteString(stderr, "Approve or deny via API; this CLI cannot submit tool_approval yet.\n")
		case msg.GetAwaitingInput() != nil:
			if err := completionOut.Flush(); err != nil {
				return err
			}
			completionPrinted = true
			awaiting := msg.GetAwaitingInput()
			if line := formatSessionStatsLine(awaiting.GetStats(), awaiting.GetStopReason()); line != "" {
				_, _ = io.WriteString(stdout, "\n── "+line+" ──\n")
			}
			if reason := strings.TrimSpace(awaiting.GetInputBlockedReason()); reason != "" {
				_, _ = fmt.Fprintf(stderr, "\nInput disabled: %s\n", reason)
				_, _ = io.WriteString(stderr, "End the session with Ctrl+D or Ctrl+C.\n")
				continue
			}
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
			completed := msg.GetCompleted()
			if !completionPrinted {
				if out := completed.GetOutput(); len(out) > 0 {
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
			if line := formatSessionStatsLine(completed.GetStats(), completed.GetStopReason()); line != "" {
				_, _ = io.WriteString(stdout, "\n── session complete · "+line+" ──\n")
			}
			if _, err := io.WriteString(stdout, "\n"); err != nil {
				return err
			}
			return nil
		case msg.GetFailed() != nil:
			failed := msg.GetFailed()
			if isInteractiveAttachReplay(start) {
				if line := strings.TrimSpace(failed.GetMessage()); line != "" {
					_, _ = io.WriteString(stderr, "\n── session failed · "+line+" ──\n")
				}
				return nil
			}
			return fmt.Errorf("session failed: %s", failed.GetMessage())
		default:
			return fmt.Errorf("run session: unexpected server message")
		}
	}
}
