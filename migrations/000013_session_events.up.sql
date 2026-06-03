CREATE TABLE session_events (
	id BIGSERIAL PRIMARY KEY,
	session_id TEXT NOT NULL REFERENCES sessions (id) ON DELETE CASCADE,
	type TEXT NOT NULL,
	payload JSONB NOT NULL,
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX session_events_session_id_id_idx ON session_events (session_id, id);

INSERT INTO runtime_meta (key, value)
VALUES ('schema_version', '13')
ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value;
