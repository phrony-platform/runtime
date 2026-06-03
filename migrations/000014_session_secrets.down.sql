DROP TABLE IF EXISTS session_secrets;

UPDATE runtime_meta SET value = '13' WHERE key = 'schema_version';
