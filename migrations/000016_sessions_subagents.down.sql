DROP INDEX IF EXISTS sessions_parent_session_id_idx;

ALTER TABLE sessions
	DROP COLUMN IF EXISTS parent_session_id,
	DROP COLUMN IF EXISTS depth;

UPDATE runtime_meta SET value = '15' WHERE key = 'schema_version';
