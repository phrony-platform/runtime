DROP TABLE IF EXISTS tool_invocations;

UPDATE runtime_meta SET value = '8' WHERE key = 'schema_version';
