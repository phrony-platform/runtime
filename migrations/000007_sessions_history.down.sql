ALTER TABLE sessions
	DROP COLUMN IF EXISTS history;

UPDATE runtime_meta SET value = '6' WHERE key = 'schema_version';
