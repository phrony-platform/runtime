package core

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
	"github.com/phrony-platform/runtime/internal/executor"
	"github.com/phrony-platform/runtime/internal/manifest"
	"github.com/phrony-platform/runtime/internal/model"
	"github.com/phrony-platform/runtime/internal/provider"
	"github.com/phrony-platform/runtime/internal/store"
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

	if len(stream.sent) < 1 {
		t.Fatalf("sent %d messages, want failed", len(stream.sent))
	}
	if stream.sent[0].GetFailed() == nil {
		t.Fatalf("first message = %T, want failed", stream.sent[0].GetBody())
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

func TestUserTextFromSessionInput_textField(t *testing.T) {
	text, err := userTextFromSessionInput(json.RawMessage(`{"text":" follow-up "}`))
	if err != nil {
		t.Fatalf("userTextFromSessionInput: %v", err)
	}
	if text != "follow-up" {
		t.Fatalf("text = %q, want follow-up", text)
	}
}

func TestUserTextFromSessionInput_empty(t *testing.T) {
	text, err := userTextFromSessionInput(nil)
	if err != nil {
		t.Fatalf("userTextFromSessionInput: %v", err)
	}
	if text != "" {
		t.Fatalf("text = %q, want empty", text)
	}
}

func TestUserTextFromSessionInput_invalidMessageType(t *testing.T) {
	_, err := userTextFromSessionInput(json.RawMessage(`{"message":42}`))
	if err == nil {
		t.Fatal("userTextFromSessionInput() = nil, want error")
	}
}

func TestUserTextFromSessionInput_notObject(t *testing.T) {
	_, err := userTextFromSessionInput(json.RawMessage(`"raw"`))
	if err == nil {
		t.Fatal("userTextFromSessionInput() = nil, want error")
	}
}

func TestAppendTurnHistory(t *testing.T) {
	h := appendTurnHistory(nil, "user", "assistant")
	if len(h) != 2 {
		t.Fatalf("len(history) = %d, want 2", len(h))
	}
	if h[0].Content != "user" || h[1].Content != "assistant" {
		t.Fatalf("history = %+v", h)
	}

	h = appendTurnHistory(h, "", "")
	if len(h) != 2 {
		t.Fatalf("empty turn should not append, len = %d", len(h))
	}
}

func TestInteractiveSessionState_runTurn_streamsDeltas(t *testing.T) {
	stream := &mockInteractiveStream{ctx: context.Background()}
	agent := &manifest.Agent{
		Spec: manifest.AgentSpec{
			Instructions: manifest.InstructionsSpec{Text: "System."},
			Model: manifest.ModelConfig{
				Provider: provider.IDAnthropic,
				Name:     "claude-sonnet-4-5",
			},
		},
	}
	st := &interactiveSessionState{
		sessionID: "sess-1",
		version:   executor.NewVersionWithProvider("version-uuid", agent, &interactiveDeltaStubProvider{}),
	}

	stopReason, text, _, err := st.runTurn(context.Background(), stream, json.RawMessage(`{"message":"hi"}`))
	if err != nil {
		t.Fatalf("runTurn: %v", err)
	}
	if stopReason != "end_turn" {
		t.Fatalf("stop_reason = %q, want end_turn", stopReason)
	}
	if text != "Hi there" {
		t.Fatalf("assistant text = %q", text)
	}
	var deltas int
	for _, msg := range stream.sent {
		if msg.GetTextDelta() != nil {
			deltas++
		}
	}
	if deltas != 2 {
		t.Fatalf("text_delta messages = %d, want 2", deltas)
	}
}

func TestInteractiveSessionState_runTurn_providerFailure(t *testing.T) {
	stream := &mockInteractiveStream{ctx: context.Background()}
	agent := &manifest.Agent{
		Spec: manifest.AgentSpec{
			Model: manifest.ModelConfig{Provider: provider.IDAnthropic, Name: "m"},
		},
	}
	st := &interactiveSessionState{
		version: executor.NewVersionWithProvider("v", agent, &interactiveFailStubProvider{}),
	}
	_, _, _, err := st.runTurn(context.Background(), stream, json.RawMessage(`{"message":"hi"}`))
	if err == nil {
		t.Fatal("runTurn() = nil, want error")
	}
}

func TestRuntime_completeInteractiveSession(t *testing.T) {
	db, mock := testSQLxDB(t)
	now := time.Now()
	output := json.RawMessage(`{"message":"ok","stop_reason":"end_turn"}`)
	mock.ExpectQuery(`UPDATE sessions`).
		WithArgs("sess-1", model.SessionStatusCompleted, output, nil, nil).
		WillReturnRows(sqlmock.NewRows([]string{"updated_at"}).AddRow(now))

	stream := &mockInteractiveStream{ctx: context.Background()}
	srv := &runtimeServer{db: db}
	err := srv.completeInteractiveSession(context.Background(), store.New(db), stream, "sess-1", "end_turn", output, 1, provider.TokenUsage{}, provider.TokenUsage{})
	if err != nil {
		t.Fatalf("completeInteractiveSession: %v", err)
	}
	if stream.sent[0].GetCompleted() == nil {
		t.Fatal("expected completed message")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestRuntime_failInteractiveSession(t *testing.T) {
	db, mock := testSQLxDB(t)
	now := time.Now()
	errMsg := "load failed"
	mock.ExpectQuery(`UPDATE sessions`).
		WithArgs("sess-1", model.SessionStatusFailed, nil, errMsg, nil).
		WillReturnRows(sqlmock.NewRows([]string{"updated_at"}).AddRow(now))

	stream := &mockInteractiveStream{ctx: context.Background()}
	srv := &runtimeServer{db: db}
	err := srv.failInteractiveSession(context.Background(), store.New(db), stream, "sess-1", fmt.Errorf("load failed"))
	if err != nil {
		t.Fatalf("failInteractiveSession: %v", err)
	}
	if stream.sent[0].GetFailed() == nil {
		t.Fatal("expected failed message")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

type interactiveDeltaStubProvider struct{}

func (d *interactiveDeltaStubProvider) ID() string { return provider.IDAnthropic }

func (d *interactiveDeltaStubProvider) Complete(ctx context.Context, req provider.CompletionRequest, ch chan<- provider.CompletionEvent) error {
	defer close(ch)
	ch <- provider.CompletionEvent{Type: provider.EventTextDelta, TextDelta: "Hi "}
	ch <- provider.CompletionEvent{Type: provider.EventTextDelta, TextDelta: "there"}
	ch <- provider.CompletionEvent{Type: provider.EventCompleted, StopReason: "end_turn"}
	return nil
}

type interactiveFailStubProvider struct{}

func (d *interactiveFailStubProvider) ID() string { return provider.IDAnthropic }

func (d *interactiveFailStubProvider) Complete(ctx context.Context, req provider.CompletionRequest, ch chan<- provider.CompletionEvent) error {
	defer close(ch)
	ch <- provider.CompletionEvent{Type: provider.EventFailed, Err: fmt.Errorf("model unavailable")}
	return nil
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

func TestRuntime_RunSessionInteractive_attachCompleted(t *testing.T) {
	db, mock := testSQLxDB(t)
	now := time.Now()
	output := []byte(`{"message":"done","stop_reason":"end_turn"}`)
	mock.ExpectQuery(`FROM sessions`).
		WithArgs("sess-1").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "agent_version_id", "input", "status", "output", "error", "history", "created_at", "updated_at",
		}).AddRow("sess-1", "version-uuid", []byte("{}"), model.SessionStatusCompleted, output, nil, []byte(`[]`), now, now))

	stream := &mockInteractiveStream{
		ctx: context.Background(),
		recv: []*runtimev1.RunSessionInteractiveClientMsg{
			{Body: &runtimev1.RunSessionInteractiveClientMsg_Start{
				Start: &runtimev1.RunSessionInteractiveStart{SessionId: "sess-1"},
			}},
		},
	}

	srv := &runtimeServer{
		db: db,
		loadSessionVersionFn: func(context.Context, *store.Queries, string) (*executor.Version, error) {
			return executor.NewVersionWithProvider("version-uuid", &manifest.Agent{
				Spec: manifest.AgentSpec{Model: manifest.ModelConfig{Provider: provider.IDAnthropic, Name: "m"}},
			}, &interactiveDeltaStubProvider{}), nil
		},
	}
	if err := srv.RunSessionInteractive(stream); err != nil {
		t.Fatalf("RunSessionInteractive: %v", err)
	}

	var started, completed bool
	for _, msg := range stream.sent {
		if msg.GetSessionStarted() != nil {
			started = true
		}
		if msg.GetCompleted() != nil {
			completed = true
			if string(msg.GetCompleted().GetOutput()) != string(output) {
				t.Fatalf("output = %s", msg.GetCompleted().GetOutput())
			}
		}
	}
	if !started || !completed {
		t.Fatalf("started=%v completed=%v", started, completed)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestRuntime_RunSessionInteractive_attachCompletedRejectsUserMessage(t *testing.T) {
	db, mock := testSQLxDB(t)
	now := time.Now()
	output := []byte(`{"message":"done","stop_reason":"end_turn"}`)
	mock.ExpectQuery(`FROM sessions`).
		WithArgs("sess-1").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "agent_version_id", "input", "status", "output", "error", "history", "created_at", "updated_at",
		}).AddRow("sess-1", "version-uuid", []byte("{}"), model.SessionStatusCompleted, output, nil, []byte(`[]`), now, now))

	stream := &mockInteractiveStream{
		ctx: context.Background(),
		recv: []*runtimev1.RunSessionInteractiveClientMsg{
			{Body: &runtimev1.RunSessionInteractiveClientMsg_Start{
				Start: &runtimev1.RunSessionInteractiveStart{SessionId: "sess-1"},
			}},
			{Body: &runtimev1.RunSessionInteractiveClientMsg_UserMessage{
				UserMessage: &runtimev1.RunSessionInteractiveUserMessage{Text: "more"},
			}},
		},
	}

	srv := &runtimeServer{
		db: db,
		loadSessionVersionFn: func(context.Context, *store.Queries, string) (*executor.Version, error) {
			return executor.NewVersionWithProvider("version-uuid", &manifest.Agent{
				Spec: manifest.AgentSpec{Model: manifest.ModelConfig{Provider: provider.IDAnthropic, Name: "m"}},
			}, &interactiveDeltaStubProvider{}), nil
		},
	}
	err := srv.RunSessionInteractive(stream)
	assertGRPCCode(t, err, codes.InvalidArgument)
}

func TestRuntime_RunSessionInteractive_attachAwaitingInputContinues(t *testing.T) {
	db, mock := testSQLxDB(t)
	now := time.Now()
	output := []byte(`{"message":"hi","stop_reason":"end_turn"}`)
	history := []byte(`[{"role":"user","content":"hello"},{"role":"assistant","content":"hi"}]`)
	mock.ExpectQuery(`FROM sessions`).
		WithArgs("sess-1").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "agent_version_id", "input", "status", "output", "error", "history", "created_at", "updated_at",
		}).AddRow("sess-1", "version-uuid", []byte(`{"message":"hello"}`), model.SessionStatusAwaitingInput, output, nil, history, now, now))
	mock.ExpectQuery(`UPDATE sessions`).
		WithArgs("sess-1", model.SessionStatusRunning, nil, nil, nil).
		WillReturnRows(sqlmock.NewRows([]string{"updated_at"}).AddRow(now))
	mock.ExpectQuery(`UPDATE sessions`).
		WithArgs("sess-1", model.SessionStatusAwaitingInput, sqlmock.AnyArg(), nil, sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"updated_at"}).AddRow(now))
	mock.ExpectQuery(`UPDATE sessions`).
		WithArgs("sess-1", model.SessionStatusCompleted, sqlmock.AnyArg(), nil, nil).
		WillReturnRows(sqlmock.NewRows([]string{"updated_at"}).AddRow(now))

	agent := &manifest.Agent{
		Spec: manifest.AgentSpec{
			Instructions: manifest.InstructionsSpec{Text: "System."},
			Model:        manifest.ModelConfig{Provider: provider.IDAnthropic, Name: "m"},
		},
	}

	stream := &mockInteractiveStream{
		ctx: context.Background(),
		recv: []*runtimev1.RunSessionInteractiveClientMsg{
			{Body: &runtimev1.RunSessionInteractiveClientMsg_Start{
				Start: &runtimev1.RunSessionInteractiveStart{SessionId: "sess-1"},
			}},
			{Body: &runtimev1.RunSessionInteractiveClientMsg_UserMessage{
				UserMessage: &runtimev1.RunSessionInteractiveUserMessage{Text: "follow-up"},
			}},
		},
	}

	srv := &runtimeServer{
		db: db,
		loadSessionVersionFn: func(context.Context, *store.Queries, string) (*executor.Version, error) {
			return executor.NewVersionWithProvider("version-uuid", agent, &interactiveDeltaStubProvider{}), nil
		},
	}
	if err := srv.RunSessionInteractive(stream); err != nil {
		t.Fatalf("RunSessionInteractive: %v", err)
	}

	var started, awaiting, completed bool
	for _, msg := range stream.sent {
		switch {
		case msg.GetSessionStarted() != nil:
			started = true
		case msg.GetAwaitingInput() != nil:
			awaiting = true
		case msg.GetCompleted() != nil:
			completed = true
		}
	}
	if !started || !awaiting || !completed {
		t.Fatalf("started=%v awaiting=%v completed=%v", started, awaiting, completed)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestRuntime_RunSessionInteractive_attachAlreadyActive(t *testing.T) {
	srv := &runtimeServer{activeSessions: &sync.Map{}}
	_ = srv.registerActiveSession("sess-1")

	stream := &mockInteractiveStream{
		ctx: context.Background(),
		recv: []*runtimev1.RunSessionInteractiveClientMsg{
			{Body: &runtimev1.RunSessionInteractiveClientMsg_Start{
				Start: &runtimev1.RunSessionInteractiveStart{SessionId: "sess-1"},
			}},
		},
	}

	db, mock := testSQLxDB(t)
	srv.db = db
	now := time.Now()
	mock.ExpectQuery(`FROM sessions`).
		WithArgs("sess-1").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "agent_version_id", "input", "status", "output", "error", "history", "created_at", "updated_at",
		}).AddRow("sess-1", "version-uuid", []byte("{}"), model.SessionStatusAwaitingInput, nil, nil, []byte(`[]`), now, now))

	err := srv.RunSessionInteractive(stream)
	assertGRPCCode(t, err, codes.FailedPrecondition)
}

func TestRuntime_RunSessionInteractive_attachRunningRejected(t *testing.T) {
	db, mock := testSQLxDB(t)
	now := time.Now()
	mock.ExpectQuery(`FROM sessions`).
		WithArgs("sess-1").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "agent_version_id", "input", "status", "output", "error", "history", "created_at", "updated_at",
		}).AddRow("sess-1", "version-uuid", []byte("{}"), model.SessionStatusRunning, nil, nil, []byte(`[]`), now, now))

	stream := &mockInteractiveStream{
		ctx: context.Background(),
		recv: []*runtimev1.RunSessionInteractiveClientMsg{
			{Body: &runtimev1.RunSessionInteractiveClientMsg_Start{
				Start: &runtimev1.RunSessionInteractiveStart{SessionId: "sess-1"},
			}},
		},
	}

	srv := &runtimeServer{db: db}
	err := srv.RunSessionInteractive(stream)
	assertGRPCCode(t, err, codes.FailedPrecondition)
}

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
