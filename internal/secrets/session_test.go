package secrets

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/phrony-platform/runtime/internal/store"
)

func TestPersistSessionSecrets_roundTrip(t *testing.T) {
	enc := mustTestEncryptor(t)

	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	mock.ExpectExec(`INSERT INTO session_secrets`).
		WithArgs("session-id", "anthropic", 1, sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))

	if err := enc.PersistSessionSecrets(context.Background(), store.New(db), "session-id", map[string][]byte{
		"anthropic": []byte("sk-live-key"),
	}); err != nil {
		t.Fatalf("PersistSessionSecrets: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestPersistSessionSecrets_empty(t *testing.T) {
	enc := mustTestEncryptor(t)
	if err := enc.PersistSessionSecrets(context.Background(), nil, "session-id", nil); err != nil {
		t.Fatalf("PersistSessionSecrets with no values: %v", err)
	}
}

func TestPersistSessionSecrets_nilEncryptor(t *testing.T) {
	var enc *Encryptor
	err := enc.PersistSessionSecrets(context.Background(), nil, "session-id", map[string][]byte{"a": []byte("b")})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "not configured") {
		t.Fatalf("err = %v, want not configured", err)
	}
}

func TestDecryptForSession_roundTrip(t *testing.T) {
	enc := mustTestEncryptor(t)
	plaintext := []byte("sk-live-key")
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

	got, err := enc.DecryptForSession(context.Background(), store.New(db), "session-id", "anthropic")
	if err != nil {
		t.Fatalf("DecryptForSession: %v", err)
	}
	if string(got) != string(plaintext) {
		t.Fatalf("plaintext = %q, want %q", got, plaintext)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestDecryptForSession_wrongSecretName(t *testing.T) {
	enc := mustTestEncryptor(t)
	plaintext := []byte("sk-live-key")
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
		WithArgs("session-id", "openai").
		WillReturnRows(sqlmock.NewRows([]string{"key_version", "nonce", "ciphertext"}).
			AddRow(sealed.KeyVersion, sealed.Nonce, sealed.Ciphertext))

	_, err = enc.DecryptForSession(context.Background(), store.New(db), "session-id", "openai")
	if err == nil {
		t.Fatal("expected decrypt error for mismatched secret name, got nil")
	}
}

func TestDecryptForSession_wrongSession(t *testing.T) {
	enc := mustTestEncryptor(t)
	plaintext := []byte("sk-live-key")
	// Sealed under a different session id: AAD mismatch must fail to open.
	sealed, err := enc.Encrypt("other-session", "anthropic", plaintext)
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

	_, err = enc.DecryptForSession(context.Background(), store.New(db), "session-id", "anthropic")
	if err == nil {
		t.Fatal("expected decrypt error for mismatched session, got nil")
	}
}

func TestDecryptForSession_missingRow(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	mock.ExpectQuery(`SELECT key_version, nonce, ciphertext`).
		WithArgs("session-id", "anthropic").
		WillReturnError(sql.ErrNoRows)

	enc := mustTestEncryptor(t)
	_, err = enc.DecryptForSession(context.Background(), store.New(db), "session-id", "anthropic")
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("err = %v, want sql.ErrNoRows", err)
	}
}

func TestPurgeSessionSecrets(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	mock.ExpectExec(`DELETE FROM session_secrets`).
		WithArgs("session-id").
		WillReturnResult(sqlmock.NewResult(0, 2))

	if err := PurgeSessionSecrets(context.Background(), store.New(db), "session-id"); err != nil {
		t.Fatalf("PurgeSessionSecrets: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}
