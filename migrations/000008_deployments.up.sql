CREATE TABLE deployments (
	id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
	agent_id UUID NOT NULL REFERENCES agents (id) ON DELETE CASCADE,
	agent_version_id UUID NOT NULL REFERENCES agent_versions (id) ON DELETE CASCADE,
	action TEXT NOT NULL CHECK (action IN ('deploy', 'rollback')),
	actor TEXT NOT NULL DEFAULT '',
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX deployments_agent_id_created_at_idx ON deployments (agent_id, created_at DESC);

ALTER TABLE agent_versions
	ADD COLUMN retired_at TIMESTAMPTZ;

INSERT INTO runtime_meta (key, value)
VALUES ('schema_version', '8')
ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value;
