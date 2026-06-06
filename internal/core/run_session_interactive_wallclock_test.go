package core

import (
	"context"
	"strings"
	"testing"
	"time"

	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
	"github.com/phrony-platform/runtime/internal/executor"
	"github.com/phrony-platform/runtime/internal/manifest"
	"github.com/phrony-platform/runtime/internal/model"
	"github.com/phrony-platform/runtime/internal/provider"
	"github.com/phrony-platform/runtime/internal/providertest"
	"github.com/phrony-platform/runtime/internal/store"
)

func TestRuntime_RunSessionInteractive_attachReconcilesStaleRunningWallClock(t *testing.T) {
	max := 30
	created := time.Now().Add(-2 * time.Minute)
	now := time.Now()
	events := foldEventsWithMessages("sess-stale", "hello", "hi", "end_turn", provider.TokenUsage{InputTokens: 1, OutputTokens: 1})

	db, mock := testSQLxDB(t)
	mock.MatchExpectationsInOrder(false)
	mock.ExpectQuery(`FROM sessions`).
		WithArgs("sess-stale").
		WillReturnRows(sessionMockRows("sess-stale", "version-uuid", model.SessionStatusRunning, []byte(`{"message":"hello"}`), nil, "sess-stale", len(events), created, now))
	for i := 0; i < 2; i++ {
		expectListEventsBySession(mock, "sess-stale", events, now)
	}
	expectAppendSessionFailedTxWithBegin(mock, "sess-stale", len(events)+1, "run limit max_wall_clock_seconds exceeded (on_limit=halt)")
	expectAttachSessionFoldQueries(mock, "sess-stale", events, now)

	agent := &manifest.Agent{
		Spec: manifest.AgentSpec{
			Instructions: manifest.InstructionsSpec{Text: "System."},
			Model:        manifest.ModelConfig{Provider: provider.IDAnthropic, Name: "m"},
			Limits:       &manifest.Limits{MaxWallClockSeconds: &max, OnLimit: "halt"},
		},
	}

	stream := &mockInteractiveStream{
		ctx: context.Background(),
		recv: []*runtimev1.RunSessionInteractiveClientMsg{
			{Body: &runtimev1.RunSessionInteractiveClientMsg_Start{
				Start: &runtimev1.RunSessionInteractiveStart{SessionId: "sess-stale"},
			}},
		},
	}

	srv := &runtimeServer{
		db: db,
		loadSessionVersionFn: func(context.Context, *store.Queries, string, string) (*executor.Version, error) {
			return executor.NewVersionWithProvider("version-uuid", agent, providertest.UsageCompleted(provider.TokenUsage{InputTokens: 1, OutputTokens: 1})), nil
		},
	}
	if err := srv.RunSessionInteractive(stream); err != nil {
		t.Fatalf("RunSessionInteractive: %v", err)
	}

	var started, failed bool
	for _, msg := range stream.sent {
		if msg.GetSessionStarted() != nil {
			started = true
		}
		if f := msg.GetFailed(); f != nil {
			failed = true
			if !strings.Contains(f.GetMessage(), "max_wall_clock_seconds") {
				t.Fatalf("failed message = %q", f.GetMessage())
			}
		}
	}
	if !started || !failed {
		t.Fatalf("started=%v failed=%v", started, failed)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}
