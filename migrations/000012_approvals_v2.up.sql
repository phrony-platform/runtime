ALTER TABLE approvals
	ADD COLUMN tool TEXT NOT NULL DEFAULT '',
	ADD COLUMN version TEXT NOT NULL DEFAULT '',
	ADD COLUMN args JSONB,
	ADD COLUMN authority_ref TEXT NOT NULL DEFAULT '',
	ADD COLUMN policy_name TEXT NOT NULL DEFAULT '',
	ADD COLUMN approvals_required INT NOT NULL DEFAULT 1,
	ADD COLUMN approvals_received INT NOT NULL DEFAULT 0,
	ADD COLUMN comprehension_required BOOL NOT NULL DEFAULT FALSE,
	ADD COLUMN on_reject TEXT NOT NULL DEFAULT '',
	ADD COLUMN on_modify TEXT NOT NULL DEFAULT '',
	ADD COLUMN expires_at TIMESTAMPTZ,
	ADD COLUMN policy_runtime JSONB;

ALTER TABLE approvals DROP CONSTRAINT approvals_status_check;

ALTER TABLE approvals
	ADD CONSTRAINT approvals_status_check CHECK (
		status IN ('pending', 'approved', 'denied', 'escalated')
	);

CREATE TABLE approval_votes (
	approval_id UUID NOT NULL REFERENCES approvals (id) ON DELETE CASCADE,
	decided_by TEXT NOT NULL,
	decision TEXT NOT NULL CHECK (decision IN ('approved', 'denied')),
	comment TEXT NOT NULL DEFAULT '',
	comprehension_acknowledged BOOL NOT NULL DEFAULT FALSE,
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	PRIMARY KEY (approval_id, decided_by)
);

CREATE INDEX approval_votes_approval_id_idx ON approval_votes (approval_id);

INSERT INTO runtime_meta (key, value)
VALUES ('schema_version', '12')
ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value;
