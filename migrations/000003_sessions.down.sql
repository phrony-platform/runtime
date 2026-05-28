DROP INDEX IF EXISTS sessions_agent_version_id_idx;
DROP TABLE IF EXISTS sessions;

INSERT INTO runtime_meta (key, value)
VALUES ('schema_version', '2')
ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value;
