CREATE TABLE agents (
	id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
	namespace TEXT NOT NULL,
	name TEXT NOT NULL,
	owner TEXT NOT NULL DEFAULT '',
	labels JSONB NOT NULL DEFAULT '{}'::jsonb,
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	UNIQUE (namespace, name)
);

CREATE TABLE agent_versions (
	id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
	agent_id UUID NOT NULL REFERENCES agents (id) ON DELETE CASCADE,
	version TEXT NOT NULL,
	content_hash TEXT NOT NULL,
	manifest JSONB NOT NULL,
	deployed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	UNIQUE (agent_id, version)
);

CREATE INDEX agent_versions_agent_id_idx ON agent_versions (agent_id);

INSERT INTO runtime_meta (key, value)
VALUES ('schema_version', '2')
ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value;
