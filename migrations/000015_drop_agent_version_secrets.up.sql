DROP TABLE IF EXISTS agent_version_secrets;

INSERT INTO runtime_meta (key, value)
VALUES ('schema_version', '15')
ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value;
