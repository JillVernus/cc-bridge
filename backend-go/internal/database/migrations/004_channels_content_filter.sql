-- Migration 004: Add content filter storage for channels
-- Stores per-channel content filter configuration as JSON

ALTER TABLE channels ADD COLUMN content_filter TEXT;
