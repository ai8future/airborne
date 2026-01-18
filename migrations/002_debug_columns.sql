-- ============================================================================
-- AIRBORNE DEBUG COLUMNS MIGRATION
-- ============================================================================
-- Purpose: Add columns for request/response debugging in admin dashboard
-- Run: psql -d airborne -f migrations/002_debug_columns.sql
-- ============================================================================

-- Add debug columns to messages table
ALTER TABLE airborne_messages
    ADD COLUMN IF NOT EXISTS system_prompt TEXT,
    ADD COLUMN IF NOT EXISTS raw_request_json JSONB,
    ADD COLUMN IF NOT EXISTS raw_response_json JSONB;

-- Comments for documentation
COMMENT ON COLUMN airborne_messages.system_prompt IS 'System prompt sent with the request';
COMMENT ON COLUMN airborne_messages.raw_request_json IS 'Raw HTTP request body sent to LLM provider';
COMMENT ON COLUMN airborne_messages.raw_response_json IS 'Raw HTTP response body from LLM provider';

-- Index for finding messages with debug data
CREATE INDEX IF NOT EXISTS idx_messages_has_debug
    ON airborne_messages(created_at DESC)
    WHERE raw_request_json IS NOT NULL;
