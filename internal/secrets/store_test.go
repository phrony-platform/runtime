package secrets

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/phrony-platform/runtime/internal/store"
)

func TestDecryptForVersion_roundTrip(t *testing.T) {
	enc := mustTestEncryptor(t)
	plaintext := []byte("sk-live-key")
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

	got, err := enc.DecryptForVersion(context.Background(), store.New(db), "version-uuid", "anthropic")
	if err != nil {
		t.Fatalf("DecryptForVersion: %v", err)
	}
	if string(got) != string(plaintext) {
		t.Fatalf("plaintext = %q, want %q", got, plaintext)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestDecryptForVersion_nilEncryptor(t *testing.T) {
	var enc *Encryptor
	_, err := enc.DecryptForVersion(context.Background(), nil, "v", "s")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "not configured") {
		t.Fatalf("err = %v, want not configured", err)
	}
}

func TestDecryptForVersion_missingRow(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	mock.ExpectQuery(`SELECT key_version, nonce, ciphertext`).
		WithArgs("version-uuid", "anthropic").
		WillReturnError(sql.ErrNoRows)

	enc := mustTestEncryptor(t)
	_, err = enc.DecryptForVersion(context.Background(), store.New(db), "version-uuid", "anthropic")
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("err = %v, want sql.ErrNoRows", err)
	}
}

func TestDecryptForVersion_wrongKeyVersion(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	enc := mustTestEncryptor(t)
	sealed, err := enc.Encrypt("version-id", "anthropic", []byte("secret"))
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	mock.ExpectQuery(`SELECT key_version, nonce, ciphertext`).
		WithArgs("version-id", "anthropic").
		WillReturnRows(sqlmock.NewRows([]string{"key_version", "nonce", "ciphertext"}).
			AddRow(99, sealed.Nonce, sealed.Ciphertext))

	_, err = enc.DecryptForVersion(context.Background(), store.New(db), "version-id", "anthropic")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "unsupported key version") {
		t.Fatalf("err = %v, want unsupported key version", err)
	}
}

func mustTestEncryptor(t *testing.T) *Encryptor {
	t.Helper()
	enc, err := NewEncryptor(bytes.Repeat([]byte{0x42}, 32), 1)
	if err != nil {
		t.Fatalf("NewEncryptor: %v", err)
	}
	return enc
}
