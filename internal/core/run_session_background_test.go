package core

import (
	"context"
	"encoding/json"
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
)

func TestRuntime_RunSessionBackground_hubDeliversEventsToSubscriber(t *testing.T) {
	db, mock := testSQLxDB(t)
	now := time.Now()
	mock.ExpectQuery(`UPDATE sessions`).
		WithArgs("sess-bg", model.SessionStatusAwaitingInput, sqlmock.AnyArg(), nil, sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"updated_at"}).AddRow(now))

	agent := &manifest.Agent{
		Spec: manifest.AgentSpec{
			Instructions: manifest.InstructionsSpec{Text: "System."},
			Model:        manifest.ModelConfig{Provider: provider.IDAnthropic, Name: "m"},
		},
	}
	hub := newSessionEventHub()
	events, unsub := hub.Subscribe()
	defer unsub()

	srv := &runtimeServer{
		db: db,
		loadSessionVersionFn: func(context.Context, *store.Queries, string, string) (*executor.Version, error) {
			return executor.NewVersionWithProvider("version-uuid", agent, providertest.DeltaCompleted()), nil
		},
	}
	driverCtx, cancel := context.WithCancel(context.Background())
	inputMux := newSessionInputMux(driverCtx)

	done := make(chan struct{})
	go func() {
		defer close(done)
		srv.runSessionBackground(driverCtx, "sess-bg", "version-uuid", []byte(`{"message":"hi"}`), hub, inputMux)
	}()

	select {
	case msg := <-events:
		if msg.GetTextDelta() == nil {
			t.Fatalf("first hub event = %T, want text_delta", msg.GetBody())
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for hub event from background driver")
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		cancel()
		<-done
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestRuntime_RunSessionBackground_reachesAwaitingInput(t *testing.T) {
	db, mock := testSQLxDB(t)
	now := time.Now()
	mock.ExpectQuery(`UPDATE sessions`).
		WithArgs("sess-bg", model.SessionStatusAwaitingInput, sqlmock.AnyArg(), nil, sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"updated_at"}).AddRow(now))

	agent := &manifest.Agent{
		Spec: manifest.AgentSpec{
			Instructions: manifest.InstructionsSpec{Text: "System."},
			Model:        manifest.ModelConfig{Provider: provider.IDAnthropic, Name: "m"},
		},
	}
	srv := &runtimeServer{
		db: db,
		loadSessionVersionFn: func(context.Context, *store.Queries, string, string) (*executor.Version, error) {
			return executor.NewVersionWithProvider("version-uuid", agent, providertest.DeltaCompleted()), nil
		},
	}
	driverCtx, cancel := context.WithCancel(context.Background())
	events := newSessionEventHub()
	inputMux := newSessionInputMux(driverCtx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		srv.runSessionBackground(driverCtx, "sess-bg", "version-uuid", []byte(`{"message":"hi"}`), events, inputMux)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		cancel()
		<-done
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestRuntime_RunSessionBackground_loadFailureMarksFailed(t *testing.T) {
	db, mock := testSQLxDB(t)
	mock.ExpectQuery(`UPDATE sessions`).
		WithArgs("sess-bg", model.SessionStatusFailed, nil, context.Canceled.Error(), nil).
		WillReturnRows(sqlmock.NewRows([]string{"updated_at"}).AddRow(time.Now()))

	srv := &runtimeServer{
		db: db,
		loadSessionVersionFn: func(context.Context, *store.Queries, string, string) (*executor.Version, error) {
			return nil, context.Canceled
		},
	}
	driverCtx, cancel := context.WithCancel(context.Background())
	events := newSessionEventHub()
	inputMux := newSessionInputMux(driverCtx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		srv.runSessionBackground(driverCtx, "sess-bg", "version-uuid", []byte(`{"message":"hi"}`), events, inputMux)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		cancel()
		<-done
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestRuntime_RunSessionBackground_registersActiveSession(t *testing.T) {
	db, mock := testSQLxDB(t)
	done := make(chan struct{})

	agent := &manifest.Agent{
		Spec: manifest.AgentSpec{
			Instructions: manifest.InstructionsSpec{Text: "System."},
			Model:        manifest.ModelConfig{Provider: provider.IDAnthropic, Name: "m"},
		},
	}
	var wg sync.WaitGroup
	srv := &runtimeServer{db: db, activeSessions: &sync.Map{}}
	srv.loadSessionVersionFn = func(context.Context, *store.Queries, string, string) (*executor.Version, error) {
		return executor.NewVersionWithProvider("version-uuid", agent, providertest.DeltaCompleted()), nil
	}
	srv.startRunSessionBackgroundFn = func(sessionID, agentVersionID string, inputJSON json.RawMessage) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sessionCtx, sessionCancel := context.WithCancel(context.Background())
			eventHub := newSessionEventHub()
			inputMux := newSessionInputMux(sessionCtx)
			if err := srv.registerActiveSession(sessionID, activeSessionEntry{
				cancel: sessionCancel, eventHub: eventHub, inputMux: inputMux,
			}); err != nil {
				sessionCancel()
				close(done)
				return
			}
			defer func() {
				sessionCancel()
				inputMux.close()
				srv.unregisterActiveSession(sessionID)
				close(done)
			}()
			srv.runSessionBackground(sessionCtx, sessionID, agentVersionID, inputJSON, eventHub, inputMux)
		}()
	}

	expectActiveDeployment(mock, "demo", "echo-agent", "version-uuid", "1.2.0")
	expectCreateRunSessionMocks(mock, "version-uuid", []byte(`{"message":"hi"}`))
	mock.ExpectQuery(`UPDATE sessions`).
		WithArgs(sqlmock.AnyArg(), model.SessionStatusAwaitingInput, sqlmock.AnyArg(), nil, sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"updated_at"}).AddRow(time.Now())).
		WillDelayFor(50 * time.Millisecond)

	resp, err := srv.RunSession(context.Background(), &runtimev1.RunSessionRequest{
		AgentRef: &runtimev1.AgentRef{Namespace: "demo", Name: "echo-agent"},
		Input:    []byte(`{"message":"hi"}`),
	})
	if err != nil {
		t.Fatalf("RunSession: %v", err)
	}
	if resp.GetStatus() != model.SessionStatusRunning {
		t.Fatalf("status = %q, want running", resp.GetStatus())
	}

	active := false
	deadline := time.After(2 * time.Second)
	for !active {
		if _, ok := srv.activeSessions.Load(resp.GetSessionId()); ok {
			active = true
			break
		}
		select {
		case <-done:
			t.Fatal("background finished before active session was observed")
		case <-deadline:
			t.Fatal("timed out waiting for active session registration")
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}

	if v, ok := srv.activeSessions.Load(resp.GetSessionId()); ok {
		entry, _ := v.(activeSessionEntry)
		entry.cancel()
	}
	wg.Wait()
	<-done
	if _, ok := srv.activeSessions.Load(resp.GetSessionId()); ok {
		t.Fatal("session still active after driver stopped")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestRuntime_expireWallClockSession_marksParkedSessionFailed(t *testing.T) {
	db, mock := testSQLxDB(t)
	now := time.Now()
	mock.ExpectQuery(`FROM sessions`).
		WithArgs("sess-bg").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "agent_version_id", "input", "status", "output", "error", "history", "created_at", "updated_at",
		}).AddRow("sess-bg", "version-uuid", []byte(`{}`), model.SessionStatusAwaitingInput, nil, nil, []byte(`[]`), now.Add(-time.Hour), now))
	mock.ExpectQuery(`UPDATE sessions`).
		WithArgs("sess-bg", model.SessionStatusFailed, nil, sqlmock.AnyArg(), nil).
		WillReturnRows(sqlmock.NewRows([]string{"updated_at"}).AddRow(now))
	mock.ExpectExec(`DELETE FROM session_secrets`).
		WithArgs("sess-bg").
		WillReturnResult(sqlmock.NewResult(0, 0))

	srv := &runtimeServer{db: db, activeSessions: &sync.Map{}}
	srv.expireWallClockSession("sess-bg", "halt")

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestRuntime_expireWallClockSession_skipsActiveSession(t *testing.T) {
	db, _ := testSQLxDB(t)
	srv := &runtimeServer{db: db, activeSessions: &sync.Map{}}
	if err := srv.registerActiveSession("sess-bg", activeSessionEntry{cancel: func() {}}); err != nil {
		t.Fatalf("registerActiveSession: %v", err)
	}
	// No SQL expectations: an attached session is left to the live stream.
	srv.expireWallClockSession("sess-bg", "halt")
}

func TestRuntime_expireWallClockSession_skipsTerminalSession(t *testing.T) {
	db, mock := testSQLxDB(t)
	now := time.Now()
	mock.ExpectQuery(`FROM sessions`).
		WithArgs("sess-bg").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "agent_version_id", "input", "status", "output", "error", "history", "created_at", "updated_at",
		}).AddRow("sess-bg", "version-uuid", []byte(`{}`), model.SessionStatusCompleted, nil, nil, []byte(`[]`), now.Add(-time.Hour), now))

	srv := &runtimeServer{db: db, activeSessions: &sync.Map{}}
	srv.expireWallClockSession("sess-bg", "halt")

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestRuntime_runSessionInteractiveLoop_waitForUserFalseStopsAfterAwaitingInput(t *testing.T) {
	db, mock := testSQLxDB(t)
	now := time.Now()
	mock.ExpectQuery(`UPDATE sessions`).
		WithArgs("sess-1", model.SessionStatusAwaitingInput, sqlmock.AnyArg(), nil, sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"updated_at"}).AddRow(now))

	agent := &manifest.Agent{
		Spec: manifest.AgentSpec{
			Instructions: manifest.InstructionsSpec{Text: "System."},
			Model:        manifest.ModelConfig{Provider: provider.IDAnthropic, Name: "m"},
		},
	}
	stream := &mockInteractiveStream{ctx: context.Background()}
	srv := &runtimeServer{
		db: db,
		loadSessionVersionFn: func(context.Context, *store.Queries, string, string) (*executor.Version, error) {
			return executor.NewVersionWithProvider("version-uuid", agent, providertest.DeltaCompleted()), nil
		},
	}
	state := &interactiveSessionState{
		sessionID:        "sess-1",
		version:          executor.NewVersionWithProvider("version-uuid", agent, providertest.DeltaCompleted()),
		sessionStartedAt: now,
	}
	events := sessionEventsFromStream(stream)
	if err := srv.runSessionInteractiveLoop(context.Background(), stream, events, store.New(db), "sess-1", state, []byte(`{"message":"hi"}`), false); err != nil {
		t.Fatalf("runSessionInteractiveLoop: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestRuntime_runSessionInteractiveLoop_waitForUserFalseDoesNotAutoCompleteOnEOF(t *testing.T) {
	db, mock := testSQLxDB(t)
	now := time.Now()
	mock.ExpectQuery(`UPDATE sessions`).
		WithArgs("sess-1", model.SessionStatusAwaitingInput, sqlmock.AnyArg(), nil, sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"updated_at"}).AddRow(now))

	agent := &manifest.Agent{
		Spec: manifest.AgentSpec{
			Instructions: manifest.InstructionsSpec{Text: "System."},
			Model:        manifest.ModelConfig{Provider: provider.IDAnthropic, Name: "m"},
		},
	}
	stream := &mockInteractiveStream{ctx: context.Background()}
	srv := &runtimeServer{
		db: db,
		loadSessionVersionFn: func(context.Context, *store.Queries, string, string) (*executor.Version, error) {
			return executor.NewVersionWithProvider("version-uuid", agent, providertest.DeltaCompleted()), nil
		},
	}
	state := &interactiveSessionState{
		sessionID:        "sess-1",
		version:          executor.NewVersionWithProvider("version-uuid", agent, providertest.DeltaCompleted()),
		sessionStartedAt: now,
	}
	events := sessionEventsFromStream(stream)
	if err := srv.runSessionInteractiveLoop(context.Background(), stream, events, store.New(db), "sess-1", state, []byte(`{"message":"hi"}`), false); err != nil {
		t.Fatalf("runSessionInteractiveLoop: %v", err)
	}
	for _, msg := range stream.sent {
		if msg.GetCompleted() != nil {
			t.Fatal("waitForUser=false should not emit completed on stream close")
		}
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}
