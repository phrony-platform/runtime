package core

import (
	"fmt"

	"github.com/phrony-platform/runtime/internal/model"
	"gorm.io/gorm"
)

const schemaVersionKey = "schema_version"
const schemaVersionValue = "1"

// Migrate applies the v0 schema and seeds runtime metadata.
func Migrate(db *gorm.DB) error {
	if err := db.AutoMigrate(&model.RuntimeMeta{}); err != nil {
		return fmt.Errorf("auto migrate: %w", err)
	}

	var meta model.RuntimeMeta
	if err := db.Where(model.RuntimeMeta{Key: schemaVersionKey}).
		Assign(model.RuntimeMeta{Value: schemaVersionValue}).
		FirstOrCreate(&meta).Error; err != nil {
		return fmt.Errorf("seed schema_version: %w", err)
	}

	return nil
}
