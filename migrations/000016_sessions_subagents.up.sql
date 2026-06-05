ALTER TABLE sessions
	ADD COLUMN parent_session_id TEXT REFERENCES sessions (id) ON DELETE CASCADE,
	ADD COLUMN depth INT NOT NULL DEFAULT 0;

CREATE INDEX sessions_parent_session_id_idx ON sessions (parent_session_id);

INSERT INTO runtime_meta (key, value)
VALUES ('schema_version', '16')
ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value;
