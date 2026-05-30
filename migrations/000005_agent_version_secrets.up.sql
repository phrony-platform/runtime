CREATE TABLE agent_version_secrets (
	agent_version_id UUID NOT NULL REFERENCES agent_versions (id) ON DELETE CASCADE,
	name TEXT NOT NULL,
	key_version INT NOT NULL DEFAULT 1,
	nonce BYTEA NOT NULL,
	ciphertext BYTEA NOT NULL,
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	PRIMARY KEY (agent_version_id, name)
);

INSERT INTO runtime_meta (key, value)
VALUES ('schema_version', '5')
ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value;
