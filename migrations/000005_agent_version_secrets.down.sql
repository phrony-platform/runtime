DROP TABLE IF EXISTS agent_version_secrets;

UPDATE runtime_meta SET value = '4' WHERE key = 'schema_version';
