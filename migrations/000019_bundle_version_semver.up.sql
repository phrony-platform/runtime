ALTER TABLE bundle_versions ADD COLUMN version TEXT NOT NULL;

CREATE UNIQUE INDEX bundle_versions_bundle_id_version_uidx
	ON bundle_versions (bundle_id, version);

INSERT INTO runtime_meta (key, value)
VALUES ('schema_version', '19')
ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value;
