CREATE TABLE tool_invocations (
	call_id TEXT PRIMARY KEY,
	session_id TEXT NOT NULL REFERENCES sessions (id) ON DELETE CASCADE,
	agent_version_id UUID NOT NULL REFERENCES agent_versions (id),
	turn INT NOT NULL,
	tool TEXT NOT NULL,
	version TEXT NOT NULL,
	args JSONB NOT NULL DEFAULT '{}'::jsonb,
	result JSONB,
	status TEXT NOT NULL CHECK (
		status IN (
			'pending',
			'queued',
			'awaiting_approval',
			'dispatched',
			'succeeded',
			'failed',
			'indeterminate'
		)
	),
	worker_identity TEXT NOT NULL DEFAULT '',
	image_digest TEXT NOT NULL DEFAULT '',
	descriptor_hash TEXT NOT NULL DEFAULT '',
	manifest_content_hash TEXT NOT NULL DEFAULT '',
	attempt INT NOT NULL DEFAULT 1,
	error_code TEXT,
	error_message TEXT,
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	dispatched_at TIMESTAMPTZ,
	completed_at TIMESTAMPTZ
);

CREATE INDEX tool_invocations_session_id_idx ON tool_invocations (session_id);
CREATE INDEX tool_invocations_session_status_idx ON tool_invocations (session_id, status);

INSERT INTO runtime_meta (key, value)
VALUES ('schema_version', '9')
ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value;
