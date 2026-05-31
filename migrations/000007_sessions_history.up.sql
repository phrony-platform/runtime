ALTER TABLE sessions
	ADD COLUMN history JSONB NOT NULL DEFAULT '[]'::jsonb;

INSERT INTO runtime_meta (key, value)
VALUES ('schema_version', '7')
ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value;
