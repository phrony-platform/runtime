CREATE TABLE session_secrets (
	session_id TEXT NOT NULL REFERENCES sessions (id) ON DELETE CASCADE,
	name TEXT NOT NULL,
	key_version INT NOT NULL DEFAULT 1,
	nonce BYTEA NOT NULL,
	ciphertext BYTEA NOT NULL,
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	PRIMARY KEY (session_id, name)
);

INSERT INTO runtime_meta (key, value)
VALUES ('schema_version', '14')
ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value;
