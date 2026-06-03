package core

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
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
	output := []byte(`{"message":"hi","stop_reason":"end_turn","turn_usage":{"input_tokens":1,"output_tokens":1},"session_usage":{"input_tokens":1,"output_tokens":1}}`)
	history := []byte(`[{"role":"user","content":"hello"},{"role":"assistant","content":"hi"}]`)

	db, mock := testSQLxDB(t)
	mock.ExpectQuery(`FROM sessions`).
		WithArgs("sess-stale").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "agent_version_id", "input", "status", "output", "error", "history", "created_at", "updated_at",
		}).AddRow("sess-stale", "version-uuid", []byte(`{"message":"hello"}`), model.SessionStatusRunning, output, nil, history, created, now))
	mock.ExpectQuery(`UPDATE sessions`).
		WithArgs("sess-stale", model.SessionStatusFailed, nil, sqlmock.AnyArg(), nil).
		WillReturnRows(sqlmock.NewRows([]string{"updated_at"}).AddRow(now))

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
