package core

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/phrony-platform/runtime/internal/evidence"
	"github.com/phrony-platform/runtime/internal/manifest"
	"github.com/phrony-platform/runtime/internal/model"
	"github.com/phrony-platform/runtime/internal/store"
)

func TestEnsureSessionEvidence_recordsAndIsIdempotent(t *testing.T) {
	t.Parallel()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	agent := &manifest.Agent{
		Metadata: manifest.AgentMetadata{
			Owner: "team-a",
			Labels: map[string]string{
				"tier": "high",
			},
			Governance: &manifest.GovernanceMetadata{
				RiskTier: "high",
				Frameworks: map[string]json.RawMessage{
					"custom/v1": json.RawMessage(`{"x":1}`),
				},
			},
		},
	}
	payload, _ := evidence.BuildSnapshot(agent).JSON()
	now := time.Now()

	mock.ExpectQuery(`FROM events`).WithArgs("sess-1").
		WillReturnRows(sessionEventLogRows(now))
	mock.ExpectBegin()
	mock.ExpectQuery(`FROM sessions`).WithArgs("sess-1").
		WillReturnRows(sessionMockRows("sess-1", "version-uuid", model.SessionStatusRunning, []byte(`{}`), nil, "sess-1", 0, now, now))
	mock.ExpectQuery(`UPDATE sessions`).WithArgs("sess-1").
		WillReturnRows(sqlmock.NewRows([]string{"event_seq"}).AddRow(1))
	mock.ExpectQuery(`INSERT INTO events`).
		WithArgs("sess-1", "sess-1", 1, EventEvidenceRecorded, sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), ActorSystem, sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(1)))
	mock.ExpectCommit()

	recorded := []store.Event{{
		ID: 1, SessionID: "sess-1", RootSessionID: "sess-1", Seq: 1, TS: now,
		Type: EventEvidenceRecorded, Actor: ActorSystem, Payload: payload,
	}}
	mock.ExpectQuery(`FROM events`).WithArgs("sess-1").
		WillReturnRows(sessionEventLogRows(now, recorded...))

	q := store.New(db)
	srv := &runtimeServer{}
	ctx := context.Background()

	snap1, err := srv.ensureSessionEvidence(ctx, q, "sess-1", agent)
	if err != nil {
		t.Fatalf("ensureSessionEvidence first: %v", err)
	}
	if snap1.Owner != "team-a" {
		t.Fatalf("snap1 owner = %q", snap1.Owner)
	}
	snap2, err := srv.ensureSessionEvidence(ctx, q, "sess-1", agent)
	if err != nil {
		t.Fatalf("ensureSessionEvidence second: %v", err)
	}
	if snap2.Owner != "team-a" {
		t.Fatalf("snap2 owner = %q", snap2.Owner)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestEvidenceSnapshotToProto_frameworkValidation(t *testing.T) {
	t.Parallel()
	snap := evidence.Snapshot{
		Governance: &evidence.Governance{
			Frameworks: []evidence.FrameworkPack{
				{ID: "eu-ai-act/v1", Validation: evidence.ValidationValidated, Payload: []byte(`{"role":"provider"}`)},
				{ID: "unknown/v1", Validation: evidence.ValidationUnvalidated, Payload: []byte(`{}`)},
			},
		},
	}
	out := evidenceSnapshotToProto(snap)
	if out == nil || out.Governance == nil {
		t.Fatal("expected governance in proto")
	}
	if len(out.Governance.Frameworks) != 2 {
		t.Fatalf("frameworks = %d", len(out.Governance.Frameworks))
	}
	if out.Governance.Frameworks[1].GetValidation() != evidence.ValidationUnvalidated {
		t.Fatalf("validation = %q", out.Governance.Frameworks[1].GetValidation())
	}
}
