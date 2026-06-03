package core

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/phrony-platform/runtime/internal/executor"
	"github.com/phrony-platform/runtime/internal/manifest"
	"github.com/phrony-platform/runtime/internal/evidence"
	"github.com/phrony-platform/runtime/internal/providertest"
	"github.com/phrony-platform/runtime/internal/provider"
	"github.com/phrony-platform/runtime/internal/store"
)

func TestSendSessionStarted_includesDescriptiveMetadata(t *testing.T) {
	agent := &manifest.Agent{
		Metadata: manifest.AgentMetadata{
			Owner: "claims-platform",
			Labels: map[string]string{
				"owning-team": "claims",
			},
			Governance: &manifest.GovernanceMetadata{
				RiskTier: "high",
				Frameworks: map[string]json.RawMessage{
					"custom/v1": json.RawMessage(`{"tier":"internal"}`),
				},
			},
		},
		Spec: manifest.AgentSpec{
			Model: manifest.ModelConfig{Provider: provider.IDAnthropic, Name: "m"},
		},
	}
	stream := &mockInteractiveStream{ctx: context.Background()}
	ver := executor.NewVersionWithProvider("version-uuid", agent, providertest.DeltaCompleted())
	snap := evidence.BuildSnapshot(agent)
	if err := sendSessionStarted(sessionEventsFromStream(stream), "sess-1", "version-uuid", ver, nil, time.Now(), nil, evidenceSnapshotToProto(snap)); err != nil {
		t.Fatalf("sendSessionStarted: %v", err)
	}
	if len(stream.sent) != 1 {
		t.Fatalf("sent %d messages, want 1", len(stream.sent))
	}
	meta := stream.sent[0].GetSessionStarted().GetDescriptiveMetadata()
	if meta == nil {
		t.Fatal("descriptive_metadata missing from session_started")
	}
	if meta.GetOwner() != "claims-platform" {
		t.Fatalf("owner = %q", meta.GetOwner())
	}
	if meta.GetGovernance().GetFrameworks()[0].GetValidation() != "unvalidated" {
		t.Fatalf("validation = %q", meta.GetGovernance().GetFrameworks()[0].GetValidation())
	}
}

func TestRuntime_RunSessionBackground_recordsSessionEvidence(t *testing.T) {
	db, mock := testSQLxDB(t)
	mock.ExpectQuery(`FROM session_evidence`).
		WithArgs("sess-bg").
		WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery(`INSERT INTO session_evidence`).
		WithArgs("sess-bg", sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"session_id"}).AddRow("sess-bg"))
	mock.ExpectQuery(`UPDATE sessions`).
		WillReturnRows(sqlmock.NewRows([]string{"updated_at"}).AddRow(time.Now()))

	agent := &manifest.Agent{
		Metadata: manifest.AgentMetadata{Owner: "ops"},
		Spec: manifest.AgentSpec{
			Model: manifest.ModelConfig{Provider: provider.IDAnthropic, Name: "m"},
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
