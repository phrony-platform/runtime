package core

import (
	"context"
	"database/sql"
	"strings"
	"sync"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
	"google.golang.org/grpc/codes"
)

func TestRuntime_CancelSession_success(t *testing.T) {
	db, mock := testSQLxDB(t)
	mock.ExpectQuery(`UPDATE sessions`).
		WithArgs("run_abc").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("run_abc"))

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
	mock.ExpectQuery(`UPDATE sessions`).
		WithArgs("run_live").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("run_live"))

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
	mock.ExpectQuery(`UPDATE sessions`).
		WithArgs("run_done").
		WillReturnError(sql.ErrNoRows)

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
