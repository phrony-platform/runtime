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
	if err := runInteractiveSession(context.Background(), stream, &runtimev1.RunSessionInteractiveStart{}, strings.NewReader(""), &stdout, &bytes.Buffer{}); err != nil {
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
	); err != nil {
		t.Fatalf("runInteractiveSession: %v", err)
	}
	if len(stream.sent) != 1 {
		t.Fatalf("sent %d messages, want only start", len(stream.sent))
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
	)
	if err == nil || !strings.Contains(err.Error(), "model unavailable") {
		t.Fatalf("err = %v, want session failed", err)
	}
}
