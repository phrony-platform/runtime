DROP TABLE IF EXISTS events;

ALTER TABLE sessions
	DROP COLUMN root_session_id,
	DROP COLUMN event_seq;

ALTER TABLE sessions
	ADD COLUMN output JSONB,
	ADD COLUMN history JSONB NOT NULL DEFAULT '[]'::jsonb;

CREATE TABLE session_evidence (
	session_id TEXT PRIMARY KEY REFERENCES sessions (id) ON DELETE CASCADE,
	payload JSONB NOT NULL,
	recorded_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX session_evidence_recorded_at_idx ON session_evidence (recorded_at);

CREATE TABLE session_events (
	id BIGSERIAL PRIMARY KEY,
	session_id TEXT NOT NULL REFERENCES sessions (id) ON DELETE CASCADE,
	type TEXT NOT NULL,
	payload JSONB NOT NULL,
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX session_events_session_id_id_idx ON session_events (session_id, id);

INSERT INTO runtime_meta (key, value)
VALUES ('schema_version', '19')
ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value;
