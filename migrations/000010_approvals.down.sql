DROP TABLE IF EXISTS approvals;

UPDATE runtime_meta SET value = '9' WHERE key = 'schema_version';
