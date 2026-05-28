DROP INDEX IF EXISTS agent_versions_agent_id_idx;
DROP TABLE IF EXISTS agent_versions;
DROP TABLE IF EXISTS agents;

INSERT INTO runtime_meta (key, value)
VALUES ('schema_version', '1')
ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value;
