package core

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
	"github.com/phrony-platform/runtime/internal/executor"
	"github.com/phrony-platform/runtime/internal/manifest"
	"github.com/phrony-platform/runtime/internal/model"
	"github.com/phrony-platform/runtime/internal/provider"
	"github.com/phrony-platform/runtime/internal/store"
)

func TestRuntime_RunSessionInteractive_oneTurnWithStatsEOF(t *testing.T) {
	db, mock := testSQLxDB(t)
	mock.ExpectQuery(`FROM agent_versions av`).
		WithArgs("demo", "echo-agent").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("version-uuid"))
	mock.ExpectQuery(`INSERT INTO sessions`).
		WithArgs(sqlmock.AnyArg(), "version-uuid", []byte(`{"message":"hi"}`), model.SessionStatusRunning).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("sess-1"))
	mock.ExpectQuery(`UPDATE sessions`).
		WithArgs(sqlmock.AnyArg(), model.SessionStatusAwaitingInput, sqlmock.AnyArg(), nil).
		WillReturnRows(sqlmock.NewRows([]string{"updated_at"}).AddRow(time.Now()))
	mock.ExpectQuery(`UPDATE sessions`).
		WithArgs(sqlmock.AnyArg(), model.SessionStatusCompleted, sqlmock.AnyArg(), nil).
		WillReturnRows(sqlmock.NewRows([]string{"updated_at"}).AddRow(time.Now()))

	agent := &manifest.Agent{
		Spec: manifest.AgentSpec{
			Instructions: manifest.InstructionsSpec{Text: "System."},
			Model:        manifest.ModelConfig{Provider: provider.IDAnthropic, Name: "claude-sonnet-4-5"},
		},
	}

	stream := &mockInteractiveStream{
		ctx: context.Background(),
		recv: []*runtimev1.RunSessionInteractiveClientMsg{
			{Body: &runtimev1.RunSessionInteractiveClientMsg_Start{
				Start: &runtimev1.RunSessionInteractiveStart{
					AgentRef: &runtimev1.AgentRef{Namespace: "demo", Name: "echo-agent"},
					Input:    []byte(`{"message":"hi"}`),
				},
			}},
		},
	}

	srv := &runtimeServer{
		db: db,
		loadSessionVersionFn: func(context.Context, *store.Queries, string) (*executor.Version, error) {
			return executor.NewVersionWithProvider("version-uuid", agent, &usageStubProvider{}), nil
		},
	}

	if err := srv.RunSessionInteractive(stream); err != nil {
		t.Fatalf("RunSessionInteractive: %v", err)
	}

	var started, awaiting, completed bool
	for _, msg := range stream.sent {
		switch {
		case msg.GetSessionStarted() != nil:
			started = true
			if got := msg.GetSessionStarted().GetModelProvider(); got != provider.IDAnthropic {
				t.Fatalf("model_provider = %q, want %q", got, provider.IDAnthropic)
			}
		case msg.GetAwaitingInput() != nil:
			awaiting = true
			ai := msg.GetAwaitingInput()
			stats := ai.GetStats()
			if stats == nil {
				t.Fatal("awaiting_input.stats is nil")
			}
			if stats.GetTurn() != 1 {
				t.Fatalf("turn = %d, want 1", stats.GetTurn())
			}
			tu := stats.GetTurnUsage()
			if tu == nil || tu.GetInputTokens() != 10 || tu.GetOutputTokens() != 5 {
				t.Fatalf("turn_usage = %+v, want 10 in / 5 out", tu)
			}
			su := stats.GetSessionUsage()
			if su == nil || su.GetInputTokens() != 10 {
				t.Fatalf("session_usage = %+v", su)
			}
		case msg.GetCompleted() != nil:
			completed = true
			if msg.GetCompleted().GetStats() == nil {
				t.Fatal("completed.stats is nil")
			}
		}
	}
	if !started || !awaiting || !completed {
		t.Fatalf("started=%v awaiting=%v completed=%v", started, awaiting, completed)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

type usageStubProvider struct{}

func (u *usageStubProvider) ID() string { return provider.IDAnthropic }

func (u *usageStubProvider) Complete(ctx context.Context, req provider.CompletionRequest, ch chan<- provider.CompletionEvent) error {
	defer close(ch)
	ch <- provider.CompletionEvent{Type: provider.EventTextDelta, TextDelta: "ok"}
	ch <- provider.CompletionEvent{
		Type:       provider.EventCompleted,
		StopReason: "end_turn",
		Usage:      provider.TokenUsage{InputTokens: 10, OutputTokens: 5},
	}
	return nil
}
