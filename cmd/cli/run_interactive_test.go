package main

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
)

type mockInteractiveClientStream struct {
	recv    []*runtimev1.RunSessionInteractiveServerMsg
	recvIdx int
	sent    []*runtimev1.RunSessionInteractiveClientMsg
}

func (m *mockInteractiveClientStream) Send(msg *runtimev1.RunSessionInteractiveClientMsg) error {
	m.sent = append(m.sent, msg)
	return nil
}

func (m *mockInteractiveClientStream) Recv() (*runtimev1.RunSessionInteractiveServerMsg, error) {
	if m.recvIdx >= len(m.recv) {
		return nil, io.EOF
	}
	msg := m.recv[m.recvIdx]
	m.recvIdx++
	return msg, nil
}

func (m *mockInteractiveClientStream) CloseSend() error { return nil }

func TestRunInteractiveSession_sessionStartedAndCompleted(t *testing.T) {
	stream := &mockInteractiveClientStream{
		recv: []*runtimev1.RunSessionInteractiveServerMsg{
			{Body: &runtimev1.RunSessionInteractiveServerMsg_SessionStarted{
				SessionStarted: &runtimev1.RunSessionInteractiveSessionStarted{SessionId: "sess-1"},
			}},
			{Body: &runtimev1.RunSessionInteractiveServerMsg_Completed{
				Completed: &runtimev1.RunSessionInteractiveCompleted{StopReason: "end_turn"},
			}},
		},
	}

	var stdout bytes.Buffer
	err := runInteractiveSession(
		context.Background(),
		stream,
		&runtimev1.RunSessionInteractiveStart{},
		strings.NewReader(""),
		&stdout,
		&bytes.Buffer{},
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("runInteractiveSession: %v", err)
	}
	if !strings.Contains(stdout.String(), "session sess-1 started") {
		t.Fatalf("stdout = %q, want session started", stdout.String())
	}
	if len(stream.sent) != 1 || stream.sent[0].GetStart() == nil {
		t.Fatal("want start message sent")
	}
}

func TestRunInteractiveSession_jsonCompletionPrettified(t *testing.T) {
	stream := &mockInteractiveClientStream{
		recv: []*runtimev1.RunSessionInteractiveServerMsg{
			{Body: &runtimev1.RunSessionInteractiveServerMsg_TextDelta{
				TextDelta: &runtimev1.RunSessionInteractiveTextDelta{
					Delta: `{"reply":"hello","topics":["greeting"],"refused":false}`,
				},
			}},
			{Body: &runtimev1.RunSessionInteractiveServerMsg_AwaitingInput{
				AwaitingInput: &runtimev1.RunSessionInteractiveAwaitingInput{StopReason: "end_turn"},
			}},
			{Body: &runtimev1.RunSessionInteractiveServerMsg_Completed{
				Completed: &runtimev1.RunSessionInteractiveCompleted{
					StopReason: "end_turn",
					Output:     []byte(`{"message":"{\"reply\":\"hello\",\"topics\":[\"greeting\"],\"refused\":false}","stop_reason":"end_turn"}`),
				},
			}},
		},
	}

	var stdout bytes.Buffer
	if err := runInteractiveSession(
		context.Background(),
		stream,
		&runtimev1.RunSessionInteractiveStart{},
		strings.NewReader(""),
		&stdout,
		&bytes.Buffer{},
		nil,
		nil,
	); err != nil {
		t.Fatalf("runInteractiveSession: %v", err)
	}
	got := stdout.String()
	if strings.Contains(got, `"reply":"hello"`) {
		t.Fatalf("stdout = %q, want prettified JSON not compact", got)
	}
	if !strings.Contains(got, "\n  \"reply\": \"hello\"") {
		t.Fatalf("stdout = %q, want indented reply field", got)
	}
}

func TestRunInteractiveSession_textDeltas(t *testing.T) {
	stream := &mockInteractiveClientStream{
		recv: []*runtimev1.RunSessionInteractiveServerMsg{
			{Body: &runtimev1.RunSessionInteractiveServerMsg_TextDelta{
				TextDelta: &runtimev1.RunSessionInteractiveTextDelta{Delta: "Hi"},
			}},
			{Body: &runtimev1.RunSessionInteractiveServerMsg_TextDelta{
				TextDelta: &runtimev1.RunSessionInteractiveTextDelta{Delta: "!"},
			}},
			{Body: &runtimev1.RunSessionInteractiveServerMsg_Completed{
				Completed: &runtimev1.RunSessionInteractiveCompleted{},
			}},
		},
	}

	var stdout bytes.Buffer
	if err := runInteractiveSession(context.Background(), stream, &runtimev1.RunSessionInteractiveStart{}, strings.NewReader(""), &stdout, &bytes.Buffer{}, nil, nil); err != nil {
		t.Fatalf("runInteractiveSession: %v", err)
	}
	if stdout.String() != "Hi!\n" {
		t.Fatalf("stdout = %q, want streamed text and trailing newline", stdout.String())
	}
}

func TestRunInteractiveSession_awaitingInputSendsUserMessage(t *testing.T) {
	stream := &mockInteractiveClientStream{
		recv: []*runtimev1.RunSessionInteractiveServerMsg{
			{Body: &runtimev1.RunSessionInteractiveServerMsg_AwaitingInput{
				AwaitingInput: &runtimev1.RunSessionInteractiveAwaitingInput{StopReason: "end_turn"},
			}},
			{Body: &runtimev1.RunSessionInteractiveServerMsg_Completed{
				Completed: &runtimev1.RunSessionInteractiveCompleted{},
			}},
		},
	}

	if err := runInteractiveSession(
		context.Background(),
		stream,
		&runtimev1.RunSessionInteractiveStart{},
		strings.NewReader("follow-up\n"),
		&bytes.Buffer{},
		&bytes.Buffer{},
		nil,
		nil,
	); err != nil {
		t.Fatalf("runInteractiveSession: %v", err)
	}
	if len(stream.sent) != 2 {
		t.Fatalf("sent %d messages, want start and user_message", len(stream.sent))
	}
	if um := stream.sent[1].GetUserMessage(); um == nil || um.GetText() != "follow-up" {
		t.Fatalf("second message = %+v, want user_message follow-up", stream.sent[1])
	}
}

func TestRunInteractiveSession_awaitingInputEOFCompletes(t *testing.T) {
	stream := &mockInteractiveClientStream{
		recv: []*runtimev1.RunSessionInteractiveServerMsg{
			{Body: &runtimev1.RunSessionInteractiveServerMsg_AwaitingInput{
				AwaitingInput: &runtimev1.RunSessionInteractiveAwaitingInput{},
			}},
			{Body: &runtimev1.RunSessionInteractiveServerMsg_Completed{
				Completed: &runtimev1.RunSessionInteractiveCompleted{},
			}},
		},
	}

	if err := runInteractiveSession(
		context.Background(),
		stream,
		&runtimev1.RunSessionInteractiveStart{},
		strings.NewReader(""),
		&bytes.Buffer{},
		&bytes.Buffer{},
		nil,
		nil,
	); err != nil {
		t.Fatalf("runInteractiveSession: %v", err)
	}
	if len(stream.sent) != 1 {
		t.Fatalf("sent %d messages, want only start", len(stream.sent))
	}
}

func TestRunInteractiveSession_plainModeShowsTokenStats(t *testing.T) {
	t.Setenv("PHRONY_NO_TUI", "1")
	stream := &mockInteractiveClientStream{
		recv: []*runtimev1.RunSessionInteractiveServerMsg{
			{Body: &runtimev1.RunSessionInteractiveServerMsg_SessionStarted{
				SessionStarted: &runtimev1.RunSessionInteractiveSessionStarted{SessionId: "sess-1"},
			}},
			{Body: &runtimev1.RunSessionInteractiveServerMsg_AwaitingInput{
				AwaitingInput: &runtimev1.RunSessionInteractiveAwaitingInput{
					StopReason: "end_turn",
					Stats: &runtimev1.InteractiveSessionStats{
						Turn: 1,
						TurnUsage: &runtimev1.TokenUsage{
							InputTokens:  12,
							OutputTokens: 4,
							TotalTokens:  16,
						},
						SessionUsage: &runtimev1.TokenUsage{
							InputTokens:  12,
							OutputTokens: 4,
							TotalTokens:  16,
						},
					},
				},
			}},
			{Body: &runtimev1.RunSessionInteractiveServerMsg_Completed{
				Completed: &runtimev1.RunSessionInteractiveCompleted{},
			}},
		},
	}

	var stdout bytes.Buffer
	if err := runInteractiveSession(
		context.Background(),
		stream,
		&runtimev1.RunSessionInteractiveStart{},
		strings.NewReader(""),
		&stdout,
		&bytes.Buffer{},
		nil,
		nil,
	); err != nil {
		t.Fatalf("runInteractiveSession: %v", err)
	}
	got := stdout.String()
	for _, want := range []string{"turn 1", "turn tokens", "12 in / 4 out"} {
		if !strings.Contains(got, want) {
			t.Fatalf("stdout = %q, want %q", got, want)
		}
	}
}

func TestRunInteractiveSession_attachFailedReadOnly(t *testing.T) {
	t.Setenv("PHRONY_NO_TUI", "1")
	stream := &mockInteractiveClientStream{
		recv: []*runtimev1.RunSessionInteractiveServerMsg{
			{Body: &runtimev1.RunSessionInteractiveServerMsg_SessionStarted{
				SessionStarted: &runtimev1.RunSessionInteractiveSessionStarted{SessionId: "sess-1"},
			}},
			{Body: &runtimev1.RunSessionInteractiveServerMsg_Failed{
				Failed: &runtimev1.RunSessionInteractiveFailed{Message: "model unavailable"},
			}},
		},
	}

	var stdout, stderr bytes.Buffer
	err := runInteractiveSession(
		context.Background(),
		stream,
		&runtimev1.RunSessionInteractiveStart{SessionId: "sess-1"},
		strings.NewReader(""),
		&stdout,
		&stderr,
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("runInteractiveSession: %v", err)
	}
	if len(stream.sent) != 1 || stream.sent[0].GetStart().GetSessionId() != "sess-1" {
		t.Fatalf("sent = %+v, want only start with session_id", stream.sent)
	}
	if !strings.Contains(stderr.String(), "session failed") {
		t.Fatalf("stderr = %q, want session failed banner", stderr.String())
	}
}

func TestRunInteractiveSession_attachCompletedReadOnly(t *testing.T) {
	t.Setenv("PHRONY_NO_TUI", "1")
	stream := &mockInteractiveClientStream{
		recv: []*runtimev1.RunSessionInteractiveServerMsg{
			{Body: &runtimev1.RunSessionInteractiveServerMsg_SessionStarted{
				SessionStarted: &runtimev1.RunSessionInteractiveSessionStarted{SessionId: "sess-1"},
			}},
			{Body: &runtimev1.RunSessionInteractiveServerMsg_Completed{
				Completed: &runtimev1.RunSessionInteractiveCompleted{
					StopReason: "end_turn",
					Output:     []byte(`{"message":"done","stop_reason":"end_turn"}`),
				},
			}},
		},
	}

	var stdout bytes.Buffer
	err := runInteractiveSession(
		context.Background(),
		stream,
		&runtimev1.RunSessionInteractiveStart{SessionId: "sess-1"},
		strings.NewReader(""),
		&stdout,
		&bytes.Buffer{},
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("runInteractiveSession: %v", err)
	}
	if len(stream.sent) != 1 || stream.sent[0].GetStart().GetSessionId() != "sess-1" {
		t.Fatalf("sent = %+v, want only start with session_id", stream.sent)
	}
	if stream.sent[0].GetStart().GetAgentRef() != nil {
		t.Fatal("attach start must not include agent_ref")
	}
	got := stdout.String()
	if !strings.Contains(got, "session complete") {
		t.Fatalf("stdout = %q, want session complete", got)
	}
}

func TestRunInteractiveSession_attachCancelledReadOnly(t *testing.T) {
	t.Setenv("PHRONY_NO_TUI", "1")
	stream := &mockInteractiveClientStream{
		recv: []*runtimev1.RunSessionInteractiveServerMsg{
			{Body: &runtimev1.RunSessionInteractiveServerMsg_SessionStarted{
				SessionStarted: &runtimev1.RunSessionInteractiveSessionStarted{SessionId: "sess-1"},
			}},
			{Body: &runtimev1.RunSessionInteractiveServerMsg_Cancelled{
				Cancelled: &runtimev1.RunSessionInteractiveCancelled{},
			}},
		},
	}

	var stdout, stderr bytes.Buffer
	err := runInteractiveSession(
		context.Background(),
		stream,
		&runtimev1.RunSessionInteractiveStart{SessionId: "sess-1"},
		strings.NewReader(""),
		&stdout,
		&stderr,
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("runInteractiveSession: %v", err)
	}
	if len(stream.sent) != 1 || stream.sent[0].GetStart().GetSessionId() != "sess-1" {
		t.Fatalf("sent = %+v, want only start with session_id", stream.sent)
	}
	if !strings.Contains(stderr.String(), "session cancelled") {
		t.Fatalf("stderr = %q, want session cancelled banner", stderr.String())
	}
}

func TestRunInteractiveSession_failed(t *testing.T) {
	stream := &mockInteractiveClientStream{
		recv: []*runtimev1.RunSessionInteractiveServerMsg{
			{Body: &runtimev1.RunSessionInteractiveServerMsg_Failed{
				Failed: &runtimev1.RunSessionInteractiveFailed{Message: "model unavailable"},
			}},
		},
	}

	err := runInteractiveSession(
		context.Background(),
		stream,
		&runtimev1.RunSessionInteractiveStart{},
		strings.NewReader(""),
		&bytes.Buffer{},
		&bytes.Buffer{},
		nil,
		nil,
	)
	if err == nil || !strings.Contains(err.Error(), "model unavailable") {
		t.Fatalf("err = %v, want session failed", err)
	}
}
