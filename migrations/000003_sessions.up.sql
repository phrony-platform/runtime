CREATE TABLE sessions (
	id TEXT PRIMARY KEY,
	agent_version_id UUID NOT NULL REFERENCES agent_versions (id) ON DELETE RESTRICT,
	input JSONB NOT NULL DEFAULT '{}'::jsonb,
	status TEXT NOT NULL DEFAULT 'pending',
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX sessions_agent_version_id_idx ON sessions (agent_version_id);

INSERT INTO runtime_meta (key, value)
VALUES ('schema_version', '3')
ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value;
