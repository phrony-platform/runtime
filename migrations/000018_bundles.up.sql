CREATE TABLE bundles (
	id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
	namespace TEXT NOT NULL,
	name TEXT NOT NULL,
	owner TEXT NOT NULL DEFAULT '',
	labels JSONB NOT NULL DEFAULT '{}'::jsonb,
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	UNIQUE (namespace, name)
);

CREATE TABLE bundle_versions (
	id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
	bundle_id UUID NOT NULL REFERENCES bundles (id) ON DELETE CASCADE,
	lock_hash TEXT NOT NULL,
	lock JSONB NOT NULL,
	root_member_version_id UUID,
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	UNIQUE (bundle_id, lock_hash)
);

CREATE INDEX bundle_versions_bundle_id_idx ON bundle_versions (bundle_id);

ALTER TABLE agent_versions
	ADD COLUMN origin TEXT NOT NULL DEFAULT 'published' CHECK (origin IN ('published', 'vendored')),
	ADD COLUMN bundle_version_id UUID REFERENCES bundle_versions (id) ON DELETE CASCADE;

ALTER TABLE agent_versions
	ALTER COLUMN agent_id DROP NOT NULL;

ALTER TABLE agent_versions
	DROP CONSTRAINT agent_versions_agent_id_version_key;

CREATE UNIQUE INDEX agent_versions_published_agent_version_uidx
	ON agent_versions (agent_id, version)
	WHERE agent_id IS NOT NULL;

CREATE INDEX agent_versions_bundle_version_id_idx ON agent_versions (bundle_version_id)
	WHERE bundle_version_id IS NOT NULL;

ALTER TABLE bundle_versions
	ADD CONSTRAINT bundle_versions_root_member_version_id_fkey
	FOREIGN KEY (root_member_version_id) REFERENCES agent_versions (id) ON DELETE RESTRICT;

CREATE TABLE bundle_members (
	bundle_version_id UUID NOT NULL REFERENCES bundle_versions (id) ON DELETE CASCADE,
	child_name TEXT NOT NULL,
	member_version_id UUID NOT NULL REFERENCES agent_versions (id) ON DELETE RESTRICT,
	ref TEXT NOT NULL DEFAULT '',
	origin TEXT NOT NULL CHECK (origin IN ('vendored', 'external')),
	is_root BOOLEAN NOT NULL DEFAULT FALSE,
	PRIMARY KEY (bundle_version_id, child_name)
);

CREATE INDEX bundle_members_member_version_id_idx ON bundle_members (member_version_id);

CREATE TABLE bundle_deployments (
	id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
	bundle_id UUID NOT NULL REFERENCES bundles (id) ON DELETE CASCADE,
	bundle_version_id UUID NOT NULL REFERENCES bundle_versions (id) ON DELETE CASCADE,
	action TEXT NOT NULL CHECK (action IN ('deploy', 'rollback')),
	actor TEXT NOT NULL DEFAULT '',
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX bundle_deployments_bundle_id_created_at_idx ON bundle_deployments (bundle_id, created_at DESC);

ALTER TABLE sessions
	ADD COLUMN bundle_version_id UUID REFERENCES bundle_versions (id) ON DELETE RESTRICT;

CREATE INDEX sessions_bundle_version_id_idx ON sessions (bundle_version_id)
	WHERE bundle_version_id IS NOT NULL;

INSERT INTO runtime_meta (key, value)
VALUES ('schema_version', '18')
ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value;
