-- Migration 005: Add stable channel_uid linkage to request_logs
-- request_logs.channel_id is an index and can change after reorder.
-- channel_uid stores the stable config channel ID for reliable linkage.

ALTER TABLE request_logs ADD COLUMN channel_uid TEXT;
CREATE INDEX IF NOT EXISTS idx_request_logs_channel_uid ON request_logs(channel_uid);

-- Best-effort backfill for historical rows:
-- Map endpoint -> channel_type and set channel_uid when channel_name uniquely identifies a channel.
UPDATE request_logs
SET channel_uid = (
    SELECT c.channel_id
    FROM channels c
    WHERE c.channel_type = CASE
        WHEN request_logs.endpoint = '/v1/responses' THEN 'responses'
        WHEN request_logs.endpoint = '/v1/gemini' THEN 'gemini'
        ELSE 'messages'
    END
    AND LOWER(c.name) = LOWER(request_logs.channel_name)
    LIMIT 1
)
WHERE (channel_uid IS NULL OR TRIM(channel_uid) = '')
  AND channel_name IS NOT NULL
  AND TRIM(channel_name) != ''
  AND (
      SELECT COUNT(*)
      FROM channels c2
      WHERE c2.channel_type = CASE
          WHEN request_logs.endpoint = '/v1/responses' THEN 'responses'
          WHEN request_logs.endpoint = '/v1/gemini' THEN 'gemini'
          ELSE 'messages'
      END
      AND LOWER(c2.name) = LOWER(request_logs.channel_name)
  ) = 1;
