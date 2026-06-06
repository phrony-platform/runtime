package core

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/phrony-platform/runtime/internal/executor"
	"github.com/phrony-platform/runtime/internal/manifest"
	"github.com/phrony-platform/runtime/internal/model"
	"github.com/phrony-platform/runtime/internal/provider"
	"github.com/phrony-platform/runtime/internal/providertest"
	"github.com/phrony-platform/runtime/internal/store"
)

func TestRuntime_reconcileSessionsOnStartup_pendingStartsBackgroundDriver(t *testing.T) {
	db, mock := testSQLxDB(t)
	now := time.Now()
	mock.ExpectQuery(`FROM sessions`).
		WillReturnRows(sessionMockRows("sess-pending", "version-uuid", model.SessionStatusPending, []byte(`{"message":"hi"}`), nil, "sess-pending", 0, now, now))
	mock.ExpectExec(`DELETE FROM session_secrets`).
		WillReturnResult(sqlmock.NewResult(0, 0))

	var mu sync.Mutex
	var started []string
	srv := &runtimeServer{
		db:             db,
		activeSessions: &sync.Map{},
		loadSessionVersionFn: func(context.Context, *store.Queries, string, string) (*executor.Version, error) {
			return executor.NewVersionWithProvider("version-uuid", &manifest.Agent{
				Spec: manifest.AgentSpec{Model: manifest.ModelConfig{Provider: provider.IDAnthropic, Name: "m"}},
			}, providertest.DeltaCompleted()), nil
		},
		startRunSessionBackgroundFn: func(sessionID, agentVersionID string, _ json.RawMessage) {
			mu.Lock()
			started = append(started, sessionID)
			mu.Unlock()
		},
	}

	srv.reconcileSessionsOnStartup(context.Background())

	mu.Lock()
	got := append([]string(nil), started...)
	mu.Unlock()
	if len(got) != 1 || got[0] != "sess-pending" {
		t.Fatalf("started = %v, want [sess-pending]", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestRuntime_purgeOrphanedTerminalSessionSecrets(t *testing.T) {
	db, mock := testSQLxDB(t)
	mock.ExpectExec(`DELETE FROM session_secrets`).
		WillReturnResult(sqlmock.NewResult(0, 2))

	srv := &runtimeServer{db: db}
	srv.purgeOrphanedTerminalSessionSecrets(context.Background())

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}
