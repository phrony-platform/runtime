package store

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestInsertAgentVersionSecret(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	mock.ExpectExec(`INSERT INTO agent_version_secrets`).
		WithArgs("version-id", "anthropic", 1, []byte{1}, []byte{2}).
		WillReturnResult(sqlmock.NewResult(0, 1))

	q := New(db)
	if err := q.InsertAgentVersionSecret(context.Background(), InsertAgentVersionSecretParams{
		AgentVersionID: "version-id",
		Name:           "anthropic",
		KeyVersion:     1,
		Nonce:          []byte{1},
		Ciphertext:     []byte{2},
	}); err != nil {
		t.Fatalf("InsertAgentVersionSecret: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}
