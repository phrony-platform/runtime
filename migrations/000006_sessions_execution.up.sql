ALTER TABLE sessions
	ADD COLUMN output JSONB,
	ADD COLUMN error TEXT;

INSERT INTO runtime_meta (key, value)
VALUES ('schema_version', '6')
ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value;
