DROP INDEX IF EXISTS sessions_bundle_version_id_idx;

ALTER TABLE sessions
	DROP COLUMN IF EXISTS bundle_version_id;

DROP TABLE IF EXISTS bundle_deployments;
DROP TABLE IF EXISTS bundle_members;

ALTER TABLE bundle_versions
	DROP CONSTRAINT IF EXISTS bundle_versions_root_member_version_id_fkey;

DROP INDEX IF EXISTS agent_versions_bundle_version_id_idx;
DROP INDEX IF EXISTS agent_versions_published_agent_version_uidx;

ALTER TABLE agent_versions
	DROP COLUMN IF EXISTS bundle_version_id,
	DROP COLUMN IF EXISTS origin;

ALTER TABLE agent_versions
	ALTER COLUMN agent_id SET NOT NULL;

ALTER TABLE agent_versions
	ADD CONSTRAINT agent_versions_agent_id_version_key UNIQUE (agent_id, version);

DROP TABLE IF EXISTS bundle_versions;
DROP TABLE IF EXISTS bundles;

UPDATE runtime_meta SET value = '17' WHERE key = 'schema_version';
