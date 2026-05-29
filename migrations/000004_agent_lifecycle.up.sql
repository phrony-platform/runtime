ALTER TABLE agents
	ADD COLUMN archived_at TIMESTAMPTZ;

ALTER TABLE agent_versions
	ADD COLUMN deprecated_at TIMESTAMPTZ;

INSERT INTO runtime_meta (key, value)
VALUES ('schema_version', '4')
ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value;
