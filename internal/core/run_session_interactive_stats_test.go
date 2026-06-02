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
	"github.com/phrony-platform/runtime/internal/providertest"
	"github.com/phrony-platform/runtime/internal/store"
)

func TestRuntime_RunSessionInteractive_oneTurnWithStatsEOF(t *testing.T) {
	db, mock := testSQLxDB(t)
	mock.MatchExpectationsInOrder(false)
	expectActiveDeployment(mock, "demo", "echo-agent", "version-uuid", "1.2.0")
	mock.ExpectQuery(`INSERT INTO sessions`).
		WithArgs(sqlmock.AnyArg(), "version-uuid", []byte(`{"message":"hi"}`), model.SessionStatusRunning).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("sess-1"))
	expectGetRunningSessionForAttach(mock, "version-uuid", []byte(`{"message":"hi"}`))
	mock.ExpectQuery(`UPDATE sessions`).
		WithArgs(sqlmock.AnyArg(), model.SessionStatusAwaitingInput, sqlmock.AnyArg(), nil, sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"updated_at"}).AddRow(time.Now()))

	agent := &manifest.Agent{
		Spec: manifest.AgentSpec{
			Instructions: manifest.InstructionsSpec{Text: "System."},
			Model:        manifest.ModelConfig{Provider: provider.IDAnthropic, Name: "claude-sonnet-4-5"},
		},
	}

	attachCtx, stopAttach := context.WithCancel(context.Background())
	defer stopAttach()
	stream := &blockingAfterStartStream{mockInteractiveStream: &mockInteractiveStream{
		ctx: attachCtx,
		recv: []*runtimev1.RunSessionInteractiveClientMsg{
			{Body: &runtimev1.RunSessionInteractiveClientMsg_Start{
				Start: &runtimev1.RunSessionInteractiveStart{
					AgentRef: &runtimev1.AgentRef{Namespace: "demo", Name: "echo-agent"},
					Input:    []byte(`{"message":"hi"}`),
				},
			}},
		},
	}}

	srv := &runtimeServer{
		db: db,
		loadSessionVersionFn: func(context.Context, *store.Queries, string) (*executor.Version, error) {
			return executor.NewVersionWithProvider("version-uuid", agent, providertest.UsageCompleted(provider.TokenUsage{InputTokens: 10, OutputTokens: 5})), nil
		},
	}

	done := make(chan error, 1)
	go func() { done <- srv.RunSessionInteractive(stream) }()

	var started, awaiting bool
	waitForInteractiveMessages(t, done, func() []*runtimev1.RunSessionInteractiveServerMsg { return stream.sent }, func(msgs []*runtimev1.RunSessionInteractiveServerMsg) bool {
		started, awaiting = false, false
		for _, msg := range msgs {
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
			}
		}
		return started && awaiting
	})
	stopAttach()
	if err := <-done; err != nil {
		t.Fatalf("RunSessionInteractive: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}
