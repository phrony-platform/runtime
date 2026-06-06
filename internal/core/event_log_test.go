package core

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/phrony-platform/runtime/internal/model"
	"github.com/phrony-platform/runtime/internal/store"
	"github.com/phrony-platform/runtime/internal/tooldispatch"
)

func TestRebuildProjections_matchesLiveToolInvocation(t *testing.T) {
	db := openToolTestPostgres(t)
	q := store.New(db.DB)

	namespace := "rebuild-" + uuid.NewString()[:8]
	sessionID := uuid.NewString()
	_, agentVersionID, _ := insertToolE2EAgentFixture(t, db, namespace)
	insertToolE2ESessionAwaitingTool(t, db, sessionID, agentVersionID, nil)
	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM events WHERE session_id = $1`, sessionID)
		_, _ = db.Exec(`DELETE FROM tool_invocations WHERE session_id = $1`, sessionID)
		_, _ = db.Exec(`DELETE FROM sessions WHERE id = $1`, sessionID)
		_, _ = db.Exec(`DELETE FROM agent_versions WHERE agent_id IN (SELECT id FROM agents WHERE namespace = $1)`, namespace)
		_, _ = db.Exec(`DELETE FROM agents WHERE namespace = $1`, namespace)
	})

	rec := NewToolInvocationRecorder(q)
	call := tooldispatch.ToolCall{
		CallID:         tooldispatch.DeriveCallID(sessionID, agentVersionID, 1, 0),
		SessionID:      sessionID,
		AgentVersionID: agentVersionID,
		Turn:           1,
		Tool:           "weather.get-forecast",
		Version:        "1.0.0",
		Args:           json.RawMessage(`{"city":"NYC"}`),
	}
	ctx := context.Background()
	if err := rec.RecordPending(ctx, call, model.ToolInvocationPending); err != nil {
		t.Fatalf("RecordPending: %v", err)
	}
	live, err := q.GetToolInvocation(ctx, call.CallID)
	if err != nil {
		t.Fatalf("GetToolInvocation live: %v", err)
	}

	if err := RebuildProjections(ctx, q, sessionID); err != nil {
		t.Fatalf("RebuildProjections: %v", err)
	}
	rebuilt, err := q.GetToolInvocation(ctx, call.CallID)
	if err != nil {
		t.Fatalf("GetToolInvocation rebuilt: %v", err)
	}
	if rebuilt.Status != live.Status || rebuilt.Tool != live.Tool || rebuilt.Version != live.Version {
		t.Fatalf("rebuilt = %+v, want live %+v", rebuilt, live)
	}
}

func TestAppendEvent_incrementsSeqAndWritesProjection(t *testing.T) {
	db, mock := testSQLxDB(t)
	now := time.Now()
	call := tooldispatch.ToolCall{
		CallID:         "call-1",
		SessionID:      "sess-1",
		AgentVersionID: "ver-1",
		Turn:           1,
		Tool:           "weather.get-forecast",
		Version:        "1.0.0",
		Args:           json.RawMessage(`{"city":"NYC"}`),
	}

	mock.ExpectQuery(`FROM sessions`).WithArgs("sess-1").
		WillReturnRows(sessionMockRows("sess-1", "ver-1", model.SessionStatusAwaitingTool, []byte(`{}`), nil, "sess-1", 0, now, now))
	mock.ExpectQuery(`UPDATE sessions`).WithArgs("sess-1").
		WillReturnRows(sqlmock.NewRows([]string{"event_seq"}).AddRow(1))
	mock.ExpectQuery(`INSERT INTO events`).
		WithArgs("sess-1", "sess-1", 1, EventToolRequested, sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), ActorAgent, sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(1)))
	mock.ExpectQuery(`INSERT INTO tool_invocations`).
		WithArgs("call-1", "sess-1", "ver-1", 1, "weather.get-forecast", "1.0.0", json.RawMessage(`{"city":"NYC"}`), model.ToolInvocationPending).
		WillReturnRows(sqlmock.NewRows([]string{"call_id"}).AddRow("call-1"))

	ctx := context.Background()
	_, seq, err := appendEventAuto(ctx, store.New(db), EventInput{
		SessionID: "sess-1",
		Type:      EventToolRequested,
		Actor:     ActorAgent,
		Tool:      &EventToolProjection{Call: call, Status: model.ToolInvocationPending},
	})
	if err != nil {
		t.Fatalf("appendEventAuto: %v", err)
	}
	if seq != 1 {
		t.Fatalf("seq = %d, want 1", seq)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestExpireWallClockSession_emitsSessionFailedEvent(t *testing.T) {
	db := openToolTestPostgres(t)
	namespace := "expire-" + uuid.NewString()[:8]
	sessionID := uuid.NewString()
	_, agentVersionID, _ := insertToolE2EAgentFixture(t, db, namespace)
	insertToolE2ESessionAwaitingTool(t, db, sessionID, agentVersionID, nil)
	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM events WHERE session_id = $1`, sessionID)
		_, _ = db.Exec(`DELETE FROM tool_invocations WHERE session_id = $1`, sessionID)
		_, _ = db.Exec(`DELETE FROM sessions WHERE id = $1`, sessionID)
		_, _ = db.Exec(`DELETE FROM agent_versions WHERE agent_id IN (SELECT id FROM agents WHERE namespace = $1)`, namespace)
		_, _ = db.Exec(`DELETE FROM agents WHERE namespace = $1`, namespace)
	})

	srv := &runtimeServer{db: db}
	srv.expireWallClockSession(sessionID, "halt")

	ctx := context.Background()
	q := store.New(db.DB)
	events, err := q.ListEventsBySession(ctx, sessionID)
	if err != nil {
		t.Fatalf("ListEventsBySession: %v", err)
	}
	var found bool
	for _, ev := range events {
		if ev.Type == EventSessionFailed {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("events = %+v, want session.failed", events)
	}
	session, err := q.GetSession(ctx, sessionID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if session.Status != model.SessionStatusFailed {
		t.Fatalf("status = %q, want failed", session.Status)
	}
}
