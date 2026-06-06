DROP INDEX IF EXISTS bundle_versions_bundle_id_version_uidx;

ALTER TABLE bundle_versions DROP COLUMN IF EXISTS version;

UPDATE runtime_meta SET value = '18' WHERE key = 'schema_version';
