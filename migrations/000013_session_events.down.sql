DROP TABLE IF EXISTS session_events;

INSERT INTO runtime_meta (key, value)
VALUES ('schema_version', '12')
ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value;
