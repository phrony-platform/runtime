package common

import (
	"errors"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/go-playground/validator/v10"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/schema"
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
	restore := stubPostgresDialector(t)
	defer restore()

	db, err := OpenDB(Settings{
		DatabaseURL: "postgres://unused",
		GRPCAddr:    defaultGRPCAddr,
		RuntimeAddr: defaultRuntimeAddr,
	})
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	t.Cleanup(func() {
		sqlDB, err := db.DB()
		if err != nil {
			t.Errorf("db.DB: %v", err)
			return
		}
		_ = sqlDB.Close()
	})

	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("db.DB: %v", err)
	}
	if got := sqlDB.Stats().MaxOpenConnections; got != defaultMaxOpenConns {
		t.Fatalf("MaxOpenConnections = %d, want %d", got, defaultMaxOpenConns)
	}
}

func TestNewPostgresDialector(t *testing.T) {
	d := newPostgresDialector("postgres://localhost:5432/test?sslmode=disable")
	if d.Name() != "postgres" {
		t.Fatalf("dialector name = %q, want postgres", d.Name())
	}
}

func TestConnectDB_dbFailed(t *testing.T) {
	_, err := connectDB(invalidPoolDialector{})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "underlying sql.DB") {
		t.Fatalf("expected underlying sql.DB error, got %v", err)
	}
}

func TestConnectDB_openFailed(t *testing.T) {
	sqlDB, mock, err := sqlmock.New(sqlmock.MonitorPingsOption(true))
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer sqlDB.Close()

	mock.ExpectPing().WillReturnError(errors.New("dial failed"))

	_, err = connectDB(postgres.New(postgres.Config{Conn: sqlDB}))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "open database") {
		t.Fatalf("expected wrapped open error, got %v", err)
	}
}

func stubPostgresDialector(t *testing.T) func() {
	t.Helper()
	prev := newPostgresDialector
	newPostgresDialector = func(string) gorm.Dialector {
		sqlDB, _, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New: %v", err)
		}
		t.Cleanup(func() { _ = sqlDB.Close() })
		return postgres.New(postgres.Config{Conn: sqlDB})
	}
	return func() { newPostgresDialector = prev }
}

// invalidPoolDialector opens GORM with a ConnPool that db.DB() cannot unwrap.
type invalidPoolDialector struct{}

func (invalidPoolDialector) Name() string { return "mock" }

func (invalidPoolDialector) Initialize(db *gorm.DB) error {
	db.ConnPool = nil
	return nil
}

func (invalidPoolDialector) Migrator(*gorm.DB) gorm.Migrator { return nil }

func (invalidPoolDialector) DataTypeOf(*schema.Field) string { return "" }

func (invalidPoolDialector) DefaultValueOf(*schema.Field) clause.Expression { return nil }

func (invalidPoolDialector) BindVarTo(clause.Writer, *gorm.Statement, interface{}) {}

func (invalidPoolDialector) QuoteTo(clause.Writer, string) {}

func (invalidPoolDialector) Explain(string, ...interface{}) string { return "" }
