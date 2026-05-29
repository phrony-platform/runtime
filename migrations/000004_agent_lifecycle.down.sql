ALTER TABLE agent_versions
	DROP COLUMN IF EXISTS deprecated_at;

ALTER TABLE agents
	DROP COLUMN IF EXISTS archived_at;

UPDATE runtime_meta SET value = '3' WHERE key = 'schema_version';
