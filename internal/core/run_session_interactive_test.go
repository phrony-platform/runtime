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
	"github.com/phrony-platform/runtime/internal/providertest"
	"github.com/phrony-platform/runtime/internal/store"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
)

// expectGetRunningSessionForAttach satisfies GetSession when RunSessionInteractive
// starts a session with agent_ref and immediately attaches to the background driver.
func expectGetRunningSessionForAttach(mock sqlmock.Sqlmock, agentVersionID string, input []byte) {
	now := time.Now()
	mock.ExpectQuery(`FROM sessions`).
		WithArgs(sqlmock.AnyArg()).
		WillReturnRows(sessionMockRows("run_attach", agentVersionID, model.SessionStatusRunning, input, nil, "run_attach", 1, now, now))
	expectAttachSessionFoldQueriesAny(mock, nil, now)
}

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
	expectActiveDeployment(mock, "demo", "echo-agent", "version-uuid", "1.2.0")
	expectCreateRunSessionMocks(mock, "version-uuid", []byte("{}"))
	mock.ExpectQuery(`SELECT manifest`).
		WithArgs("version-uuid").
		WillReturnError(sql.ErrNoRows)
	expectAppendSessionFailedTxAny(mock, 2)

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
	h := appendTurnHistory(nil, "user", "assistant", "end_turn", provider.TokenUsage{InputTokens: 3, OutputTokens: 2}, 1500*time.Millisecond)
	if len(h) != 2 {
		t.Fatalf("len(history) = %d, want 2", len(h))
	}
	if h[0].Content != "user" || h[1].Content != "assistant" {
		t.Fatalf("history = %+v", h)
	}
	if h[1].StopReason != "end_turn" || h[1].TurnUsage.InputTokens != 3 {
		t.Fatalf("assistant metadata = %+v", h[1])
	}

	h = appendTurnHistory(h, "", "", "", provider.TokenUsage{}, 0)
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
		version:   executor.NewVersionWithProvider("version-uuid", agent, providertest.DeltaCompleted()),
	}

	stopReason, text, _, err := st.runTurn(context.Background(), nil, stream, json.RawMessage(`{"message":"hi"}`))
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
		version: executor.NewVersionWithProvider("v", agent, providertest.Fail(fmt.Errorf("model unavailable"))),
	}
	_, _, _, err := st.runTurn(context.Background(), nil, stream, json.RawMessage(`{"message":"hi"}`))
	if err == nil {
		t.Fatal("runTurn() = nil, want error")
	}
}

func TestRuntime_completeInteractiveSession(t *testing.T) {
	db, mock := testSQLxDB(t)
	output := json.RawMessage(`{"message":"ok","stop_reason":"end_turn"}`)
	expectAppendSessionCompletedTx(mock, "sess-1", 1)
	mock.ExpectExec(`DELETE FROM session_secrets`).
		WithArgs("sess-1").
		WillReturnResult(sqlmock.NewResult(0, 1))

	stream := &mockInteractiveStream{ctx: context.Background()}
	srv := &runtimeServer{db: db}
	err := srv.completeInteractiveSession(context.Background(), store.New(db), stream, "sess-1", "end_turn", output, 1, provider.TokenUsage{}, provider.TokenUsage{}, nil)
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
	errMsg := "load failed"
	expectAppendSessionFailedTx(mock, "sess-1", 1, errMsg)
	mock.ExpectExec(`DELETE FROM session_secrets`).
		WithArgs("sess-1").
		WillReturnResult(sqlmock.NewResult(0, 1))

	stream := &mockInteractiveStream{ctx: context.Background()}
	srv := &runtimeServer{db: db}
	err := srv.failInteractiveSession(context.Background(), store.New(db), sessionEventsFromStream(stream), "sess-1", fmt.Errorf("load failed"))
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

// blockingAfterStartStream keeps the attach recv side open until the stream
// context is cancelled, so tests can forward live hub events before detach.
type blockingAfterStartStream struct {
	*mockInteractiveStream
}

func (s *blockingAfterStartStream) Recv() (*runtimev1.RunSessionInteractiveClientMsg, error) {
	if s.recvIdx < len(s.recv) {
		return s.mockInteractiveStream.Recv()
	}
	<-s.ctx.Done()
	return nil, io.EOF
}

func waitForInteractiveMessages(
	t *testing.T,
	done <-chan error,
	sent func() []*runtimev1.RunSessionInteractiveServerMsg,
	pred func([]*runtimev1.RunSessionInteractiveServerMsg) bool,
) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		if pred(sent()) {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for interactive messages, sent=%+v", sent())
		case err := <-done:
			if err != nil {
				t.Fatalf("RunSessionInteractive: %v", err)
			}
			if pred(sent()) {
				return
			}
			t.Fatalf("interactive stream ended early, sent=%+v", sent())
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}
}

func TestRuntime_RunSessionInteractive_attachCompleted(t *testing.T) {
	db, mock := testSQLxDB(t)
	now := time.Now()
	output := []byte(`{"message":"done","stop_reason":"end_turn","turn_usage":{"input_tokens":10,"output_tokens":5},"session_usage":{"input_tokens":10,"output_tokens":5,"total_tokens":15}}`)
	events := foldEventsFromOutputJSON("sess-1", output)
	mock.ExpectQuery(`FROM sessions`).
		WithArgs("sess-1").
		WillReturnRows(sessionMockRows("sess-1", "version-uuid", model.SessionStatusCompleted, []byte(`{}`), nil, "sess-1", len(events), now, now))
	expectAttachSessionFoldQueries(mock, "sess-1", events, now)

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
		loadSessionVersionFn: func(context.Context, *store.Queries, string, string) (*executor.Version, error) {
			return executor.NewVersionWithProvider("version-uuid", &manifest.Agent{
				Spec: manifest.AgentSpec{Model: manifest.ModelConfig{Provider: provider.IDAnthropic, Name: "m"}},
			}, providertest.DeltaCompleted()), nil
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
			expectedOutput := buildSessionOutput(events)
			if string(msg.GetCompleted().GetOutput()) != string(expectedOutput) {
				t.Fatalf("output = %s, want %s", msg.GetCompleted().GetOutput(), expectedOutput)
			}
			stats := msg.GetCompleted().GetStats()
			if stats == nil || stats.GetSessionUsage() == nil {
				t.Fatalf("completed stats = %+v, want session_usage", stats)
			}
			if stats.GetSessionUsage().GetInputTokens() != 10 || stats.GetSessionUsage().GetOutputTokens() != 5 {
				t.Fatalf("session_usage = %+v", stats.GetSessionUsage())
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
	events := foldEventsFromOutputJSON("sess-1", output)
	mock.ExpectQuery(`FROM sessions`).
		WithArgs("sess-1").
		WillReturnRows(sessionMockRows("sess-1", "version-uuid", model.SessionStatusCompleted, []byte(`{}`), nil, "sess-1", len(events), now, now))
	expectAttachSessionFoldQueries(mock, "sess-1", events, now)

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
		loadSessionVersionFn: func(context.Context, *store.Queries, string, string) (*executor.Version, error) {
			return executor.NewVersionWithProvider("version-uuid", &manifest.Agent{
				Spec: manifest.AgentSpec{Model: manifest.ModelConfig{Provider: provider.IDAnthropic, Name: "m"}},
			}, providertest.DeltaCompleted()), nil
		},
	}
	err := srv.RunSessionInteractive(stream)
	assertGRPCCode(t, err, codes.InvalidArgument)
}

func TestRuntime_RunSessionInteractive_attachAwaitingInputContinues(t *testing.T) {
	db, mock := testSQLxDB(t)
	mock.MatchExpectationsInOrder(false)
	now := time.Now()
	events := foldEventsWithMessages("sess-1", "hello", "hi", "end_turn", provider.TokenUsage{InputTokens: 10, OutputTokens: 5})
	mock.ExpectQuery(`FROM sessions`).
		WithArgs("sess-1").
		WillReturnRows(sessionMockRows("sess-1", "version-uuid", model.SessionStatusAwaitingInput, []byte(`{"message":"hello"}`), nil, "sess-1", len(events), now, now))
	for i := 0; i < 3; i++ {
		expectListEventsBySession(mock, "sess-1", events, now)
	}
	mock.ExpectQuery(`UPDATE sessions`).
		WithArgs("sess-1", model.SessionStatusRunning, nil).
		WillReturnRows(sqlmock.NewRows([]string{"updated_at"}).AddRow(now))
	expectRecordTurnEvents(mock, "sess-1", 3, 4, true)
	turnEvents := append(events, store.Event{
		ID: 3, SessionID: "sess-1", RootSessionID: "sess-1", Seq: 3, TS: now,
		Type: EventMessageUser, Actor: ActorUser, Payload: userMessagePayload("follow-up"),
	}, store.Event{
		ID: 4, SessionID: "sess-1", RootSessionID: "sess-1", Seq: 4, TS: now,
		Type: EventMessageAssistant, Actor: ActorAgent, Payload: assistantMessagePayload("Hi there", "end_turn", provider.TokenUsage{}, 0),
	})
	expectListEventsBySession(mock, "sess-1", turnEvents, now)
	expectListEventsBySession(mock, "sess-1", turnEvents, now)
	expectParkAwaitingInput(mock, "sess-1", now)
	expectAppendSessionCompletedTxWithBegin(mock, "sess-1", 5)

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
		loadSessionVersionFn: func(context.Context, *store.Queries, string, string) (*executor.Version, error) {
			return executor.NewVersionWithProvider("version-uuid", agent, providertest.DeltaCompleted()), nil
		},
	}
	if err := srv.RunSessionInteractive(stream); err != nil {
		t.Fatalf("RunSessionInteractive: %v", err)
	}

	var started, attachAwaiting, completed bool
	var awaitingCount int
	for _, msg := range stream.sent {
		switch {
		case msg.GetSessionStarted() != nil:
			started = true
			h := msg.GetSessionStarted().GetHistory()
			if len(h) != 2 || h[0].GetRole() != "user" || h[0].GetContent() != "hello" ||
				h[1].GetRole() != "assistant" || h[1].GetContent() != "hi" {
				t.Fatalf("history = %+v", h)
			}
		case msg.GetAwaitingInput() != nil:
			awaitingCount++
			if awaitingCount == 1 {
				attachAwaiting = true
				stats := msg.GetAwaitingInput().GetStats()
				if stats == nil || stats.GetTurn() != 1 {
					t.Fatalf("attach stats turn = %+v", stats)
				}
				su := stats.GetSessionUsage()
				if su == nil || su.GetInputTokens() != 10 || su.GetOutputTokens() != 5 {
					t.Fatalf("attach session_usage = %+v", su)
				}
				tu := stats.GetTurnUsage()
				if tu == nil || tu.GetInputTokens() != 10 {
					t.Fatalf("attach turn_usage = %+v", tu)
				}
			}
		case msg.GetCompleted() != nil:
			completed = true
		}
	}
	if !started || !attachAwaiting || !completed {
		t.Fatalf("started=%v attachAwaiting=%v completed=%v", started, attachAwaiting, completed)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestRuntime_RunSessionInteractive_attachActiveDriverMissingHub(t *testing.T) {
	srv := &runtimeServer{activeSessions: &sync.Map{}}
	_ = srv.registerActiveSession("sess-1", activeSessionEntry{cancel: func() {}})

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
		WillReturnRows(sessionMockRows("sess-1", "version-uuid", model.SessionStatusRunning, []byte(`{}`), nil, "sess-1", 0, now, now))
	expectAttachSessionFoldQueries(mock, "sess-1", nil, now)

	err := srv.RunSessionInteractive(stream)
	assertGRPCCode(t, err, codes.Internal)
}

func TestRuntime_RunSessionInteractive_attachNotFound(t *testing.T) {
	db, mock := testSQLxDB(t)
	mock.ExpectQuery(`FROM sessions`).
		WithArgs("missing").
		WillReturnError(sql.ErrNoRows)

	stream := &mockInteractiveStream{
		ctx: context.Background(),
		recv: []*runtimev1.RunSessionInteractiveClientMsg{
			{Body: &runtimev1.RunSessionInteractiveClientMsg_Start{
				Start: &runtimev1.RunSessionInteractiveStart{SessionId: "missing"},
			}},
		},
	}

	srv := &runtimeServer{db: db}
	err := srv.RunSessionInteractive(stream)
	assertGRPCCode(t, err, codes.NotFound)
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestRuntime_RunSessionInteractive_attachPendingRejected(t *testing.T) {
	db, mock := testSQLxDB(t)
	now := time.Now()
	mock.ExpectQuery(`FROM sessions`).
		WithArgs("sess-1").
		WillReturnRows(sessionMockRows("sess-1", "version-uuid", model.SessionStatusPending, []byte(`{}`), nil, "sess-1", 0, now, now))

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

func TestRuntime_RunSessionInteractive_attachInvalidHistory(t *testing.T) {
	db, mock := testSQLxDB(t)
	now := time.Now()
	mock.ExpectQuery(`FROM sessions`).
		WithArgs("sess-1").
		WillReturnRows(sessionMockRows("sess-1", "version-uuid", model.SessionStatusAwaitingInput, []byte(`{}`), nil, "sess-1", 0, now, now))
	mock.ExpectQuery(`FROM events`).WithArgs("sess-1").WillReturnError(sql.ErrConnDone)

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
		loadSessionVersionFn: func(context.Context, *store.Queries, string, string) (*executor.Version, error) {
			return executor.NewVersionWithProvider("version-uuid", &manifest.Agent{
				Spec: manifest.AgentSpec{Model: manifest.ModelConfig{Provider: provider.IDAnthropic, Name: "m"}},
			}, providertest.DeltaCompleted()), nil
		},
	}
	err := srv.RunSessionInteractive(stream)
	assertGRPCCode(t, err, codes.Internal)
}

func TestRuntime_RunSessionInteractive_attachFailed(t *testing.T) {
	db, mock := testSQLxDB(t)
	now := time.Now()
	errMsg := "model unavailable"
	mock.ExpectQuery(`FROM sessions`).
		WithArgs("sess-1").
		WillReturnRows(sessionMockRows("sess-1", "version-uuid", model.SessionStatusFailed, []byte(`{}`), &errMsg, "sess-1", 1, now, now))
	expectAttachSessionFoldQueries(mock, "sess-1", nil, now)

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
		loadSessionVersionFn: func(context.Context, *store.Queries, string, string) (*executor.Version, error) {
			return executor.NewVersionWithProvider("version-uuid", &manifest.Agent{
				Spec: manifest.AgentSpec{Model: manifest.ModelConfig{Provider: provider.IDAnthropic, Name: "m"}},
			}, providertest.DeltaCompleted()), nil
		},
	}
	if err := srv.RunSessionInteractive(stream); err != nil {
		t.Fatalf("RunSessionInteractive: %v", err)
	}

	var started, failed bool
	for _, msg := range stream.sent {
		if msg.GetSessionStarted() != nil {
			started = true
		}
		if f := msg.GetFailed(); f != nil {
			failed = true
			if f.GetMessage() != errMsg {
				t.Fatalf("failed message = %q, want %q", f.GetMessage(), errMsg)
			}
		}
	}
	if !started || !failed {
		t.Fatalf("started=%v failed=%v", started, failed)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestRuntime_RunSessionInteractive_attachCancelled(t *testing.T) {
	db, mock := testSQLxDB(t)
	now := time.Now()
	mock.ExpectQuery(`FROM sessions`).
		WithArgs("sess-1").
		WillReturnRows(sessionMockRows("sess-1", "version-uuid", model.SessionStatusCancelled, []byte(`{}`), nil, "sess-1", 1, now, now))
	expectAttachSessionFoldQueries(mock, "sess-1", nil, now)

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
		loadSessionVersionFn: func(context.Context, *store.Queries, string, string) (*executor.Version, error) {
			return executor.NewVersionWithProvider("version-uuid", &manifest.Agent{
				Spec: manifest.AgentSpec{Model: manifest.ModelConfig{Provider: provider.IDAnthropic, Name: "m"}},
			}, providertest.DeltaCompleted()), nil
		},
	}
	if err := srv.RunSessionInteractive(stream); err != nil {
		t.Fatalf("RunSessionInteractive: %v", err)
	}

	var started, cancelled bool
	for _, msg := range stream.sent {
		if msg.GetSessionStarted() != nil {
			started = true
			if ms := msg.GetSessionStarted().GetSessionEndedAtUnixMs(); ms != now.UnixMilli() {
				t.Fatalf("session_ended_at_unix_ms = %d, want %d", ms, now.UnixMilli())
			}
		}
		if c := msg.GetCancelled(); c != nil {
			cancelled = true
			if c.GetSessionEndedAtUnixMs() != now.UnixMilli() {
				t.Fatalf("cancelled session_ended_at_unix_ms = %d, want %d", c.GetSessionEndedAtUnixMs(), now.UnixMilli())
			}
		}
	}
	if !started || !cancelled {
		t.Fatalf("started=%v cancelled=%v", started, cancelled)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestRuntime_RunSessionInteractive_attachFailedRejectsUserMessage(t *testing.T) {
	db, mock := testSQLxDB(t)
	now := time.Now()
	errMsg := "model unavailable"
	mock.ExpectQuery(`FROM sessions`).
		WithArgs("sess-1").
		WillReturnRows(sessionMockRows("sess-1", "version-uuid", model.SessionStatusFailed, []byte(`{}`), &errMsg, "sess-1", 1, now, now))
	expectAttachSessionFoldQueries(mock, "sess-1", nil, now)

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
		loadSessionVersionFn: func(context.Context, *store.Queries, string, string) (*executor.Version, error) {
			return executor.NewVersionWithProvider("version-uuid", &manifest.Agent{
				Spec: manifest.AgentSpec{Model: manifest.ModelConfig{Provider: provider.IDAnthropic, Name: "m"}},
			}, providertest.DeltaCompleted()), nil
		},
	}
	err := srv.RunSessionInteractive(stream)
	assertGRPCCode(t, err, codes.InvalidArgument)
}

func TestRuntime_RunSessionInteractive_attachRunningReplaysLiveAssistant(t *testing.T) {
	db, mock := testSQLxDB(t)
	now := time.Now()
	events := foldEventsWithMessages("sess-1", "hi", "", "end_turn", provider.TokenUsage{})
	events = events[:1]
	mock.ExpectQuery(`FROM sessions`).WithArgs("sess-1").
		WillReturnRows(sessionMockRows("sess-1", "version-uuid", model.SessionStatusRunning, []byte(`{"message":"hi"}`), nil, "sess-1", 1, now, now))
	expectAttachSessionFoldQueries(mock, "sess-1", events, now)

	hub := newSessionEventHub()
	inputMux := newSessionInputMux(context.Background())
	entry := activeSessionEntry{
		cancel:        func() {},
		eventHub:      hub,
		inputMux:      inputMux,
		liveAssistant: "partial stream text",
	}
	srv := &runtimeServer{
		db: db,
		loadSessionVersionFn: func(context.Context, *store.Queries, string, string) (*executor.Version, error) {
			return executor.NewVersionWithProvider("version-uuid", &manifest.Agent{
				Spec: manifest.AgentSpec{Model: manifest.ModelConfig{Provider: provider.IDAnthropic, Name: "m"}},
			}, providertest.DeltaCompleted()), nil
		},
	}
	if err := srv.registerActiveSession("sess-1", entry); err != nil {
		t.Fatalf("registerActiveSession: %v", err)
	}
	defer srv.unregisterActiveSession("sess-1")

	attachCtx, detach := context.WithCancel(context.Background())
	defer detach()
	stream := &blockingAfterStartStream{mockInteractiveStream: &mockInteractiveStream{
		ctx: attachCtx,
		recv: []*runtimev1.RunSessionInteractiveClientMsg{
			{Body: &runtimev1.RunSessionInteractiveClientMsg_Start{
				Start: &runtimev1.RunSessionInteractiveStart{SessionId: "sess-1"},
			}},
		},
	}}
	done := make(chan error, 1)
	go func() { done <- srv.RunSessionInteractive(stream) }()
	deadline := time.After(2 * time.Second)
	var replayDelta string
	for replayDelta == "" {
		for _, msg := range stream.sent {
			if d := msg.GetTextDelta(); d != nil {
				replayDelta = d.GetDelta()
			}
		}
		if replayDelta != "" {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("sent = %+v, want live replay text_delta", stream.sent)
		case err := <-done:
			t.Fatalf("attach ended early: %v", err)
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}
	detach()
	<-done
	if replayDelta != "partial stream text" {
		t.Fatalf("replay delta = %q, want partial stream text", replayDelta)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestRuntime_RunSessionInteractive_attachRunningSubscribesToActiveDriver(t *testing.T) {
	db, mock := testSQLxDB(t)
	now := time.Now()
	mock.ExpectQuery(`FROM sessions`).
		WithArgs("sess-1").
		WillReturnRows(sessionMockRows("sess-1", "version-uuid", model.SessionStatusRunning, []byte(`{}`), nil, "sess-1", 0, now, now))
	expectAttachSessionFoldQueries(mock, "sess-1", nil, now)

	driverCtx, driverCancel := context.WithCancel(context.Background())
	defer driverCancel()
	hub := newSessionEventHub()
	inputMux := newSessionInputMux(driverCtx)
	driverCancelled := false
	entryCancel := func() { driverCancelled = true; driverCancel() }

	attachCtx, detach := context.WithCancel(context.Background())
	defer detach()
	stream := &blockingAfterStartStream{mockInteractiveStream: &mockInteractiveStream{
		ctx: attachCtx,
		recv: []*runtimev1.RunSessionInteractiveClientMsg{
			{Body: &runtimev1.RunSessionInteractiveClientMsg_Start{
				Start: &runtimev1.RunSessionInteractiveStart{SessionId: "sess-1"},
			}},
		},
	}}

	srv := &runtimeServer{
		db: db,
		loadSessionVersionFn: func(context.Context, *store.Queries, string, string) (*executor.Version, error) {
			return executor.NewVersionWithProvider("version-uuid", &manifest.Agent{
				Spec: manifest.AgentSpec{Model: manifest.ModelConfig{Provider: provider.IDAnthropic, Name: "m"}},
			}, providertest.DeltaCompleted()), nil
		},
	}
	if err := srv.registerActiveSession("sess-1", activeSessionEntry{
		cancel:   entryCancel,
		eventHub: hub,
		inputMux: inputMux,
	}); err != nil {
		t.Fatalf("registerActiveSession: %v", err)
	}
	defer srv.unregisterActiveSession("sess-1")

	attachDone := make(chan error, 1)
	go func() {
		attachDone <- srv.RunSessionInteractive(stream)
	}()

	deadline := time.After(2 * time.Second)
	for stream.sent == nil {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for session_started")
		case err := <-attachDone:
			t.Fatalf("attach ended early: %v", err)
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}
	if stream.sent[0].GetSessionStarted() == nil {
		t.Fatalf("first message = %T, want session_started", stream.sent[0].GetBody())
	}

	hub.Send(&runtimev1.RunSessionInteractiveServerMsg{
		Body: &runtimev1.RunSessionInteractiveServerMsg_TextDelta{
			TextDelta: &runtimev1.RunSessionInteractiveTextDelta{Delta: "live"},
		},
	})

	sawDelta := false
	deltaDeadline := time.After(2 * time.Second)
	for !sawDelta {
		for _, msg := range stream.sent {
			if msg.GetTextDelta() != nil && msg.GetTextDelta().GetDelta() == "live" {
				sawDelta = true
				break
			}
		}
		if sawDelta {
			break
		}
		select {
		case <-deltaDeadline:
			t.Fatalf("sent = %+v, want text_delta from hub", stream.sent)
		case err := <-attachDone:
			t.Fatalf("attach ended before hub event: %v", err)
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}

	detach()
	if err := <-attachDone; err != nil {
		t.Fatalf("RunSessionInteractive: %v", err)
	}
	if driverCancelled {
		t.Fatal("detach must not cancel the background driver")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestRuntime_RunSessionInteractive_attachRunningFanOutToMultipleSubscribers(t *testing.T) {
	db, mock := testSQLxDB(t)
	mock.MatchExpectationsInOrder(false)
	now := time.Now()
	for range 2 {
		mock.ExpectQuery(`FROM sessions`).WithArgs("sess-1").
			WillReturnRows(sessionMockRows("sess-1", "version-uuid", model.SessionStatusRunning, []byte(`{}`), nil, "sess-1", 0, now, now))
		expectAttachSessionFoldQueries(mock, "sess-1", nil, now)
	}

	driverCtx, driverCancel := context.WithCancel(context.Background())
	defer driverCancel()
	hub := newSessionEventHub()
	inputMux := newSessionInputMux(driverCtx)

	srv := &runtimeServer{
		db: db,
		loadSessionVersionFn: func(context.Context, *store.Queries, string, string) (*executor.Version, error) {
			return executor.NewVersionWithProvider("version-uuid", &manifest.Agent{
				Spec: manifest.AgentSpec{Model: manifest.ModelConfig{Provider: provider.IDAnthropic, Name: "m"}},
			}, providertest.DeltaCompleted()), nil
		},
	}
	if err := srv.registerActiveSession("sess-1", activeSessionEntry{
		cancel: driverCancel, eventHub: hub, inputMux: inputMux,
	}); err != nil {
		t.Fatalf("registerActiveSession: %v", err)
	}
	defer srv.unregisterActiveSession("sess-1")

	startAttach := func() (*blockingAfterStartStream, chan error) {
		ctx, cancel := context.WithCancel(context.Background())
		t.Cleanup(cancel)
		stream := &blockingAfterStartStream{mockInteractiveStream: &mockInteractiveStream{
			ctx: ctx,
			recv: []*runtimev1.RunSessionInteractiveClientMsg{
				{Body: &runtimev1.RunSessionInteractiveClientMsg_Start{
					Start: &runtimev1.RunSessionInteractiveStart{SessionId: "sess-1"},
				}},
			},
		}}
		done := make(chan error, 1)
		go func() { done <- srv.RunSessionInteractive(stream) }()
		deadline := time.After(2 * time.Second)
		for stream.sent == nil {
			select {
			case <-deadline:
				t.Fatal("timed out waiting for session_started")
			case err := <-done:
				t.Fatalf("attach ended early: %v", err)
			default:
				time.Sleep(5 * time.Millisecond)
			}
		}
		return stream, done
	}

	stream1, done1 := startAttach()
	stream2, done2 := startAttach()

	hub.Send(&runtimev1.RunSessionInteractiveServerMsg{
		Body: &runtimev1.RunSessionInteractiveServerMsg_TextDelta{
			TextDelta: &runtimev1.RunSessionInteractiveTextDelta{Delta: "live"},
		},
	})

	waitDelta := func(stream *blockingAfterStartStream, done chan error) {
		t.Helper()
		deadline := time.After(2 * time.Second)
		for {
			for _, msg := range stream.sent {
				if msg.GetTextDelta() != nil && msg.GetTextDelta().GetDelta() == "live" {
					return
				}
			}
			select {
			case <-deadline:
				t.Fatalf("sent = %+v, want text_delta from hub", stream.sent)
			case err := <-done:
				t.Fatalf("attach ended before hub event: %v", err)
			default:
				time.Sleep(5 * time.Millisecond)
			}
		}
	}
	waitDelta(stream1, done1)
	waitDelta(stream2, done2)

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestRuntime_RunSessionInteractive_detachThenReattachReceivesHubEvents(t *testing.T) {
	db, mock := testSQLxDB(t)
	mock.MatchExpectationsInOrder(false)
	now := time.Now()
	for range 2 {
		mock.ExpectQuery(`FROM sessions`).WithArgs("sess-1").
			WillReturnRows(sessionMockRows("sess-1", "version-uuid", model.SessionStatusRunning, []byte(`{}`), nil, "sess-1", 0, now, now))
		expectAttachSessionFoldQueries(mock, "sess-1", nil, now)
	}

	driverCtx, driverCancel := context.WithCancel(context.Background())
	defer driverCancel()
	hub := newSessionEventHub()
	inputMux := newSessionInputMux(driverCtx)
	driverCancelled := false
	entryCancel := func() { driverCancelled = true; driverCancel() }

	srv := &runtimeServer{
		db: db,
		loadSessionVersionFn: func(context.Context, *store.Queries, string, string) (*executor.Version, error) {
			return executor.NewVersionWithProvider("version-uuid", &manifest.Agent{
				Spec: manifest.AgentSpec{Model: manifest.ModelConfig{Provider: provider.IDAnthropic, Name: "m"}},
			}, providertest.DeltaCompleted()), nil
		},
	}
	if err := srv.registerActiveSession("sess-1", activeSessionEntry{
		cancel: entryCancel, eventHub: hub, inputMux: inputMux,
	}); err != nil {
		t.Fatalf("registerActiveSession: %v", err)
	}
	defer srv.unregisterActiveSession("sess-1")

	startAttach := func() (*blockingAfterStartStream, context.CancelFunc, chan error) {
		attachCtx, detach := context.WithCancel(context.Background())
		stream := &blockingAfterStartStream{mockInteractiveStream: &mockInteractiveStream{
			ctx: attachCtx,
			recv: []*runtimev1.RunSessionInteractiveClientMsg{
				{Body: &runtimev1.RunSessionInteractiveClientMsg_Start{
					Start: &runtimev1.RunSessionInteractiveStart{SessionId: "sess-1"},
				}},
			},
		}}
		done := make(chan error, 1)
		go func() { done <- srv.RunSessionInteractive(stream) }()
		deadline := time.After(2 * time.Second)
		for stream.sent == nil {
			select {
			case <-deadline:
				t.Fatal("timed out waiting for session_started")
			case err := <-done:
				t.Fatalf("attach ended early: %v", err)
			default:
				time.Sleep(5 * time.Millisecond)
			}
		}
		if stream.sent[0].GetSessionStarted() == nil {
			t.Fatalf("first message = %T, want session_started", stream.sent[0].GetBody())
		}
		return stream, detach, done
	}

	waitHubDelta := func(stream *blockingAfterStartStream, done chan error) {
		t.Helper()
		deadline := time.After(2 * time.Second)
		for {
			for _, msg := range stream.sent {
				if msg.GetTextDelta() != nil && msg.GetTextDelta().GetDelta() == "reattach" {
					return
				}
			}
			select {
			case <-deadline:
				t.Fatalf("sent = %+v, want text_delta reattach", stream.sent)
			case err := <-done:
				t.Fatalf("attach ended before hub event: %v", err)
			default:
				time.Sleep(5 * time.Millisecond)
			}
		}
	}

	_, detach1, done1 := startAttach()
	detach1()
	if err := <-done1; err != nil {
		t.Fatalf("first attach: %v", err)
	}
	if driverCancelled {
		t.Fatal("detach must not cancel the background driver")
	}
	if _, ok := srv.activeSessionEntryFor("sess-1"); !ok {
		t.Fatal("driver should remain registered after detach")
	}

	stream2, detach2, done2 := startAttach()
	hub.Send(&runtimev1.RunSessionInteractiveServerMsg{
		Body: &runtimev1.RunSessionInteractiveServerMsg_TextDelta{
			TextDelta: &runtimev1.RunSessionInteractiveTextDelta{Delta: "reattach"},
		},
	})
	waitHubDelta(stream2, done2)
	detach2()
	if err := <-done2; err != nil {
		t.Fatalf("second attach: %v", err)
	}
	if driverCancelled {
		t.Fatal("second detach must not cancel the background driver")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestRuntime_RunSessionInteractive_attachDetachDeliversInboundToDriver(t *testing.T) {
	db, mock := testSQLxDB(t)
	now := time.Now()
	events := foldEventsWithMessages("sess-1", "hello", "hi", "end_turn", provider.TokenUsage{})
	mock.ExpectQuery(`FROM sessions`).
		WithArgs("sess-1").
		WillReturnRows(sessionMockRows("sess-1", "version-uuid", model.SessionStatusAwaitingInput, []byte(`{"message":"hello"}`), nil, "sess-1", len(events), now, now))
	expectAttachSessionFoldQueries(mock, "sess-1", events, now)

	driverCtx, driverCancel := context.WithCancel(context.Background())
	defer driverCancel()
	inputMux := newSessionInputMux(driverCtx)
	hub := newSessionEventHub()

	recvDone := make(chan struct{})
	go func() {
		msg, err := inputMux.Recv()
		if err != nil {
			t.Errorf("inputMux Recv: %v", err)
			close(recvDone)
			return
		}
		if msg.GetUserMessage().GetText() != "follow-up" {
			t.Errorf("text = %q, want follow-up", msg.GetUserMessage().GetText())
		}
		close(recvDone)
	}()

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
		loadSessionVersionFn: func(context.Context, *store.Queries, string, string) (*executor.Version, error) {
			return executor.NewVersionWithProvider("version-uuid", &manifest.Agent{
				Spec: manifest.AgentSpec{Model: manifest.ModelConfig{Provider: provider.IDAnthropic, Name: "m"}},
			}, providertest.DeltaCompleted()), nil
		},
	}
	if err := srv.registerActiveSession("sess-1", activeSessionEntry{
		cancel:   driverCancel,
		eventHub: hub,
		inputMux: inputMux,
	}); err != nil {
		t.Fatalf("registerActiveSession: %v", err)
	}
	defer srv.unregisterActiveSession("sess-1")

	if err := srv.RunSessionInteractive(stream); err != nil {
		t.Fatalf("RunSessionInteractive: %v", err)
	}

	select {
	case <-recvDone:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for user_message on input mux")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestRuntime_RunSessionInteractive_invalidInput(t *testing.T) {
	db, mock := testSQLxDB(t)
	expectActiveDeployment(mock, "demo", "echo-agent", "version-uuid", "1.2.0")

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

func Test_sessionEndedAtForAttach(t *testing.T) {
	now := time.Now()
	limitErr := "run limit max_wall_clock_seconds exceeded (on_limit=halt)"
	tokenErr := "run limit max_tokens_per_run exceeded (on_limit=halt)"
	otherErr := "model unavailable"

	tests := []struct {
		name    string
		session store.Session
		wantEnd bool
	}{
		{
			name: "completed",
			session: store.Session{Status: model.SessionStatusCompleted, UpdatedAt: now},
			wantEnd: true,
		},
		{
			name: "cancelled",
			session: store.Session{Status: model.SessionStatusCancelled, UpdatedAt: now},
			wantEnd: true,
		},
		{
			name: "failed wall clock",
			session: store.Session{Status: model.SessionStatusFailed, UpdatedAt: now, Error: &limitErr},
			wantEnd: true,
		},
		{
			name: "failed resumable token limit",
			session: store.Session{Status: model.SessionStatusFailed, UpdatedAt: now, Error: &tokenErr},
			wantEnd: false,
		},
		{
			name: "failed other",
			session: store.Session{Status: model.SessionStatusFailed, UpdatedAt: now, Error: &otherErr},
			wantEnd: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := sessionEndedAtForAttach(&tc.session)
			if tc.wantEnd {
				if got == nil || !got.Equal(now) {
					t.Fatalf("sessionEndedAtForAttach() = %v, want %v", got, now)
				}
			} else if got != nil {
				t.Fatalf("sessionEndedAtForAttach() = %v, want nil", got)
			}
		})
	}
}
