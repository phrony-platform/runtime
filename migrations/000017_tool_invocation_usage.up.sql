ALTER TABLE tool_invocations
	ADD COLUMN usage_input_tokens INT NOT NULL DEFAULT 0,
	ADD COLUMN usage_output_tokens INT NOT NULL DEFAULT 0,
	ADD COLUMN usage_estimated BOOLEAN NOT NULL DEFAULT FALSE;

INSERT INTO runtime_meta (key, value)
VALUES ('schema_version', '17')
ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value;
