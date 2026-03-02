-- Migration 007: add first-token timing fields to request_logs
-- first_token_time is nullable (non-stream requests may not have first-token timing)
-- first_token_duration_ms stores milliseconds from request fire to first token

ALTER TABLE request_logs ADD COLUMN first_token_time DATETIME;
ALTER TABLE request_logs ADD COLUMN first_token_duration_ms INTEGER DEFAULT 0;
