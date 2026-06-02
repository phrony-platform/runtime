DROP TABLE IF EXISTS approval_votes;

ALTER TABLE approvals DROP CONSTRAINT approvals_status_check;

ALTER TABLE approvals
	ADD CONSTRAINT approvals_status_check CHECK (
		status IN ('pending', 'approved', 'denied')
	);

ALTER TABLE approvals
	DROP COLUMN IF EXISTS tool,
	DROP COLUMN IF EXISTS version,
	DROP COLUMN IF EXISTS args,
	DROP COLUMN IF EXISTS authority_ref,
	DROP COLUMN IF EXISTS policy_name,
	DROP COLUMN IF EXISTS approvals_required,
	DROP COLUMN IF EXISTS approvals_received,
	DROP COLUMN IF EXISTS comprehension_required,
	DROP COLUMN IF EXISTS on_reject,
	DROP COLUMN IF EXISTS on_modify,
	DROP COLUMN IF EXISTS expires_at,
	DROP COLUMN IF EXISTS policy_runtime;

UPDATE runtime_meta
SET value = '11'
WHERE key = 'schema_version';
