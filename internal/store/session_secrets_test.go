package store

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestInsertSessionSecret(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	mock.ExpectExec(`INSERT INTO session_secrets`).
		WithArgs("session-id", "anthropic", 1, []byte{1}, []byte{2}).
		WillReturnResult(sqlmock.NewResult(0, 1))

	q := New(db)
	if err := q.InsertSessionSecret(context.Background(), InsertSessionSecretParams{
		SessionID:  "session-id",
		Name:       "anthropic",
		KeyVersion: 1,
		Nonce:      []byte{1},
		Ciphertext: []byte{2},
	}); err != nil {
		t.Fatalf("InsertSessionSecret: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestSessionSecret(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	mock.ExpectQuery(`SELECT key_version, nonce, ciphertext`).
		WithArgs("session-id", "anthropic").
		WillReturnRows(sqlmock.NewRows([]string{"key_version", "nonce", "ciphertext"}).
			AddRow(1, []byte{1}, []byte{2}))

	q := New(db)
	row, err := q.SessionSecret(context.Background(), "session-id", "anthropic")
	if err != nil {
		t.Fatalf("SessionSecret: %v", err)
	}
	if row.KeyVersion != 1 {
		t.Fatalf("key_version = %d, want 1", row.KeyVersion)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestDeleteSessionSecrets(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	mock.ExpectExec(`DELETE FROM session_secrets`).
		WithArgs("session-id").
		WillReturnResult(sqlmock.NewResult(0, 2))

	q := New(db)
	if err := q.DeleteSessionSecrets(context.Background(), "session-id"); err != nil {
		t.Fatalf("DeleteSessionSecrets: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestDeleteTerminalSessionSecrets(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	mock.ExpectExec(`DELETE FROM session_secrets`).
		WillReturnResult(sqlmock.NewResult(0, 3))

	q := New(db)
	if err := q.DeleteTerminalSessionSecrets(context.Background()); err != nil {
		t.Fatalf("DeleteTerminalSessionSecrets: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}
