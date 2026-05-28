package core

import (
	"fmt"

	"github.com/jmoiron/sqlx"
)

// SchemaMetaVersionKey is the runtime_meta key for the applied schema version.
const SchemaMetaVersionKey = "schema_version"

const schemaVersionValue = "1"

const createRuntimeMetaTable = `
CREATE TABLE IF NOT EXISTS runtime_meta (
	key TEXT PRIMARY KEY,
	value TEXT NOT NULL
)`

const upsertSchemaVersion = `
INSERT INTO runtime_meta (key, value)
VALUES ($1, $2)
ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value`

// Migrate applies the v0 schema and seeds runtime metadata.
func Migrate(db *sqlx.DB) error {
	if _, err := db.Exec(createRuntimeMetaTable); err != nil {
		return fmt.Errorf("create runtime_meta: %w", err)
	}

	if _, err := db.Exec(upsertSchemaVersion, SchemaMetaVersionKey, schemaVersionValue); err != nil {
		return fmt.Errorf("seed schema_version: %w", err)
	}

	return nil
}
