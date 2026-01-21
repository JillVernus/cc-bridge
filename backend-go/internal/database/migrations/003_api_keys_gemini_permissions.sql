-- Add Gemini channel allowlist support for API keys
-- New column: allowed_channels_gemini (JSON-encoded int array)

ALTER TABLE api_keys ADD COLUMN allowed_channels_gemini TEXT;

