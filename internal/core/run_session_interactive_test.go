package core

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
	"github.com/phrony-platform/runtime/internal/model"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
)

func TestRuntime_RunSessionInteractive_firstMessageMustBeStart(t *testing.T) {
	stream := &mockInteractiveStream{
		ctx:  context.Background(),
		recv: []*runtimev1.RunSessionInteractiveClientMsg{},
	}
	srv := &runtimeServer{db: testServeDB(t)}
	err := srv.RunSessionInteractive(stream)
	assertGRPCCode(t, err, codes.InvalidArgument)
}

func TestRuntime_RunSessionInteractive_userMessageBeforeStart(t *testing.T) {
	stream := &mockInteractiveStream{
		ctx: context.Background(),
		recv: []*runtimev1.RunSessionInteractiveClientMsg{
			{Body: &runtimev1.RunSessionInteractiveClientMsg_UserMessage{
				UserMessage: &runtimev1.RunSessionInteractiveUserMessage{Text: "hi"},
			}},
		},
	}
	srv := &runtimeServer{db: testServeDB(t)}
	err := srv.RunSessionInteractive(stream)
	assertGRPCCode(t, err, codes.InvalidArgument)
}

func TestRuntime_RunSessionInteractive_noDatabase(t *testing.T) {
	stream := &mockInteractiveStream{
		ctx: context.Background(),
		recv: []*runtimev1.RunSessionInteractiveClientMsg{
			{Body: &runtimev1.RunSessionInteractiveClientMsg_Start{
				Start: &runtimev1.RunSessionInteractiveStart{
					AgentRef: &runtimev1.AgentRef{Namespace: "demo", Name: "echo"},
				},
			}},
		},
	}
	srv := &runtimeServer{}
	err := srv.RunSessionInteractive(stream)
	assertGRPCCode(t, err, codes.FailedPrecondition)
}

func TestRuntime_RunSessionInteractive_sessionStartedThenFailedOnLoad(t *testing.T) {
	db, mock := testSQLxDB(t)
	mock.ExpectQuery(`FROM agent_versions av`).
		WithArgs("demo", "echo-agent").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("version-uuid"))
	mock.ExpectQuery(`INSERT INTO sessions`).
		WithArgs(sqlmock.AnyArg(), "version-uuid", []byte("{}"), model.SessionStatusRunning).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("sess-1"))
	mock.ExpectQuery(`SELECT manifest`).
		WithArgs("version-uuid").
		WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery(`UPDATE sessions`).
		WillReturnRows(sqlmock.NewRows([]string{"updated_at"}).AddRow(time.Now()))

	stream := &mockInteractiveStream{
		ctx: context.Background(),
		recv: []*runtimev1.RunSessionInteractiveClientMsg{
			{Body: &runtimev1.RunSessionInteractiveClientMsg_Start{
				Start: &runtimev1.RunSessionInteractiveStart{
					AgentRef: &runtimev1.AgentRef{Namespace: "demo", Name: "echo-agent"},
				},
			}},
		},
	}

	srv := &runtimeServer{db: db, secretsEnc: mustTestEncryptor(t)}
	err := srv.RunSessionInteractive(stream)
	if err != nil {
		t.Fatalf("RunSessionInteractive: %v", err)
	}

	if len(stream.sent) < 2 {
		t.Fatalf("sent %d messages, want at least session_started and failed", len(stream.sent))
	}
	if stream.sent[0].GetSessionStarted() == nil {
		t.Fatalf("first message = %T, want session_started", stream.sent[0].GetBody())
	}
	if stream.sent[1].GetFailed() == nil {
		t.Fatalf("second message = %T, want failed", stream.sent[1].GetBody())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestUserTextFromSessionInput(t *testing.T) {
	text, err := userTextFromSessionInput(json.RawMessage(`{"message":" hello "}`))
	if err != nil {
		t.Fatalf("userTextFromSessionInput: %v", err)
	}
	if text != "hello" {
		t.Fatalf("text = %q, want hello", text)
	}
}

type mockInteractiveStream struct {
	ctx     context.Context
	recv    []*runtimev1.RunSessionInteractiveClientMsg
	sent    []*runtimev1.RunSessionInteractiveServerMsg
	recvIdx int
}

func (m *mockInteractiveStream) Context() context.Context { return m.ctx }

func (m *mockInteractiveStream) Recv() (*runtimev1.RunSessionInteractiveClientMsg, error) {
	if m.recvIdx >= len(m.recv) {
		return nil, io.EOF
	}
	msg := m.recv[m.recvIdx]
	m.recvIdx++
	return msg, nil
}

func (m *mockInteractiveStream) Send(msg *runtimev1.RunSessionInteractiveServerMsg) error {
	m.sent = append(m.sent, msg)
	return nil
}

func (m *mockInteractiveStream) RecvMsg(msg interface{}) error {
	in, err := m.Recv()
	if err != nil {
		return err
	}
	out, ok := msg.(*runtimev1.RunSessionInteractiveClientMsg)
	if !ok {
		return fmt.Errorf("recv into %T", msg)
	}
	*out = *in
	return nil
}

func (m *mockInteractiveStream) SendMsg(msg interface{}) error {
	out, ok := msg.(*runtimev1.RunSessionInteractiveServerMsg)
	if !ok {
		return fmt.Errorf("send %T", msg)
	}
	return m.Send(out)
}

func (m *mockInteractiveStream) SetHeader(metadata.MD) error  { return nil }
func (m *mockInteractiveStream) SendHeader(metadata.MD) error { return nil }
func (m *mockInteractiveStream) SetTrailer(metadata.MD)       {}

func TestRuntime_RunSessionInteractive_invalidInput(t *testing.T) {
	db, mock := testSQLxDB(t)
	mock.ExpectQuery(`FROM agent_versions av`).
		WithArgs("demo", "echo-agent").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("version-uuid"))

	stream := &mockInteractiveStream{
		ctx: context.Background(),
		recv: []*runtimev1.RunSessionInteractiveClientMsg{
			{Body: &runtimev1.RunSessionInteractiveClientMsg_Start{
				Start: &runtimev1.RunSessionInteractiveStart{
					AgentRef: &runtimev1.AgentRef{Namespace: "demo", Name: "echo-agent"},
					Input:    []byte(`["not","object"]`),
				},
			}},
		},
	}

	srv := &runtimeServer{db: db}
	err := srv.RunSessionInteractive(stream)
	assertGRPCCode(t, err, codes.InvalidArgument)
	if !strings.Contains(statusMessage(t, err), "JSON object") {
		t.Fatalf("error = %v, want JSON object", err)
	}
}
