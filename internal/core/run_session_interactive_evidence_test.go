package core

import (
	"context"
	"encoding/json"
	"testing"
	"time"

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
	mock.MatchExpectationsInOrder(false)
	now := time.Now()
	expectListEventsBySession(mock, "sess-bg", nil, now)
	expectAppendEventTxWithBegin(mock, "sess-bg", 1, EventEvidenceRecorded, "", nil)
	expectRecordTurnEvents(mock, "sess-bg", 2, 3, true)
	expectSyncFoldAfterTurn(mock, "sess-bg", "hi", "Hi there", "end_turn", provider.TokenUsage{}, now)
	expectParkAwaitingInput(mock, "sess-bg", now)

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
