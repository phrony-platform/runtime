package core

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
	"github.com/phrony-platform/runtime/internal/model"
	"github.com/phrony-platform/runtime/internal/store"
)

func completedSessionRows(sessionID string, _ []byte, now time.Time) *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"id", "agent_version_id", "input", "status", "error", "root_session_id", "event_seq", "created_at", "updated_at",
	}).AddRow(sessionID, "version-uuid", []byte("{}"), model.SessionStatusCompleted, nil, sessionID, 1, now, now)
}

func cancelledSessionRows(sessionID string, now time.Time) *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"id", "agent_version_id", "input", "status", "error", "root_session_id", "event_seq", "created_at", "updated_at",
	}).AddRow(sessionID, "version-uuid", []byte("{}"), model.SessionStatusCancelled, nil, sessionID, 1, now, now)
}

// blockRecvOnCancelStream blocks Recv after the start message until ctx is
// cancelled, without returning EOF. That lets the driver loop observe
// context cancellation and emit out-of-band completion instead of treating
// stream close as a clean client detach.
type blockRecvOnCancelStream struct {
	*mockInteractiveStream
}

func (s *blockRecvOnCancelStream) Recv() (*runtimev1.RunSessionInteractiveClientMsg, error) {
	if s.recvIdx < len(s.recv) {
		return s.mockInteractiveStream.Recv()
	}
	<-s.ctx.Done()
	select {}
}

func expectCompleteSessionRPC(mock sqlmock.Sqlmock, sessionID string) {
	now := time.Now()
	mock.ExpectBegin()
	mock.ExpectQuery(`FROM sessions`).WithArgs(sessionID).
		WillReturnRows(sessionMockRows(sessionID, "version-uuid", model.SessionStatusRunning, []byte(`{}`), nil, sessionID, 0, now, now))
	mock.ExpectQuery(`UPDATE sessions`).WithArgs(sessionID).
		WillReturnRows(sqlmock.NewRows([]string{"event_seq"}).AddRow(1))
	mock.ExpectQuery(`INSERT INTO events`).
		WithArgs(sessionID, sessionID, 1, EventSessionCompleted, sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(1)))
	mock.ExpectQuery(`UPDATE sessions`).
		WithArgs(sessionID).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(sessionID))
	mock.ExpectCommit()
	mock.ExpectExec(`DELETE FROM session_secrets`).
		WithArgs(sessionID).
		WillReturnResult(sqlmock.NewResult(0, 0))
}

func TestRuntime_completedExternally_afterDriverContextCancelled(t *testing.T) {
	db, mock := testSQLxDB(t)
	now := time.Now()
	output := []byte(`{"message":"ok","stop_reason":"end_turn"}`)
	mock.ExpectQuery(`FROM sessions`).
		WithArgs("sess-1").
		WillReturnRows(completedSessionRows("sess-1", output, now))
	mock.ExpectQuery(`FROM events`).WithArgs("sess-1").
		WillReturnRows(sessionEventLogRows(now))

	stream := &mockInteractiveStream{ctx: context.Background()}
	srv := &runtimeServer{db: db}
	driverCtx, cancel := context.WithCancel(context.Background())
	cancel()

	done, err := srv.completedExternally(driverCtx, store.New(db), stream, "sess-1", &interactiveSessionState{})
	if err != nil {
		t.Fatalf("completedExternally: %v", err)
	}
	if !done {
		t.Fatal("expected completedExternally to emit terminal event")
	}
	if stream.sent[0].GetCompleted() == nil {
		t.Fatal("expected Completed message on event sink")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestRuntime_runSessionInteractiveLoop_emitsCompletedOnDriverCancel(t *testing.T) {
	db, mock := testSQLxDB(t)
	now := time.Now()
	output := []byte(`{"message":"ok","stop_reason":"end_turn"}`)
	for i := 0; i < 2; i++ {
		mock.ExpectQuery(`FROM sessions`).
			WithArgs("sess-1").
			WillReturnRows(completedSessionRows("sess-1", output, now))
	}
	mock.ExpectQuery(`FROM events`).WithArgs("sess-1").
		WillReturnRows(sessionEventLogRows(now))

	driverCtx, driverCancel := context.WithCancel(context.Background())
	stream := &blockRecvOnCancelStream{mockInteractiveStream: &mockInteractiveStream{
		ctx:  driverCtx,
		recv: []*runtimev1.RunSessionInteractiveClientMsg{},
	}}
	srv := &runtimeServer{db: db}
	state := &interactiveSessionState{
		sessionID:        "sess-1",
		sessionStartedAt: now,
	}
	events := sessionEventsFromStream(stream)

	loopDone := make(chan error, 1)
	go func() {
		loopDone <- srv.runSessionInteractiveLoop(driverCtx, stream, events, store.New(db), "sess-1", state, nil, true)
	}()

	time.Sleep(30 * time.Millisecond)
	driverCancel()

	select {
	case err := <-loopDone:
		if err != nil {
			t.Fatalf("runSessionInteractiveLoop: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for loop shutdown")
	}

	if !interactiveStreamHasKind(stream.sent, "completed") {
		t.Fatalf("sent = %+v, want completed", interactiveStreamStepKinds(stream.sent))
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestRuntime_runSessionInteractiveLoop_emitsCancelledOnDriverCancel(t *testing.T) {
	db, mock := testSQLxDB(t)
	now := time.Now()
	mock.ExpectQuery(`FROM sessions`).
		WithArgs("sess-1").
		WillReturnRows(cancelledSessionRows("sess-1", now))
	mock.ExpectQuery(`FROM sessions`).
		WithArgs("sess-1").
		WillReturnRows(cancelledSessionRows("sess-1", now))

	driverCtx, driverCancel := context.WithCancel(context.Background())
	stream := &blockingAfterStartStream{mockInteractiveStream: &mockInteractiveStream{
		ctx:  driverCtx,
		recv: []*runtimev1.RunSessionInteractiveClientMsg{},
	}}
	srv := &runtimeServer{db: db}
	state := &interactiveSessionState{
		sessionID:        "sess-1",
		sessionStartedAt: now,
	}
	events := sessionEventsFromStream(stream)

	loopDone := make(chan error, 1)
	go func() {
		loopDone <- srv.runSessionInteractiveLoop(driverCtx, stream, events, store.New(db), "sess-1", state, nil, true)
	}()

	time.Sleep(30 * time.Millisecond)
	driverCancel()

	select {
	case err := <-loopDone:
		if err != nil {
			t.Fatalf("runSessionInteractiveLoop: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for loop shutdown")
	}

	if !interactiveStreamHasKind(stream.sent, "cancelled") {
		t.Fatalf("sent = %+v, want cancelled", interactiveStreamStepKinds(stream.sent))
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestRuntime_CompleteSession_emitsCompletedToAttachedClient(t *testing.T) {
	db, mock := testSQLxDB(t)
	now := time.Now()
	sessionID := "sess-1"
	output := []byte(`{"message":"ok","stop_reason":"end_turn"}`)

	expectCompleteSessionRPC(mock, sessionID)
	for i := 0; i < 2; i++ {
		mock.ExpectQuery(`FROM sessions`).
			WithArgs(sessionID).
			WillReturnRows(completedSessionRows(sessionID, output, now))
	}
	mock.ExpectQuery(`FROM events`).WithArgs(sessionID).
		WillReturnRows(sessionEventLogRows(now))

	driverCtx, driverCancel := context.WithCancel(context.Background())
	defer driverCancel()
	hub := newSessionEventHub()
	inputMux := newSessionInputMux(driverCtx)
	// Driver recv uses a blocking stream so inputMux.close() during CompleteSession
	// does not EOF the loop before it handles driver context cancellation.
	driverStream := &blockRecvOnCancelStream{mockInteractiveStream: &mockInteractiveStream{
		ctx:  driverCtx,
		recv: []*runtimev1.RunSessionInteractiveClientMsg{},
	}}

	srv := &runtimeServer{db: db, activeSessions: &sync.Map{}}
	if err := srv.registerActiveSession(sessionID, activeSessionEntry{
		cancel: driverCancel, eventHub: hub, inputMux: inputMux,
	}); err != nil {
		t.Fatalf("registerActiveSession: %v", err)
	}
	defer srv.unregisterActiveSession(sessionID)

	state := &interactiveSessionState{
		sessionID:        sessionID,
		sessionStartedAt: now,
	}
	loopDone := make(chan error, 1)
	go func() {
		loopDone <- srv.runSessionInteractiveLoop(driverCtx, driverStream, hub, store.New(db), sessionID, state, nil, true)
	}()

	attachCtx, attachCancel := context.WithCancel(context.Background())
	defer attachCancel()
	attachStream := &blockingAfterStartStream{mockInteractiveStream: &mockInteractiveStream{
		ctx:  attachCtx,
		recv: []*runtimev1.RunSessionInteractiveClientMsg{},
	}}
	hubEvents, unsubscribe := hub.Subscribe()
	defer unsubscribe()
	bridgeDone := make(chan error, 1)
	go func() {
		bridgeDone <- bridgeInteractiveAttachStream(attachCtx, attachStream, hubEvents, inputMux)
	}()

	time.Sleep(30 * time.Millisecond)

	if _, err := srv.CompleteSession(context.Background(), &runtimev1.CompleteSessionRequest{
		SessionId: sessionID,
	}); err != nil {
		t.Fatalf("CompleteSession: %v", err)
	}

	waitForInteractiveMessages(t, bridgeDone, func() []*runtimev1.RunSessionInteractiveServerMsg {
		return attachStream.sent
	}, func(msgs []*runtimev1.RunSessionInteractiveServerMsg) bool {
		return interactiveStreamHasKind(msgs, "completed")
	})

	select {
	case err := <-loopDone:
		if err != nil {
			t.Fatalf("runSessionInteractiveLoop: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for driver loop exit")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestRuntime_isBenignDriverLoopExit_completedSession(t *testing.T) {
	db, mock := testSQLxDB(t)
	now := time.Now()
	mock.ExpectQuery(`FROM sessions`).
		WithArgs("sess-1").
		WillReturnRows(completedSessionRows("sess-1", []byte(`{"stop_reason":"end_turn"}`), now))
	mock.ExpectQuery(`FROM sessions`).
		WithArgs("sess-1").
		WillReturnRows(completedSessionRows("sess-1", []byte(`{"stop_reason":"end_turn"}`), now))

	q := store.New(db)
	driverCtx, cancel := context.WithCancel(context.Background())
	cancel()

	if !isBenignDriverLoopExit(driverCtx, q, "sess-1", context.Canceled) {
		t.Fatal("expected completed session shutdown to be benign")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func interactiveStreamHasKind(msgs []*runtimev1.RunSessionInteractiveServerMsg, kind string) bool {
	for _, msg := range msgs {
		switch kind {
		case "completed":
			if msg.GetCompleted() != nil {
				return true
			}
		case "cancelled":
			if msg.GetCancelled() != nil {
				return true
			}
		}
	}
	return false
}
