CREATE TABLE agent_version_secrets (
	agent_version_id UUID NOT NULL REFERENCES agent_versions (id) ON DELETE CASCADE,
	name TEXT NOT NULL,
	key_version INT NOT NULL DEFAULT 1,
	nonce BYTEA NOT NULL,
	ciphertext BYTEA NOT NULL,
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	PRIMARY KEY (agent_version_id, name)
);

UPDATE runtime_meta SET value = '14' WHERE key = 'schema_version';
