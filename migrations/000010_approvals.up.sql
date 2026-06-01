CREATE TABLE approvals (
	id UUID PRIMARY KEY,
	session_id TEXT NOT NULL REFERENCES sessions (id) ON DELETE CASCADE,
	call_id TEXT NOT NULL REFERENCES tool_invocations (call_id) ON DELETE CASCADE,
	status TEXT NOT NULL CHECK (status IN ('pending', 'approved', 'denied')),
	route TEXT NOT NULL DEFAULT '',
	reason TEXT NOT NULL DEFAULT '',
	decided_by TEXT NOT NULL DEFAULT '',
	comment TEXT NOT NULL DEFAULT '',
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	decided_at TIMESTAMPTZ
);

CREATE INDEX approvals_session_id_idx ON approvals (session_id);
CREATE INDEX approvals_session_status_idx ON approvals (session_id, status);

INSERT INTO runtime_meta (key, value)
VALUES ('schema_version', '10')
ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value;
