ALTER TABLE tool_invocations
	DROP COLUMN usage_input_tokens,
	DROP COLUMN usage_output_tokens,
	DROP COLUMN usage_estimated;

INSERT INTO runtime_meta (key, value)
VALUES ('schema_version', '16')
ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value;
