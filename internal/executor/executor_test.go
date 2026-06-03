package executor

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/phrony-platform/runtime/internal/manifest"
	"github.com/phrony-platform/runtime/internal/provider"
	"github.com/phrony-platform/runtime/internal/providertest"
	"github.com/phrony-platform/runtime/internal/secrets"
	"github.com/phrony-platform/runtime/internal/store"
	"github.com/phrony-platform/runtime/internal/tooldispatch"
)

func TestExecutor_LoadVersionForSession(t *testing.T) {
	manifestJSON := []byte(`{
		"apiVersion":"phrony.com/v1",
		"kind":"Agent",
		"metadata":{"name":"a","namespace":"n","version":"1.0.0"},
		"secrets":{"anthropic":{"fromEnv":"ANTHROPIC_API_KEY"}},
		"spec":{
			"purpose":"p",
			"instructions":{"text":"System."},
			"model":{"provider":"anthropic","name":"claude-sonnet-4-5"}
		}
	}`)

	enc := mustTestEncryptor(t)
	plaintext := []byte("sk-test-key")
	sealed, err := enc.Encrypt("session-id", "anthropic", plaintext)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	mock.ExpectQuery(`SELECT manifest`).
		WithArgs("version-uuid").
		WillReturnRows(sqlmock.NewRows([]string{"manifest"}).AddRow(manifestJSON))
	mock.ExpectQuery(`SELECT key_version, nonce, ciphertext`).
		WithArgs("session-id", "anthropic").
		WillReturnRows(sqlmock.NewRows([]string{"key_version", "nonce", "ciphertext"}).
			AddRow(sealed.KeyVersion, sealed.Nonce, sealed.Ciphertext))

	ex := &Executor{Enc: enc, Q: store.New(db)}
	v, err := ex.LoadVersionForSession(context.Background(), "session-id", "version-uuid")
	if err != nil {
		t.Fatalf("LoadVersionForSession: %v", err)
	}
	if v.AgentVersionID != "version-uuid" {
		t.Fatalf("agent_version_id = %q", v.AgentVersionID)
	}
	if v.provider == nil {
		t.Fatal("provider is nil")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func mustTestEncryptor(t *testing.T) *secrets.Encryptor {
	t.Helper()
	enc, err := secrets.NewEncryptor(bytes.Repeat([]byte{0x42}, 32), 1)
	if err != nil {
		t.Fatalf("NewEncryptor: %v", err)
	}
	return enc
}

func TestExecutor_LoadVersionForSession_nilExecutor(t *testing.T) {
	var ex *Executor
	_, err := ex.LoadVersionForSession(context.Background(), "session-id", "v")
	if err == nil {
		t.Fatal("LoadVersionForSession() = nil, want error")
	}
}

func TestExecutor_LoadVersionForSession_noDatabase(t *testing.T) {
	ex := &Executor{Enc: mustTestEncryptor(t)}
	_, err := ex.LoadVersionForSession(context.Background(), "session-id", "v")
	if err == nil {
		t.Fatal("LoadVersionForSession() = nil, want error")
	}
}

func TestExecutor_LoadVersionForSession_emptySessionID(t *testing.T) {
	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	ex := &Executor{Enc: mustTestEncryptor(t), Q: store.New(db)}
	_, err = ex.LoadVersionForSession(context.Background(), "", "version-uuid")
	if err == nil {
		t.Fatal("LoadVersionForSession() = nil, want error")
	}
}

func TestExecutor_LoadVersionForSession_emptyVersionID(t *testing.T) {
	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	ex := &Executor{Enc: mustTestEncryptor(t), Q: store.New(db)}
	_, err = ex.LoadVersionForSession(context.Background(), "session-id", "")
	if err == nil {
		t.Fatal("LoadVersionForSession() = nil, want error")
	}
}

func TestExecutor_LoadVersionForSession_invalidManifest(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	mock.ExpectQuery(`SELECT manifest`).
		WithArgs("version-uuid").
		WillReturnRows(sqlmock.NewRows([]string{"manifest"}).AddRow([]byte(`not json`)))

	ex := &Executor{Enc: mustTestEncryptor(t), Q: store.New(db)}
	_, err = ex.LoadVersionForSession(context.Background(), "session-id", "version-uuid")
	if err == nil {
		t.Fatal("LoadVersionForSession() = nil, want error")
	}
}

func TestExecutor_LoadVersionForSession_notFound(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	mock.ExpectQuery(`SELECT manifest`).
		WithArgs("missing").
		WillReturnError(sql.ErrNoRows)

	ex := &Executor{Enc: mustTestEncryptor(t), Q: store.New(db)}
	_, err = ex.LoadVersionForSession(context.Background(), "session-id", "missing")
	if err == nil {
		t.Fatal("LoadVersionForSession() = nil, want error")
	}
}

func TestStreamCompletion_streamsDeltas(t *testing.T) {
	stub := providertest.DeltaCompleted()
	v := testVersion(stub)
	ch := make(chan Event, 8)

	err := v.StreamCompletion(context.Background(), RunParams{
		Input: json.RawMessage(`{"message":"hello"}`),
	}, ch)
	if err != nil {
		t.Fatalf("StreamCompletion: %v", err)
	}

	var deltas []string
	var completed bool
	for ev := range ch {
		switch ev.Type {
		case EventTextDelta:
			deltas = append(deltas, ev.TextDelta)
		case EventCompleted:
			completed = true
			if ev.StopReason != "end_turn" {
				t.Fatalf("stop_reason = %q", ev.StopReason)
			}
		case EventFailed:
			t.Fatalf("failed: %v", ev.Err)
		}
	}
	if !completed {
		t.Fatal("missing completed event")
	}
	if strings.Join(deltas, "") != "Hi there" {
		t.Fatalf("deltas = %q", strings.Join(deltas, ""))
	}
}

func TestStreamCompletion_providerFailure(t *testing.T) {
	v := testVersion(providertest.Fail(errors.New("provider down")))
	ch := make(chan Event, 4)
	err := v.StreamCompletion(context.Background(), RunParams{
		Input: json.RawMessage(`{"message":"hello"}`),
	}, ch)
	if err == nil {
		t.Fatal("StreamCompletion() = nil, want error")
	}
	var failed bool
	for ev := range ch {
		if ev.Type == EventFailed {
			failed = true
		}
	}
	if !failed {
		t.Fatal("missing failed event")
	}
}

func TestStreamCompletion_skipsEmptyDeltas(t *testing.T) {
	v := testVersion(providertest.EmptyDeltaCompleted())
	ch := make(chan Event, 8)
	if err := v.StreamCompletion(context.Background(), RunParams{
		Input: json.RawMessage(`{"message":"hello"}`),
	}, ch); err != nil {
		t.Fatalf("StreamCompletion: %v", err)
	}
	var deltas int
	for ev := range ch {
		if ev.Type == EventTextDelta {
			deltas++
		}
	}
	if deltas != 1 {
		t.Fatalf("text deltas = %d, want 1 (empty deltas skipped)", deltas)
	}
}

func TestStreamCompletion_prependsHistory(t *testing.T) {
	v := testVersion(providertest.DeltaCompleted())
	ch := make(chan Event, 8)
	history := []provider.Message{{Role: provider.RoleUser, Content: "prior"}}
	if err := v.StreamCompletion(context.Background(), RunParams{
		Input:   json.RawMessage(`{"message":"hello"}`),
		History: history,
	}, ch); err != nil {
		t.Fatalf("StreamCompletion: %v", err)
	}
	for range ch {
	}
}

func TestStreamCompletion_nilVersion(t *testing.T) {
	var v *Version
	ch := make(chan Event, 1)
	err := v.StreamCompletion(context.Background(), RunParams{}, ch)
	if err == nil {
		t.Fatal("StreamCompletion() = nil, want error")
	}
}

func TestStreamCompletion_enforcesTokenLimit(t *testing.T) {
	max := 3
	stub := providertest.DeltaCompleted()
	v := testVersion(stub)
	v.Agent.Spec.Instructions = manifest.InstructionsSpec{}
	v.Agent.Spec.Limits = &manifest.Limits{MaxTokensPerRun: &max, OnLimit: "halt"}

	ch := make(chan Event, 8)
	err := v.StreamCompletion(context.Background(), RunParams{
		Input: json.RawMessage(`{"message":"hello"}`),
	}, ch)
	if err == nil {
		t.Fatal("StreamCompletion() = nil, want error")
	}
	var lim *LimitError
	if !errors.As(err, &lim) || lim.Kind != LimitMaxTokensPerRun {
		t.Fatalf("err = %v", err)
	}

	var failed bool
	for ev := range ch {
		if ev.Type == EventFailed {
			failed = true
			var evLim *LimitError
			if !errors.As(ev.Err, &evLim) {
				t.Fatalf("failed err = %v", ev.Err)
			}
		}
	}
	if !failed {
		t.Fatal("missing failed event on channel")
	}
}

func TestStreamCompletion_toolUseLoop(t *testing.T) {
	toolName := "weather_get_forecast"
	call := provider.ToolCall{ID: "call_1", Name: toolName, Args: json.RawMessage(`{"city":"NYC"}`)}
	stub := providertest.Sequence(
		providertest.ToolUseCompleted(call).Events,
		providertest.DeltaCompleted().Events,
	)
	agent := testAgentWithTool(toolName)
	v := NewVersionWithProvider("version-uuid", agent, stub)

	var dispatched []string
	disp := &tooldispatch.FakeDispatcher{
		DispatchFn: func(_ context.Context, call tooldispatch.ToolCall) (tooldispatch.ToolResult, error) {
			dispatched = append(dispatched, call.Tool)
			return tooldispatch.ToolResult{
				CallID:  call.CallID,
				Payload: json.RawMessage(`{"temp":72}`),
			}, nil
		},
	}

	ch := make(chan Event, 16)
	err := v.StreamCompletion(context.Background(), RunParams{
		SessionID:  "sess-1",
		Turn:       1,
		Input:      json.RawMessage(`{"message":"weather?"}`),
		Dispatcher: disp,
	}, ch)
	if err != nil {
		t.Fatalf("StreamCompletion: %v", err)
	}
	if len(dispatched) != 1 || dispatched[0] != "weather.get-forecast" {
		t.Fatalf("dispatched = %v", dispatched)
	}
	if stub.Calls != 2 {
		t.Fatalf("provider completions = %d, want 2", stub.Calls)
	}

	var completed bool
	for ev := range ch {
		switch ev.Type {
		case EventCompleted:
			completed = true
			if ev.StopReason != provider.StopReasonEndTurn {
				t.Fatalf("stop_reason = %q", ev.StopReason)
			}
		case EventFailed:
			t.Fatalf("failed: %v", ev.Err)
		}
	}
	if !completed {
		t.Fatal("missing completed event")
	}
}

func TestStreamCompletion_toolUseRequiresDispatcher(t *testing.T) {
	toolName := "weather_get_forecast"
	call := provider.ToolCall{ID: "call_1", Name: toolName, Args: json.RawMessage(`{}`)}
	v := NewVersionWithProvider("v", testAgentWithTool(toolName), providertest.ToolUseCompleted(call))

	ch := make(chan Event, 4)
	err := v.StreamCompletion(context.Background(), RunParams{
		Input: json.RawMessage(`{"message":"go"}`),
	}, ch)
	if err == nil {
		t.Fatal("StreamCompletion() = nil, want error")
	}
	for range ch {
	}
}

func TestStreamCompletion_loopIterationLimit_escalate(t *testing.T) {
	max := 1
	toolName := "weather_get_forecast"
	call := provider.ToolCall{ID: "call_1", Name: toolName, Args: json.RawMessage(`{}`)}
	stub := providertest.ToolUseCompleted(call)
	agent := testAgentWithTool(toolName)
	agent.Spec.Limits = &manifest.Limits{MaxLoopIterations: &max, OnLimit: OnLimitEscalate}
	v := NewVersionWithProvider("v", agent, stub)

	ch := make(chan Event, 8)
	err := v.StreamCompletion(context.Background(), RunParams{
		SessionID:  "sess",
		Input:      json.RawMessage(`{"message":"go"}`),
		Dispatcher: &tooldispatch.FakeDispatcher{},
	}, ch)
	if !IsEscalationError(err) {
		t.Fatalf("err = %v, want EscalationError", err)
	}
	var escalated bool
	for ev := range ch {
		if ev.Type == EventEscalation {
			escalated = true
		}
	}
	if !escalated {
		t.Fatal("missing escalation event")
	}
}

func testAgentWithTool(wireName string) *manifest.Agent {
	return &manifest.Agent{
		Spec: manifest.AgentSpec{
			Instructions: manifest.InstructionsSpec{Text: "System."},
			Model: manifest.ModelConfig{
				Provider: provider.IDAnthropic,
				Name:     "claude-sonnet-4-5",
			},
			Tools: []manifest.ToolBinding{{
				Ref:             "weather.get-forecast@1.0.0",
				As:              wireName,
				Version:         "1.0.0",
				InputSchema:     &manifest.SchemaSpec{Inline: map[string]any{"type": "object"}},
				SideEffectClass: manifest.SideEffectReadOnly,
			}},
		},
	}
}

func testVersion(p provider.Provider) *Version {
	agent := &manifest.Agent{
		Spec: manifest.AgentSpec{
			Instructions: manifest.InstructionsSpec{Text: "System."},
			Model: manifest.ModelConfig{
				Provider: provider.IDAnthropic,
				Name:     "claude-sonnet-4-5",
			},
		},
	}
	return NewVersionWithProvider("version-uuid", agent, p)
}
