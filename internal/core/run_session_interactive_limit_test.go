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

func TestInteractiveSessionState_sessionLoopIterationLimitError(t *testing.T) {
	max := 2
	st := &interactiveSessionState{
		turnCount: 2,
		version: executor.NewVersionWithProvider("v", &manifest.Agent{
			Spec: manifest.AgentSpec{
				Limits: &manifest.Limits{MaxLoopIterations: &max, OnLimit: "halt"},
			},
		}, nil),
	}
	if err := st.sessionLoopIterationLimitError(); err == nil {
		t.Fatal("expected loop limit error at turnCount == max")
	}
	st.turnCount = 1
	if err := st.sessionLoopIterationLimitError(); err != nil {
		t.Fatalf("turnCount 1 with max 2: %v", err)
	}
	st.turnCount = 0
	if err := st.sessionLoopIterationLimitError(); err != nil {
		t.Fatalf("turnCount 0 with max 2: %v", err)
	}
}

func TestExecutorIsLimitErrorMessage(t *testing.T) {
	if !executor.IsLimitErrorMessage("run limit max_tokens_per_run exceeded (on_limit=halt)") {
		t.Fatal("expected limit session error")
	}
	if executor.IsLimitErrorMessage("model unavailable") {
		t.Fatal("unexpected limit match for generic error")
	}
}

func TestInteractiveSessionState_sessionWallClockLimitError(t *testing.T) {
	max := 30
	st := &interactiveSessionState{
		sessionStartedAt: time.Now().Add(-time.Minute),
		version: executor.NewVersionWithProvider("v", &manifest.Agent{
			Spec: manifest.AgentSpec{
				Limits: &manifest.Limits{MaxWallClockSeconds: &max, OnLimit: "halt"},
			},
		}, nil),
	}
	if err := st.sessionWallClockLimitError(); err == nil {
		t.Fatal("expected wall clock limit error")
	}
}

func TestRuntime_RunSessionInteractive_cumulativeTokenLimitBlocksInput(t *testing.T) {
	max := 14
	db, mock := testSQLxDB(t)
	mock.ExpectQuery(`FROM agent_versions av`).
		WithArgs("demo", "echo-agent").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("version-uuid"))
	mock.ExpectQuery(`INSERT INTO sessions`).
		WithArgs(sqlmock.AnyArg(), "version-uuid", []byte(`{"message":"one"}`), model.SessionStatusRunning).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("sess-1"))
	mock.ExpectQuery(`UPDATE sessions`).
		WithArgs(sqlmock.AnyArg(), model.SessionStatusAwaitingInput, sqlmock.AnyArg(), nil, sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"updated_at"}).AddRow(time.Now()))
	mock.ExpectQuery(`UPDATE sessions`).
		WithArgs(sqlmock.AnyArg(), model.SessionStatusCompleted, sqlmock.AnyArg(), nil, nil).
		WillReturnRows(sqlmock.NewRows([]string{"updated_at"}).AddRow(time.Now()))

	agent := &manifest.Agent{
		Spec: manifest.AgentSpec{
			Instructions: manifest.InstructionsSpec{Text: "System."},
			Model:        manifest.ModelConfig{Provider: provider.IDAnthropic, Name: "m"},
			Limits:       &manifest.Limits{MaxTokensPerRun: &max, OnLimit: "halt"},
		},
	}

	stream := &mockInteractiveStream{
		ctx: context.Background(),
		recv: []*runtimev1.RunSessionInteractiveClientMsg{
			{Body: &runtimev1.RunSessionInteractiveClientMsg_Start{
				Start: &runtimev1.RunSessionInteractiveStart{
					AgentRef: &runtimev1.AgentRef{Namespace: "demo", Name: "echo-agent"},
					Input:    []byte(`{"message":"one"}`),
				},
			}},
		},
	}

	srv := &runtimeServer{
		db: db,
		loadSessionVersionFn: func(context.Context, *store.Queries, string) (*executor.Version, error) {
			return executor.NewVersionWithProvider("version-uuid", agent, providertest.UsageCompleted(provider.TokenUsage{InputTokens: 10, OutputTokens: 5})), nil
		},
	}
	_ = srv.RunSessionInteractive(stream)

	var blockedAwaiting bool
	for _, msg := range stream.sent {
		if ai := msg.GetAwaitingInput(); ai != nil && ai.GetInputBlockedReason() != "" {
			blockedAwaiting = true
			if !strings.Contains(ai.GetInputBlockedReason(), "max_tokens_per_run") {
				t.Fatalf("blocked reason = %q", ai.GetInputBlockedReason())
			}
		}
		if msg.GetFailed() != nil {
			t.Fatalf("unexpected failed: %s", msg.GetFailed().GetMessage())
		}
	}
	if !blockedAwaiting {
		t.Fatal("expected awaiting_input with input_blocked_reason after first turn")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestRuntime_RunSessionInteractive_loopIterationLimitBlocksInput(t *testing.T) {
	max := 1
	db, mock := testSQLxDB(t)
	mock.ExpectQuery(`FROM agent_versions av`).
		WithArgs("demo", "echo-agent").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("version-uuid"))
	mock.ExpectQuery(`INSERT INTO sessions`).
		WithArgs(sqlmock.AnyArg(), "version-uuid", []byte(`{"message":"one"}`), model.SessionStatusRunning).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("sess-1"))
	mock.ExpectQuery(`UPDATE sessions`).
		WithArgs(sqlmock.AnyArg(), model.SessionStatusAwaitingInput, sqlmock.AnyArg(), nil, sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"updated_at"}).AddRow(time.Now()))
	mock.ExpectQuery(`UPDATE sessions`).
		WithArgs(sqlmock.AnyArg(), model.SessionStatusCompleted, sqlmock.AnyArg(), nil, nil).
		WillReturnRows(sqlmock.NewRows([]string{"updated_at"}).AddRow(time.Now()))

	agent := &manifest.Agent{
		Spec: manifest.AgentSpec{
			Instructions: manifest.InstructionsSpec{Text: "System."},
			Model:        manifest.ModelConfig{Provider: provider.IDAnthropic, Name: "m"},
			Limits:       &manifest.Limits{MaxLoopIterations: &max, OnLimit: "halt"},
		},
	}

	stream := &mockInteractiveStream{
		ctx: context.Background(),
		recv: []*runtimev1.RunSessionInteractiveClientMsg{
			{Body: &runtimev1.RunSessionInteractiveClientMsg_Start{
				Start: &runtimev1.RunSessionInteractiveStart{
					AgentRef: &runtimev1.AgentRef{Namespace: "demo", Name: "echo-agent"},
					Input:    []byte(`{"message":"one"}`),
				},
			}},
			{Body: &runtimev1.RunSessionInteractiveClientMsg_UserMessage{
				UserMessage: &runtimev1.RunSessionInteractiveUserMessage{Text: "two"},
			}},
		},
	}

	srv := &runtimeServer{
		db: db,
		loadSessionVersionFn: func(context.Context, *store.Queries, string) (*executor.Version, error) {
			return executor.NewVersionWithProvider("version-uuid", agent, providertest.UsageCompleted(provider.TokenUsage{InputTokens: 10, OutputTokens: 5})), nil
		},
	}
	_ = srv.RunSessionInteractive(stream)

	var blocked bool
	for _, msg := range stream.sent {
		if ai := msg.GetAwaitingInput(); ai != nil && ai.GetInputBlockedReason() != "" {
			blocked = true
			if !strings.Contains(ai.GetInputBlockedReason(), "max_loop_iterations") {
				t.Fatalf("blocked reason = %q", ai.GetInputBlockedReason())
			}
		}
		if msg.GetFailed() != nil {
			t.Fatalf("unexpected failed: %s", msg.GetFailed().GetMessage())
		}
	}
	if !blocked {
		t.Fatal("expected input blocked after max_loop_iterations")
	}
}

func TestRuntime_RunSessionInteractive_attachOverTokenLimitBlocksInput(t *testing.T) {
	max := 10
	now := time.Now()
	output := []byte(`{"message":"hi","stop_reason":"end_turn","turn_usage":{"input_tokens":8,"output_tokens":4,"total_tokens":12},"session_usage":{"input_tokens":8,"output_tokens":4,"total_tokens":12}}`)
	history := []byte(`[{"role":"user","content":"hello"},{"role":"assistant","content":"hi"}]`)

	db, mock := testSQLxDB(t)
	mock.ExpectQuery(`FROM sessions`).
		WithArgs("sess-1").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "agent_version_id", "input", "status", "output", "error", "history", "created_at", "updated_at",
		}).AddRow("sess-1", "version-uuid", []byte(`{"message":"hello"}`), model.SessionStatusAwaitingInput, output, nil, history, now, now))

	agent := &manifest.Agent{
		Spec: manifest.AgentSpec{
			Instructions: manifest.InstructionsSpec{Text: "System."},
			Model:        manifest.ModelConfig{Provider: provider.IDAnthropic, Name: "m"},
			Limits:       &manifest.Limits{MaxTokensPerRun: &max, OnLimit: "halt"},
		},
	}

	stream := &mockInteractiveStream{
		ctx: context.Background(),
		recv: []*runtimev1.RunSessionInteractiveClientMsg{
			{Body: &runtimev1.RunSessionInteractiveClientMsg_Start{
				Start: &runtimev1.RunSessionInteractiveStart{SessionId: "sess-1"},
			}},
		},
	}

	srv := &runtimeServer{
		db: db,
		loadSessionVersionFn: func(context.Context, *store.Queries, string) (*executor.Version, error) {
			return executor.NewVersionWithProvider("version-uuid", agent, providertest.UsageCompleted(provider.TokenUsage{InputTokens: 10, OutputTokens: 5})), nil
		},
	}
	if err := srv.RunSessionInteractive(stream); err != nil {
		t.Fatalf("RunSessionInteractive: %v", err)
	}

	var started, blocked bool
	for _, msg := range stream.sent {
		if msg.GetSessionStarted() != nil {
			started = true
		}
		if ai := msg.GetAwaitingInput(); ai != nil {
			if ai.GetInputBlockedReason() == "" {
				t.Fatal("expected blocked awaiting_input on attach")
			}
			blocked = true
		}
		if msg.GetFailed() != nil {
			t.Fatal("attach over limit should not fail session")
		}
	}
	if !started || !blocked {
		t.Fatalf("started=%v blocked=%v", started, blocked)
	}
}

func TestRuntime_RunSessionInteractive_attachFailedRunLimitBlocked(t *testing.T) {
	now := time.Now()
	limitErr := "run limit max_tokens_per_run exceeded (on_limit=halt)"
	output := []byte(`{"message":"hi","stop_reason":"end_turn","turn_usage":{"input_tokens":8,"output_tokens":4},"session_usage":{"input_tokens":8,"output_tokens":4}}`)
	history := []byte(`[{"role":"user","content":"hello"},{"role":"assistant","content":"hi"}]`)

	db, mock := testSQLxDB(t)
	mock.ExpectQuery(`FROM sessions`).
		WithArgs("sess-1").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "agent_version_id", "input", "status", "output", "error", "history", "created_at", "updated_at",
		}).AddRow("sess-1", "version-uuid", []byte(`{"message":"hello"}`), model.SessionStatusFailed, output, &limitErr, history, now, now))
	mock.ExpectQuery(`UPDATE sessions`).
		WithArgs("sess-1", model.SessionStatusAwaitingInput, nil, "", nil).
		WillReturnRows(sqlmock.NewRows([]string{"updated_at"}).AddRow(now))

	agent := &manifest.Agent{
		Spec: manifest.AgentSpec{
			Instructions: manifest.InstructionsSpec{Text: "System."},
			Model:        manifest.ModelConfig{Provider: provider.IDAnthropic, Name: "m"},
			Limits:       &manifest.Limits{MaxTokensPerRun: intPtr(10), OnLimit: "halt"},
		},
	}

	stream := &mockInteractiveStream{
		ctx: context.Background(),
		recv: []*runtimev1.RunSessionInteractiveClientMsg{
			{Body: &runtimev1.RunSessionInteractiveClientMsg_Start{
				Start: &runtimev1.RunSessionInteractiveStart{SessionId: "sess-1"},
			}},
		},
	}

	srv := &runtimeServer{
		db: db,
		loadSessionVersionFn: func(context.Context, *store.Queries, string) (*executor.Version, error) {
			return executor.NewVersionWithProvider("version-uuid", agent, providertest.UsageCompleted(provider.TokenUsage{InputTokens: 10, OutputTokens: 5})), nil
		},
	}
	_ = srv.RunSessionInteractive(stream)

	var started, blocked bool
	for _, msg := range stream.sent {
		if msg.GetSessionStarted() != nil {
			started = true
		}
		if ai := msg.GetAwaitingInput(); ai != nil && ai.GetInputBlockedReason() != "" {
			blocked = true
		}
		if msg.GetFailed() != nil {
			t.Fatalf("unexpected failed message: %s", msg.GetFailed().GetMessage())
		}
	}
	if !started || !blocked {
		t.Fatalf("started=%v blocked=%v", started, blocked)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func intPtr(n int) *int { return &n }

func TestRuntime_RunSessionInteractive_wallClockPushesBlockedWhileWaiting(t *testing.T) {
	max := 30
	now := time.Now()
	output := []byte(`{"message":"hi","stop_reason":"end_turn","turn_usage":{"input_tokens":1,"output_tokens":1},"session_usage":{"input_tokens":1,"output_tokens":1}}`)
	history := []byte(`[{"role":"user","content":"hello"},{"role":"assistant","content":"hi"}]`)

	db, mock := testSQLxDB(t)
	mock.ExpectQuery(`FROM sessions`).
		WithArgs("sess-1").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "agent_version_id", "input", "status", "output", "error", "history", "created_at", "updated_at",
		}).AddRow("sess-1", "version-uuid", []byte(`{"message":"hello"}`), model.SessionStatusAwaitingInput, output, nil, history, now.Add(-time.Minute), now))

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
				Start: &runtimev1.RunSessionInteractiveStart{SessionId: "sess-1"},
			}},
		},
	}

	srv := &runtimeServer{
		db: db,
		loadSessionVersionFn: func(context.Context, *store.Queries, string) (*executor.Version, error) {
			return executor.NewVersionWithProvider("version-uuid", agent, providertest.UsageCompleted(provider.TokenUsage{InputTokens: 10, OutputTokens: 5})), nil
		},
	}
	done := make(chan struct{})
	go func() {
		_ = srv.RunSessionInteractive(stream)
		close(done)
	}()

	deadline := time.Now().Add(2 * time.Second)
	for {
		var blocked bool
		for _, msg := range stream.sent {
			if ai := msg.GetAwaitingInput(); ai != nil && strings.Contains(ai.GetInputBlockedReason(), "max_wall_clock_seconds") {
				blocked = true
				break
			}
		}
		if blocked {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("sent = %+v, want proactive wall clock blocked awaiting_input", stream.sent)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
