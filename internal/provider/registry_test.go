package provider

import (
	"bytes"
	"context"
	"database/sql"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/phrony-platform/runtime/internal/manifest"
	"github.com/phrony-platform/runtime/internal/secrets"
	"github.com/phrony-platform/runtime/internal/store"
)

func TestNew_unsupportedProvider(t *testing.T) {
	_, err := New("unknown", "key", "")
	if err == nil {
		t.Fatal("New() = nil, want error")
	}
}

func TestNew_openAICompatibleProvider(t *testing.T) {
	p, err := New(IDOpenAICompatible, "key", "http://localhost:11434/v1")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if p.ID() != IDOpenAICompatible {
		t.Fatalf("ID() = %q, want %q", p.ID(), IDOpenAICompatible)
	}
}

func TestNewForSession_openAICompatibleKeyless(t *testing.T) {
	agent := &manifest.Agent{
		Spec: manifest.AgentSpec{
			Model: manifest.ModelConfig{
				Provider: IDOpenAICompatible,
				Name:     "llama3",
				BaseURL:  "http://localhost:11434/v1",
			},
		},
	}
	reg, err := NewForSession(context.Background(), mustTestEncryptor(t), nil, "session-id", agent)
	if err != nil {
		t.Fatalf("NewForSession: %v", err)
	}
	p, err := reg.ModelProvider(agent)
	if err != nil {
		t.Fatalf("ModelProvider: %v", err)
	}
	if p.ID() != IDOpenAICompatible {
		t.Fatalf("provider id = %q, want %q", p.ID(), IDOpenAICompatible)
	}
}

func TestNewForSession_openAICompatibleWithSecret(t *testing.T) {
	enc := mustTestEncryptor(t)
	plaintext := []byte("sk-test-key")
	sealed, err := enc.Encrypt("session-id", "openai-compatible", plaintext)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	mock.ExpectQuery(`SELECT key_version, nonce, ciphertext`).
		WithArgs("session-id", "openai-compatible").
		WillReturnRows(sqlmock.NewRows([]string{"key_version", "nonce", "ciphertext"}).
			AddRow(sealed.KeyVersion, sealed.Nonce, sealed.Ciphertext))

	agent := &manifest.Agent{
		Secrets: map[string]manifest.SecretDefinition{
			"openai-compatible": {FromEnv: "OPENAI_COMPATIBLE_API_KEY"},
		},
		Spec: manifest.AgentSpec{
			Model: manifest.ModelConfig{
				Provider: IDOpenAICompatible,
				Name:     "llama3",
				BaseURL:  "http://localhost:11434/v1",
			},
		},
	}
	reg, err := NewForSession(context.Background(), enc, store.New(db), "session-id", agent)
	if err != nil {
		t.Fatalf("NewForSession: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}

	p, err := reg.ModelProvider(agent)
	if err != nil {
		t.Fatalf("ModelProvider: %v", err)
	}
	if p.ID() != IDOpenAICompatible {
		t.Fatalf("provider id = %q, want %q", p.ID(), IDOpenAICompatible)
	}
}

func TestRegistry_ModelProvider(t *testing.T) {
	reg := NewRegistry()
	p := &stubProvider{id: IDAnthropic}
	reg.Register(p)

	agent := &manifest.Agent{
		Spec: manifest.AgentSpec{
			Model: manifest.ModelConfig{Provider: IDAnthropic, Name: "claude"},
		},
	}
	got, err := reg.ModelProvider(agent)
	if err != nil {
		t.Fatalf("ModelProvider: %v", err)
	}
	if got != p {
		t.Fatal("ModelProvider returned unexpected provider")
	}
}

func TestNewForSession_decryptsModelSecret(t *testing.T) {
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

	mock.ExpectQuery(`SELECT key_version, nonce, ciphertext`).
		WithArgs("session-id", "anthropic").
		WillReturnRows(sqlmock.NewRows([]string{"key_version", "nonce", "ciphertext"}).
			AddRow(sealed.KeyVersion, sealed.Nonce, sealed.Ciphertext))

	agent := testAgentWithSecrets()
	reg, err := NewForSession(context.Background(), enc, store.New(db), "session-id", agent)
	if err != nil {
		t.Fatalf("NewForSession: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}

	p, err := reg.ModelProvider(agent)
	if err != nil {
		t.Fatalf("ModelProvider: %v", err)
	}
	if p.ID() != IDAnthropic {
		t.Fatalf("provider id = %q, want %q", p.ID(), IDAnthropic)
	}
}

func TestAPIKeyForModel_noSecrets(t *testing.T) {
	_, err := APIKeyForModel(context.Background(), mustTestEncryptor(t), nil, "v", validAgent())
	if err == nil {
		t.Fatal("APIKeyForModel() = nil, want error")
	}
}

func TestAPIKeyForModel_success(t *testing.T) {
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

	mock.ExpectQuery(`SELECT key_version, nonce, ciphertext`).
		WithArgs("session-id", "anthropic").
		WillReturnRows(sqlmock.NewRows([]string{"key_version", "nonce", "ciphertext"}).
			AddRow(sealed.KeyVersion, sealed.Nonce, sealed.Ciphertext))

	key, err := APIKeyForModel(context.Background(), enc, store.New(db), "session-id", testAgentWithSecrets())
	if err != nil {
		t.Fatalf("APIKeyForModel: %v", err)
	}
	if key != "sk-test-key" {
		t.Fatalf("key = %q, want sk-test-key", key)
	}
}

func TestAPIKeyForModel_nilAgent(t *testing.T) {
	_, err := APIKeyForModel(context.Background(), mustTestEncryptor(t), nil, "v", nil)
	if err == nil {
		t.Fatal("APIKeyForModel() = nil, want error")
	}
}

func TestAPIKeyForModel_noEncryptor(t *testing.T) {
	_, err := APIKeyForModel(context.Background(), nil, nil, "v", testAgentWithSecrets())
	if err == nil {
		t.Fatal("APIKeyForModel() = nil, want error")
	}
}

func TestAPIKeyForModel_emptySecret(t *testing.T) {
	enc := mustTestEncryptor(t)
	sealed, err := enc.Encrypt("session-id", "anthropic", []byte("   "))
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	mock.ExpectQuery(`SELECT key_version, nonce, ciphertext`).
		WithArgs("session-id", "anthropic").
		WillReturnRows(sqlmock.NewRows([]string{"key_version", "nonce", "ciphertext"}).
			AddRow(sealed.KeyVersion, sealed.Nonce, sealed.Ciphertext))

	_, err = APIKeyForModel(context.Background(), enc, store.New(db), "session-id", testAgentWithSecrets())
	if err == nil {
		t.Fatal("APIKeyForModel() = nil, want error")
	}
}

func TestRegistry_RegisterNil(t *testing.T) {
	reg := NewRegistry()
	reg.Register(nil)
	if _, ok := reg.Get(IDAnthropic); ok {
		t.Fatal("nil provider should not register")
	}
}

func TestRegistry_GetMissing(t *testing.T) {
	reg := NewRegistry()
	if _, ok := reg.Get("missing"); ok {
		t.Fatal("Get() = true, want false")
	}
}

func TestAPIKeyForModel_missingRow(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	mock.ExpectQuery(`SELECT key_version, nonce, ciphertext`).
		WithArgs("session-id", "anthropic").
		WillReturnError(sql.ErrNoRows)

	_, err = APIKeyForModel(
		context.Background(),
		mustTestEncryptor(t),
		store.New(db),
		"session-id",
		testAgentWithSecrets(),
	)
	if err == nil {
		t.Fatal("APIKeyForModel() = nil, want error")
	}
}

func testAgentWithSecrets() *manifest.Agent {
	return &manifest.Agent{
		Secrets: map[string]manifest.SecretDefinition{
			"anthropic": {FromEnv: "ANTHROPIC_API_KEY"},
		},
		Spec: manifest.AgentSpec{
			Model: manifest.ModelConfig{
				Provider: IDAnthropic,
				Name:     "claude-sonnet-4-5",
			},
		},
	}
}

func validAgent() *manifest.Agent {
	return &manifest.Agent{
		Spec: manifest.AgentSpec{
			Model: manifest.ModelConfig{Provider: IDAnthropic, Name: "claude"},
		},
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

type stubProvider struct {
	id string
}

func (s *stubProvider) ID() string { return s.id }

func (s *stubProvider) Complete(ctx context.Context, req CompletionRequest, ch chan<- CompletionEvent) error {
	defer close(ch)
	ch <- CompletionEvent{Type: EventCompleted, StopReason: StopReasonEndTurn}
	return nil
}
