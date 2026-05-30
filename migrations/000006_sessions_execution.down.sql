ALTER TABLE sessions
	DROP COLUMN IF EXISTS output,
	DROP COLUMN IF EXISTS error;

UPDATE runtime_meta SET value = '5' WHERE key = 'schema_version';
