package core

import (
	"context"

	"gorm.io/gorm"
)

func pingDB(ctx context.Context, db *gorm.DB) error {
	return db.WithContext(ctx).Exec("SELECT 1").Error
}
