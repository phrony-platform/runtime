package core

import (
	"encoding/json"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/phrony-platform/runtime/internal/model"
	"github.com/phrony-platform/runtime/internal/provider"
	"github.com/phrony-platform/runtime/internal/store"
)

var sessionSelectColumns = []string{
	"id", "agent_version_id", "input", "status", "error", "root_session_id", "event_seq", "created_at", "updated_at",
}

func sessionMockRows(sessionID, agentVersionID, status string, input []byte, sessionErr *string, rootSessionID string, eventSeq int, createdAt, updatedAt time.Time) *sqlmock.Rows {
	if rootSessionID == "" {
		rootSessionID = sessionID
	}
	return sqlmock.NewRows(sessionSelectColumns).AddRow(
		sessionID, agentVersionID, input, status, sessionErr, rootSessionID, eventSeq, createdAt, updatedAt,
	)
}

func expectGetSession(mock sqlmock.Sqlmock, sessionID, agentVersionID, status string, input []byte, sessionErr *string, rootSessionID string, eventSeq int, createdAt, updatedAt time.Time) {
	mock.ExpectQuery(`FROM sessions`).
		WithArgs(sessionID).
		WillReturnRows(sessionMockRows(sessionID, agentVersionID, status, input, sessionErr, rootSessionID, eventSeq, createdAt, updatedAt))
}

func expectListEventsBySession(mock sqlmock.Sqlmock, sessionID string, events []store.Event, now time.Time) {
	mock.ExpectQuery(`FROM events`).WithArgs(sessionID).
		WillReturnRows(sessionEventLogRows(now, events...))
}

func expectListEventsBySessionAny(mock sqlmock.Sqlmock, events []store.Event, now time.Time) {
	mock.ExpectQuery(`FROM events`).WithArgs(sqlmock.AnyArg()).
		WillReturnRows(sessionEventLogRows(now, events...))
}

func expectAppendEventTx(mock sqlmock.Sqlmock, sessionID string, eventSeq int, eventType string, updateStatus string, sessionErr *string) {
	now := time.Now()
	mock.ExpectQuery(`FROM sessions`).WithArgs(sessionID).
		WillReturnRows(sessionMockRows(sessionID, "version-uuid", model.SessionStatusRunning, []byte(`{}`), nil, sessionID, eventSeq-1, now, now))
	mock.ExpectQuery(`UPDATE sessions`).WithArgs(sessionID).
		WillReturnRows(sqlmock.NewRows([]string{"event_seq"}).AddRow(eventSeq))
	mock.ExpectQuery(`INSERT INTO events`).
		WithArgs(sessionID, sessionID, eventSeq, eventType, sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(eventSeq)))
	if updateStatus != "" {
		mock.ExpectQuery(`UPDATE sessions`).
			WithArgs(sessionID, updateStatus, sessionErr).
			WillReturnRows(sqlmock.NewRows([]string{"updated_at"}).AddRow(now))
	}
}

func expectAppendSessionFailedTx(mock sqlmock.Sqlmock, sessionID string, eventSeq int, errMsg string) {
	errText := errMsg
	expectAppendEventTx(mock, sessionID, eventSeq, EventSessionFailed, model.SessionStatusFailed, &errText)
}

func expectAppendSessionFailedTxWithBegin(mock sqlmock.Sqlmock, sessionID string, eventSeq int, errMsg string) {
	errText := errMsg
	expectAppendEventTxWithBegin(mock, sessionID, eventSeq, EventSessionFailed, model.SessionStatusFailed, &errText)
}

func expectAppendEventTxWithBegin(mock sqlmock.Sqlmock, sessionID string, eventSeq int, eventType string, updateStatus string, sessionErr *string) {
	now := time.Now()
	mock.ExpectBegin()
	mock.ExpectQuery(`FROM sessions`).WithArgs(sessionID).
		WillReturnRows(sessionMockRows(sessionID, "version-uuid", model.SessionStatusRunning, []byte(`{}`), nil, sessionID, eventSeq-1, now, now))
	mock.ExpectQuery(`UPDATE sessions`).WithArgs(sessionID).
		WillReturnRows(sqlmock.NewRows([]string{"event_seq"}).AddRow(eventSeq))
	mock.ExpectQuery(`INSERT INTO events`).
		WithArgs(sessionID, sessionID, eventSeq, eventType, sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(eventSeq)))
	if updateStatus != "" {
		mock.ExpectQuery(`UPDATE sessions`).
			WithArgs(sessionID, updateStatus, sessionErr).
			WillReturnRows(sqlmock.NewRows([]string{"updated_at"}).AddRow(now))
	}
	mock.ExpectCommit()
}

func expectAppendSessionFailedTxAny(mock sqlmock.Sqlmock, eventSeq int) {
	now := time.Now()
	mock.ExpectBegin()
	mock.ExpectQuery(`FROM sessions`).WithArgs(sqlmock.AnyArg()).
		WillReturnRows(sessionMockRows("sess-any", "version-uuid", model.SessionStatusRunning, []byte(`{}`), nil, "sess-any", eventSeq-1, now, now))
	mock.ExpectQuery(`UPDATE sessions`).WithArgs(sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"event_seq"}).AddRow(eventSeq))
	mock.ExpectQuery(`INSERT INTO events`).
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), eventSeq, EventSessionFailed, sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(eventSeq)))
	mock.ExpectQuery(`UPDATE sessions`).
		WithArgs(sqlmock.AnyArg(), model.SessionStatusFailed, sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"updated_at"}).AddRow(now))
	mock.ExpectCommit()
}

func expectAppendSessionCompletedTx(mock sqlmock.Sqlmock, sessionID string, eventSeq int) {
	expectAppendEventTx(mock, sessionID, eventSeq, EventSessionCompleted, model.SessionStatusCompleted, nil)
}

func expectAppendSessionCompletedTxWithBegin(mock sqlmock.Sqlmock, sessionID string, eventSeq int) {
	expectAppendEventTxWithBegin(mock, sessionID, eventSeq, EventSessionCompleted, model.SessionStatusCompleted, nil)
}

func foldEventsWithMessages(sessionID, userMsg, assistantMsg, stopReason string, turnUsage provider.TokenUsage) []store.Event {
	now := time.Now()
	return []store.Event{
		{
			ID: 1, SessionID: sessionID, RootSessionID: sessionID, Seq: 1, TS: now,
			Type: EventMessageUser, Actor: ActorUser, Payload: userMessagePayload(userMsg),
		},
		{
			ID: 2, SessionID: sessionID, RootSessionID: sessionID, Seq: 2, TS: now.Add(time.Millisecond),
			Type: EventMessageAssistant, Actor: ActorAgent,
			Payload: assistantMessagePayload(assistantMsg, stopReason, turnUsage, 0),
		},
	}
}

func foldEventsForCompletedOutput(sessionID, message, stopReason string, turnUsage provider.TokenUsage, turnDurationMs int64) []store.Event {
	now := time.Now()
	return []store.Event{
		{
			ID: 1, SessionID: sessionID, RootSessionID: sessionID, Seq: 1, TS: now,
			Type: EventMessageUser, Actor: ActorUser, Payload: userMessagePayload("hello"),
		},
		{
			ID: 2, SessionID: sessionID, RootSessionID: sessionID, Seq: 2, TS: now.Add(time.Millisecond),
			Type: EventMessageAssistant, Actor: ActorAgent,
			Payload: assistantMessagePayload(message, stopReason, turnUsage, turnDurationMs),
		},
	}
}

func foldEventsFromOutputJSON(sessionID string, output json.RawMessage) []store.Event {
	var obj sessionOutput
	if err := json.Unmarshal(output, &obj); err != nil {
		return nil
	}
	turnUsage := provider.TokenUsage{}
	if obj.TurnUsage != nil {
		turnUsage = usageFromSessionOutput(obj.TurnUsage)
	}
	dur := int64(0)
	if len(obj.Turns) > 0 {
		dur = obj.Turns[len(obj.Turns)-1].TurnDurationMs
	}
	return foldEventsForCompletedOutput(sessionID, obj.Message, obj.StopReason, turnUsage, dur)
}

func expectAttachSessionFoldQueries(mock sqlmock.Sqlmock, sessionID string, events []store.Event, now time.Time) {
	// loadFoldedSession and replaySessionEventLog list events; ensureSessionEvidence
	// skips the query when the agent snapshot is empty (typical in unit tests).
	for i := 0; i < 2; i++ {
		expectListEventsBySession(mock, sessionID, events, now)
	}
}

func expectAttachSessionFoldQueriesAny(mock sqlmock.Sqlmock, events []store.Event, now time.Time) {
	for i := 0; i < 2; i++ {
		expectListEventsBySessionAny(mock, events, now)
	}
}

func expectInsertSessionWithStartedEvent(mock sqlmock.Sqlmock, versionID string, input []byte, parentSessionID interface{}, depth int, bundleVersionID, rootSessionID interface{}) {
	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO sessions`).
		WithArgs(sqlmock.AnyArg(), versionID, input, model.SessionStatusRunning, parentSessionID, depth, bundleVersionID, rootSessionID).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("generated-session"))
	mock.ExpectQuery(`UPDATE sessions`).WithArgs(sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"event_seq"}).AddRow(1))
	mock.ExpectQuery(`INSERT INTO events`).
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), 1, EventSessionStarted, sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), ActorSystem, input).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(1)))
	mock.ExpectCommit()
}

func expectAttachSessionFoldQueriesWithEvidence(mock sqlmock.Sqlmock, sessionID string, events []store.Event, now time.Time) {
	for i := 0; i < 3; i++ {
		expectListEventsBySession(mock, sessionID, events, now)
	}
}

func expectRecordTurnEvents(mock sqlmock.Sqlmock, sessionID string, userSeq, assistantSeq int, useBegin bool) {
	if useBegin {
		expectAppendEventTxWithBegin(mock, sessionID, userSeq, EventMessageUser, "", nil)
		expectAppendEventTxWithBegin(mock, sessionID, assistantSeq, EventMessageAssistant, "", nil)
	} else {
		expectAppendEventTx(mock, sessionID, userSeq, EventMessageUser, "", nil)
		expectAppendEventTx(mock, sessionID, assistantSeq, EventMessageAssistant, "", nil)
	}
}

func expectRecordTurnEventsAny(mock sqlmock.Sqlmock, userSeq, assistantSeq int, useBegin bool) {
	now := time.Now()
	appendOne := func(eventSeq int, eventType string) {
		if useBegin {
			mock.ExpectBegin()
		}
		mock.ExpectQuery(`FROM sessions`).WithArgs(sqlmock.AnyArg()).
			WillReturnRows(sessionMockRows("sess-any", "version-uuid", model.SessionStatusRunning, []byte(`{}`), nil, "sess-any", eventSeq-1, now, now))
		mock.ExpectQuery(`UPDATE sessions`).WithArgs(sqlmock.AnyArg()).
			WillReturnRows(sqlmock.NewRows([]string{"event_seq"}).AddRow(eventSeq))
		mock.ExpectQuery(`INSERT INTO events`).
			WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), eventSeq, eventType, sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg()).
			WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(eventSeq)))
		if useBegin {
			mock.ExpectCommit()
		}
	}
	appendOne(userSeq, EventMessageUser)
	appendOne(assistantSeq, EventMessageAssistant)
}

func expectSyncFoldAfterTurn(mock sqlmock.Sqlmock, sessionID, userMsg, assistantMsg, stopReason string, turnUsage provider.TokenUsage, now time.Time) {
	events := foldEventsWithMessages(sessionID, userMsg, assistantMsg, stopReason, turnUsage)
	expectListEventsBySession(mock, sessionID, events, now)
	expectListEventsBySession(mock, sessionID, events, now)
}

func expectSyncFoldAfterTurnAny(mock sqlmock.Sqlmock, userMsg, assistantMsg, stopReason string, turnUsage provider.TokenUsage, now time.Time) {
	events := foldEventsWithMessages("sess-any", userMsg, assistantMsg, stopReason, turnUsage)
	expectListEventsBySessionAny(mock, events, now)
	expectListEventsBySessionAny(mock, events, now)
}

func expectParkAwaitingInput(mock sqlmock.Sqlmock, sessionID string, now time.Time) {
	mock.ExpectQuery(`UPDATE sessions`).
		WithArgs(sessionID, model.SessionStatusAwaitingInput, nil).
		WillReturnRows(sqlmock.NewRows([]string{"updated_at"}).AddRow(now))
}

func expectParkAwaitingInputAny(mock sqlmock.Sqlmock, now time.Time) {
	mock.ExpectQuery(`UPDATE sessions`).
		WithArgs(sqlmock.AnyArg(), model.SessionStatusAwaitingInput, nil).
		WillReturnRows(sqlmock.NewRows([]string{"updated_at"}).AddRow(now))
}

func expectListEventsByRoot(mock sqlmock.Sqlmock, rootSessionID string, events []store.Event, now time.Time) {
	mock.ExpectQuery(`WHERE root_session_id`).WithArgs(rootSessionID).
		WillReturnRows(sessionEventLogRows(now, events...))
}

func expectAppendToolIndeterminateTx(mock sqlmock.Sqlmock, sessionID string, eventSeq int, callID string) {
	expectAppendEventTx(mock, sessionID, eventSeq, EventToolIndeterminate, "", nil)
	mock.ExpectQuery(`UPDATE tool_invocations`).
		WithArgs(callID, model.ToolInvocationIndeterminate, "indeterminate", sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"call_id"}).AddRow(callID))
}

func expectAppendApprovalRequiredTx(mock sqlmock.Sqlmock, sessionID string, eventSeq int) {
	now := time.Now()
	expectAppendEventTx(mock, sessionID, eventSeq, EventApprovalRequired, "", nil)
	mock.ExpectQuery(`INSERT INTO tool_invocations`).
		WillReturnRows(sqlmock.NewRows([]string{"call_id"}).AddRow("call-hitl"))
	mock.ExpectQuery(`INSERT INTO approvals`).
		WillReturnRows(sqlmock.NewRows([]string{"created_at"}).AddRow(now))
}

func expectApprovalEscalateTimeoutFlow(mock sqlmock.Sqlmock, sessionID, parentApprovalID string, eventSeq int) {
	now := time.Now()
	mock.ExpectBegin()
	mock.ExpectQuery(`FROM sessions`).WithArgs(sessionID).
		WillReturnRows(sessionMockRows(sessionID, "av-1", model.SessionStatusAwaitingApproval, []byte(`{}`), nil, sessionID, eventSeq-1, now, now))
	mock.ExpectQuery(`UPDATE sessions`).WithArgs(sessionID).
		WillReturnRows(sqlmock.NewRows([]string{"event_seq"}).AddRow(eventSeq))
	mock.ExpectQuery(`INSERT INTO events`).
		WithArgs(sessionID, sessionID, eventSeq, EventApprovalEscalated, sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(eventSeq)))
	mock.ExpectQuery(`UPDATE approvals`).
		WithArgs(parentApprovalID, model.ApprovalStatusEscalated, "system:timeout").
		WillReturnRows(sqlmock.NewRows([]string{"decided_at"}).AddRow(now))
	mock.ExpectCommit()

	mock.ExpectBegin()
	mock.ExpectQuery(`FROM sessions`).WithArgs(sessionID).
		WillReturnRows(sessionMockRows(sessionID, "av-1", model.SessionStatusAwaitingApproval, []byte(`{}`), nil, sessionID, eventSeq, now, now))
	mock.ExpectQuery(`UPDATE sessions`).WithArgs(sessionID).
		WillReturnRows(sqlmock.NewRows([]string{"event_seq"}).AddRow(eventSeq+1))
	mock.ExpectQuery(`INSERT INTO events`).
		WithArgs(sessionID, sessionID, eventSeq+1, EventApprovalRequired, sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(eventSeq+1)))
	mock.ExpectQuery(`INSERT INTO tool_invocations`).
		WillReturnRows(sqlmock.NewRows([]string{"call_id"}).AddRow("call-1"))
	mock.ExpectQuery(`INSERT INTO approvals`).
		WillReturnRows(sqlmock.NewRows([]string{"created_at"}).AddRow(now))
	mock.ExpectCommit()
}
