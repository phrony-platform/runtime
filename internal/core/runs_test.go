package core

import (
	"context"
	"database/sql"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
	"google.golang.org/grpc/codes"
)

func expectLifecycleEventTx(mock sqlmock.Sqlmock, sessionID string) {
	now := time.Now()
	mock.ExpectBegin()
	mock.ExpectQuery(`FROM sessions`).WithArgs(sessionID).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "agent_version_id", "input", "status", "error", "root_session_id", "event_seq", "created_at", "updated_at",
		}).AddRow(sessionID, "av-1", []byte(`{}`), "running", nil, sessionID, 0, now, now))
	mock.ExpectQuery(`UPDATE sessions`).WithArgs(sessionID).
		WillReturnRows(sqlmock.NewRows([]string{"event_seq"}).AddRow(1))
	mock.ExpectQuery(`INSERT INTO events`).
		WithArgs(sessionID, sessionID, 1, sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(1)))
}

func TestRuntime_CancelSession_success(t *testing.T) {
	db, mock := testSQLxDB(t)
	expectLifecycleEventTx(mock, "run_abc")
	mock.ExpectQuery(`UPDATE sessions`).
		WithArgs("run_abc").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("run_abc"))
	mock.ExpectCommit()
	mock.ExpectExec(`DELETE FROM session_secrets`).
		WithArgs("run_abc").
		WillReturnResult(sqlmock.NewResult(0, 0))

	srv := &runtimeServer{db: db}
	_, err := srv.CancelSession(context.Background(), &runtimev1.CancelSessionRequest{
		SessionId: "run_abc",
	})
	if err != nil {
		t.Fatalf("CancelSession: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestRuntime_CancelSession_invokesActiveCancel(t *testing.T) {
	db, mock := testSQLxDB(t)
	expectLifecycleEventTx(mock, "run_live")
	mock.ExpectQuery(`UPDATE sessions`).
		WithArgs("run_live").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("run_live"))
	mock.ExpectCommit()
	mock.ExpectExec(`DELETE FROM session_secrets`).
		WithArgs("run_live").
		WillReturnResult(sqlmock.NewResult(0, 0))

	srv := &runtimeServer{db: db, activeSessions: &sync.Map{}}
	cancelled := false
	if err := srv.registerActiveSession("run_live", activeSessionEntry{cancel: func() { cancelled = true }}); err != nil {
		t.Fatalf("registerActiveSession: %v", err)
	}

	_, err := srv.CancelSession(context.Background(), &runtimev1.CancelSessionRequest{
		SessionId: "run_live",
	})
	if err != nil {
		t.Fatalf("CancelSession: %v", err)
	}
	if !cancelled {
		t.Fatal("expected active session cancel func to run")
	}
	if _, loaded := srv.activeSessions.Load("run_live"); loaded {
		t.Fatal("expected session removed from activeSessions")
	}
}

func TestRuntime_CancelSession_notFound(t *testing.T) {
	db, mock := testSQLxDB(t)
	mock.ExpectBegin()
	mock.ExpectQuery(`FROM sessions`).WithArgs("run_done").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "agent_version_id", "input", "status", "error", "root_session_id", "event_seq", "created_at", "updated_at",
		}).AddRow("run_done", "av-1", []byte(`{}`), "running", nil, "run_done", 0, time.Now(), time.Now()))
	mock.ExpectQuery(`UPDATE sessions`).WithArgs("run_done").
		WillReturnRows(sqlmock.NewRows([]string{"event_seq"}).AddRow(1))
	mock.ExpectQuery(`INSERT INTO events`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(1)))
	mock.ExpectQuery(`UPDATE sessions`).WithArgs("run_done").
		WillReturnError(sql.ErrNoRows)
	mock.ExpectRollback()

	srv := &runtimeServer{db: db}
	_, err := srv.CancelSession(context.Background(), &runtimev1.CancelSessionRequest{
		SessionId: "run_done",
	})
	assertGRPCCode(t, err, codes.NotFound)
	if !strings.Contains(statusMessage(t, err), "not found") {
		t.Fatalf("error = %v, want not found", err)
	}
}

func TestRuntime_CancelSession_missingID(t *testing.T) {
	srv := &runtimeServer{db: testServeDB(t)}
	_, err := srv.CancelSession(context.Background(), &runtimev1.CancelSessionRequest{})
	assertGRPCCode(t, err, codes.InvalidArgument)
}

func TestRuntime_CompleteSession_success(t *testing.T) {
	db, mock := testSQLxDB(t)
	expectLifecycleEventTx(mock, "run_abc")
	mock.ExpectQuery(`UPDATE sessions`).
		WithArgs("run_abc").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("run_abc"))
	mock.ExpectCommit()
	mock.ExpectExec(`DELETE FROM session_secrets`).
		WithArgs("run_abc").
		WillReturnResult(sqlmock.NewResult(0, 0))

	srv := &runtimeServer{db: db}
	_, err := srv.CompleteSession(context.Background(), &runtimev1.CompleteSessionRequest{
		SessionId: "run_abc",
	})
	if err != nil {
		t.Fatalf("CompleteSession: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestRuntime_CompleteSession_invokesActiveCancel(t *testing.T) {
	db, mock := testSQLxDB(t)
	expectLifecycleEventTx(mock, "run_live")
	mock.ExpectQuery(`UPDATE sessions`).
		WithArgs("run_live").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("run_live"))
	mock.ExpectCommit()
	mock.ExpectExec(`DELETE FROM session_secrets`).
		WithArgs("run_live").
		WillReturnResult(sqlmock.NewResult(0, 0))

	srv := &runtimeServer{db: db, activeSessions: &sync.Map{}}
	stopped := false
	if err := srv.registerActiveSession("run_live", activeSessionEntry{cancel: func() { stopped = true }}); err != nil {
		t.Fatalf("registerActiveSession: %v", err)
	}

	_, err := srv.CompleteSession(context.Background(), &runtimev1.CompleteSessionRequest{
		SessionId: "run_live",
	})
	if err != nil {
		t.Fatalf("CompleteSession: %v", err)
	}
	if !stopped {
		t.Fatal("expected active session cancel func to run")
	}
	if _, loaded := srv.activeSessions.Load("run_live"); loaded {
		t.Fatal("expected session removed from activeSessions")
	}
}

func TestRuntime_CompleteSession_notFound(t *testing.T) {
	db, mock := testSQLxDB(t)
	mock.ExpectBegin()
	mock.ExpectQuery(`FROM sessions`).WithArgs("run_done").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "agent_version_id", "input", "status", "error", "root_session_id", "event_seq", "created_at", "updated_at",
		}).AddRow("run_done", "av-1", []byte(`{}`), "running", nil, "run_done", 0, time.Now(), time.Now()))
	mock.ExpectQuery(`UPDATE sessions`).WithArgs("run_done").
		WillReturnRows(sqlmock.NewRows([]string{"event_seq"}).AddRow(1))
	mock.ExpectQuery(`INSERT INTO events`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(1)))
	mock.ExpectQuery(`UPDATE sessions`).WithArgs("run_done").
		WillReturnError(sql.ErrNoRows)
	mock.ExpectRollback()

	srv := &runtimeServer{db: db}
	_, err := srv.CompleteSession(context.Background(), &runtimev1.CompleteSessionRequest{
		SessionId: "run_done",
	})
	assertGRPCCode(t, err, codes.NotFound)
	if !strings.Contains(statusMessage(t, err), "not found") {
		t.Fatalf("error = %v, want not found", err)
	}
}

func TestRuntime_CompleteSession_missingID(t *testing.T) {
	srv := &runtimeServer{db: testServeDB(t)}
	_, err := srv.CompleteSession(context.Background(), &runtimev1.CompleteSessionRequest{})
	assertGRPCCode(t, err, codes.InvalidArgument)
}
