DROP TABLE IF EXISTS session_evidence;

INSERT INTO runtime_meta (key, value)
VALUES ('schema_version', '10')
ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value;
