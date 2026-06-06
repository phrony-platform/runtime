DROP TABLE IF EXISTS session_events;
DROP TABLE IF EXISTS session_evidence;

ALTER TABLE sessions
	DROP COLUMN output,
	DROP COLUMN history;

ALTER TABLE sessions
	ADD COLUMN root_session_id TEXT NOT NULL DEFAULT '',
	ADD COLUMN event_seq INT NOT NULL DEFAULT 0;

CREATE TABLE events (
	id BIGSERIAL PRIMARY KEY,
	session_id TEXT NOT NULL REFERENCES sessions (id) ON DELETE CASCADE,
	root_session_id TEXT NOT NULL,
	seq INT NOT NULL,
	ts TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
	type TEXT NOT NULL,
	turn INT,
	call_id TEXT,
	child_session_id TEXT,
	actor TEXT NOT NULL DEFAULT '',
	payload JSONB NOT NULL DEFAULT '{}',
	UNIQUE (session_id, seq)
);

CREATE INDEX events_session_seq_idx ON events (session_id, seq);
CREATE INDEX events_root_ts_idx ON events (root_session_id, ts, id);
CREATE INDEX events_call_id_idx ON events (call_id) WHERE call_id IS NOT NULL;

INSERT INTO runtime_meta (key, value)
VALUES ('schema_version', '20')
ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value;
