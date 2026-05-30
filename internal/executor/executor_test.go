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
	"github.com/phrony-platform/runtime/internal/secrets"
	"github.com/phrony-platform/runtime/internal/store"
)

func TestExecutor_LoadVersion(t *testing.T) {
	manifestJSON := []byte(`{
		"apiVersion":"phrony.dev/v1",
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
	sealed, err := enc.Encrypt("version-uuid", "anthropic", plaintext)
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
		WithArgs("version-uuid", "anthropic").
		WillReturnRows(sqlmock.NewRows([]string{"key_version", "nonce", "ciphertext"}).
			AddRow(sealed.KeyVersion, sealed.Nonce, sealed.Ciphertext))

	ex := &Executor{Enc: enc, Q: store.New(db)}
	v, err := ex.LoadVersion(context.Background(), "version-uuid")
	if err != nil {
		t.Fatalf("LoadVersion: %v", err)
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

func TestExecutor_LoadVersion_notFound(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	mock.ExpectQuery(`SELECT manifest`).
		WithArgs("missing").
		WillReturnError(sql.ErrNoRows)

	ex := &Executor{Enc: mustTestEncryptor(t), Q: store.New(db)}
	_, err = ex.LoadVersion(context.Background(), "missing")
	if err == nil {
		t.Fatal("LoadVersion() = nil, want error")
	}
}

func TestStreamCompletion_streamsDeltas(t *testing.T) {
	stub := &deltaStubProvider{}
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

func TestStreamCompletion_enforcesTokenLimit(t *testing.T) {
	max := 3
	stub := &deltaStubProvider{}
	v := testVersion(stub)
	v.Agent.Spec.Instructions = manifest.InstructionsSpec{}
	v.Agent.Spec.Limits = &manifest.Limits{MaxTokensPerRun: &max, OnLimit: "halt"}

	ch := make(chan Event, 8)
	err := v.StreamCompletion(context.Background(), RunParams{
		Input: json.RawMessage(`{"message":"hello"}`),
	}, ch)
	if err == nil {
		t.Fatal("StreamCompletion() = nil, want limit error")
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

func testVersion(p provider.Provider) *Version {
	return &Version{
		AgentVersionID: "version-uuid",
		Agent: &manifest.Agent{
			Spec: manifest.AgentSpec{
				Instructions: manifest.InstructionsSpec{Text: "System."},
				Model: manifest.ModelConfig{
					Provider: provider.IDAnthropic,
					Name:     "claude-sonnet-4-5",
				},
			},
		},
		provider: p,
	}
}

type deltaStubProvider struct{}

func (d *deltaStubProvider) ID() string { return provider.IDAnthropic }

func (d *deltaStubProvider) Complete(ctx context.Context, req provider.CompletionRequest, ch chan<- provider.CompletionEvent) error {
	defer close(ch)
	ch <- provider.CompletionEvent{Type: provider.EventTextDelta, TextDelta: "Hi "}
	ch <- provider.CompletionEvent{Type: provider.EventTextDelta, TextDelta: "there"}
	ch <- provider.CompletionEvent{Type: provider.EventCompleted, StopReason: "end_turn"}
	return nil
}
