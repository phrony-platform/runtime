package common

import (
	"errors"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/go-playground/validator/v10"
	"github.com/jmoiron/sqlx"
)

func TestOpenDB_invalidSettings(t *testing.T) {
	_, err := OpenDB(Settings{})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var verrs validator.ValidationErrors
	if !errors.As(err, &verrs) {
		t.Fatalf("expected validator.ValidationErrors, got %v", err)
	}
}

func TestOpenDB_success(t *testing.T) {
	restore := stubConnectPostgres(t)
	defer restore()

	db, err := OpenDB(Settings{
		DatabaseURL: "postgres://unused",
		GRPCAddr:    defaultGRPCAddr,
		RuntimeAddr: defaultRuntimeAddr,
	})
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if got := db.Stats().MaxOpenConnections; got != defaultMaxOpenConns {
		t.Fatalf("MaxOpenConnections = %d, want %d", got, defaultMaxOpenConns)
	}
}

func TestConnectDB_openFailed(t *testing.T) {
	prev := connectPostgres
	connectPostgres = func(string) (*sqlx.DB, error) {
		return nil, errors.New("dial failed")
	}
	t.Cleanup(func() { connectPostgres = prev })

	_, err := connectDB("postgres://unused")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "open database") {
		t.Fatalf("expected wrapped open error, got %v", err)
	}
}

func stubConnectPostgres(t *testing.T) func() {
	t.Helper()
	prev := connectPostgres
	connectPostgres = func(string) (*sqlx.DB, error) {
		sqlDB, _, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New: %v", err)
		}
		t.Cleanup(func() { _ = sqlDB.Close() })
		return sqlx.NewDb(sqlDB, "pgx"), nil
	}
	return func() { connectPostgres = prev }
}
