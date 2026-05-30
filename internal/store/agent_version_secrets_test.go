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

func TestListSecretsForVersion(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	mock.ExpectQuery(`FROM agent_version_secrets`).
		WithArgs("version-id").
		WillReturnRows(sqlmock.NewRows([]string{"name", "key_version", "nonce", "ciphertext"}).
			AddRow("anthropic", 1, []byte{1}, []byte{2}).
			AddRow("openai", 1, []byte{3}, []byte{4}))

	q := New(db)
	rows, err := q.ListSecretsForVersion(context.Background(), "version-id")
	if err != nil {
		t.Fatalf("ListSecretsForVersion: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("len(rows) = %d, want 2", len(rows))
	}
	if rows[0].Name != "anthropic" || rows[1].Name != "openai" {
		t.Fatalf("names = %q, %q", rows[0].Name, rows[1].Name)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}
