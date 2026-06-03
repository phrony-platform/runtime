package core

import (
	"context"
	"testing"
	"time"

	"github.com/phrony-platform/runtime/internal/executor"
	"github.com/phrony-platform/runtime/internal/manifest"
	"github.com/phrony-platform/runtime/internal/model"
	"github.com/phrony-platform/runtime/internal/provider"
	"github.com/phrony-platform/runtime/internal/providertest"
	"github.com/phrony-platform/runtime/internal/store"
)

func TestReconcileRecoveredSession_awaitingApprovalParks(t *testing.T) {
	db, mock := testSQLxDB(t)
	q := store.New(db)
	srv := &runtimeServer{db: db}

	expires := time.Now().Add(time.Hour)
	mock.ExpectQuery(`FROM approvals`).
		WithArgs("sess-1").
		WillReturnRows(approvalRowFrom(approvalRowOpts{
			ID: "appr-1", SessionID: "sess-1", CallID: "call-1",
			Status: model.ApprovalStatusPending, ExpiresAt: &expires,
		}))
	agent := &manifest.Agent{
		Spec: manifest.AgentSpec{
			Instructions: manifest.InstructionsSpec{Text: "System."},
			Model:        manifest.ModelConfig{Provider: provider.IDAnthropic, Name: "m"},
		},
	}
	srv.loadSessionVersionFn = func(context.Context, *store.Queries, string, string) (*executor.Version, error) {
		return executor.NewVersionWithProvider("av-1", agent, providertest.DeltaCompleted()), nil
	}

	srv.reconcileRecoveredSession(context.Background(), q, store.Session{
		ID:             "sess-1",
		AgentVersionID: "av-1",
		Status:         model.SessionStatusAwaitingApproval,
	})

	coord := srv.approvalCoord()
	coord.mu.Lock()
	parked := coord.parkedSessions["sess-1"]
	coord.mu.Unlock()
	if parked != "appr-1" {
		t.Fatalf("parked approval = %q, want appr-1", parked)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql: %v", err)
	}
}
