package common

import (
	"fmt"

	"github.com/jmoiron/sqlx"
	_ "github.com/jackc/pgx/v5/stdlib"
)

const (
	defaultMaxOpenConns = 25
	defaultMaxIdleConns = 5
)

var connectPostgres = func(dsn string) (*sqlx.DB, error) {
	return sqlx.Connect("pgx", dsn)
}

// SwapConnectPostgres replaces the Postgres connector for tests and returns a restore func.
func SwapConnectPostgres(fn func(string) (*sqlx.DB, error)) func() {
	prev := connectPostgres
	connectPostgres = fn
	return func() { connectPostgres = prev }
}

// OpenDB connects to Postgres using sqlx and tunes the connection pool.
func OpenDB(settings Settings) (*sqlx.DB, error) {
	if err := settings.Validate(); err != nil {
		return nil, err
	}
	return connectDB(settings.DatabaseURL)
}

func connectDB(dsn string) (*sqlx.DB, error) {
	db, err := connectPostgres(dsn)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	db.SetMaxOpenConns(defaultMaxOpenConns)
	db.SetMaxIdleConns(defaultMaxIdleConns)

	return db, nil
}
