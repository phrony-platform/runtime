CREATE TABLE session_evidence (
	session_id TEXT PRIMARY KEY REFERENCES sessions (id) ON DELETE CASCADE,
	payload JSONB NOT NULL,
	recorded_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX session_evidence_recorded_at_idx ON session_evidence (recorded_at);

INSERT INTO runtime_meta (key, value)
VALUES ('schema_version', '11')
ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value;
