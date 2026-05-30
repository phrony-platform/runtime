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
	_, err := New("unknown", "key")
	if err == nil {
		t.Fatal("New() = nil, want error")
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

func TestNewForAgentVersion_decryptsModelSecret(t *testing.T) {
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

	mock.ExpectQuery(`SELECT key_version, nonce, ciphertext`).
		WithArgs("version-uuid", "anthropic").
		WillReturnRows(sqlmock.NewRows([]string{"key_version", "nonce", "ciphertext"}).
			AddRow(sealed.KeyVersion, sealed.Nonce, sealed.Ciphertext))

	agent := testAgentWithSecrets()
	reg, err := NewForAgentVersion(context.Background(), enc, store.New(db), "version-uuid", agent)
	if err != nil {
		t.Fatalf("NewForAgentVersion: %v", err)
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

func TestAPIKeyForModel_missingRow(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	mock.ExpectQuery(`SELECT key_version, nonce, ciphertext`).
		WithArgs("version-uuid", "anthropic").
		WillReturnError(sql.ErrNoRows)

	_, err = APIKeyForModel(
		context.Background(),
		mustTestEncryptor(t),
		store.New(db),
		"version-uuid",
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
	ch <- CompletionEvent{Type: EventCompleted, StopReason: "end_turn"}
	return nil
}
