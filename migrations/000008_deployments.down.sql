DROP TABLE IF EXISTS deployments;

ALTER TABLE agent_versions
	DROP COLUMN IF EXISTS retired_at;

UPDATE runtime_meta SET value = '7' WHERE key = 'schema_version';
