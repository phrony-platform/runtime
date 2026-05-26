package common

import (
	"fmt"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

const (
	defaultMaxOpenConns = 25
	defaultMaxIdleConns = 5
)

var newPostgresDialector = func(dsn string) gorm.Dialector {
	return postgres.Open(dsn)
}

// OpenDB connects to Postgres using GORM and tunes the underlying sql.DB pool.
func OpenDB(settings Settings) (*gorm.DB, error) {
	if err := settings.Validate(); err != nil {
		return nil, err
	}
	return connectDB(newPostgresDialector(settings.DatabaseURL))
}

func connectDB(dialector gorm.Dialector) (*gorm.DB, error) {
	db, err := gorm.Open(dialector, &gorm.Config{})
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("underlying sql.DB: %w", err)
	}

	sqlDB.SetMaxOpenConns(defaultMaxOpenConns)
	sqlDB.SetMaxIdleConns(defaultMaxIdleConns)

	return db, nil
}
