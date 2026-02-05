-- ============================================================================
-- SOLSTICE THREAD EXTENSIONS MIGRATION
-- ============================================================================
-- Purpose: Add Solstice-specific columns to airborne threads tables
-- This enables Solstice to share Airborne's thread storage infrastructure
-- Run: psql -d airborne -f migrations/007_solstice_thread_extensions.sql
-- ============================================================================

-- ============================================================================
-- AI8 TENANT THREAD EXTENSIONS
-- ============================================================================

ALTER TABLE ai8_airborne_threads ADD COLUMN IF NOT EXISTS
    solstice_thread_id TEXT UNIQUE;              -- Solstice's hash-based thread ID

ALTER TABLE ai8_airborne_threads ADD COLUMN IF NOT EXISTS
    original_sender TEXT;                         -- First sender email

ALTER TABLE ai8_airborne_threads ADD COLUMN IF NOT EXISTS
    original_sender_user_id BIGINT;               -- Email4AI user ID

ALTER TABLE ai8_airborne_threads ADD COLUMN IF NOT EXISTS
    seen_recipients TEXT[];                       -- All recipients ever seen

ALTER TABLE ai8_airborne_threads ADD COLUMN IF NOT EXISTS
    has_replied BOOLEAN DEFAULT false;            -- Group conversation wake-up

ALTER TABLE ai8_airborne_threads ADD COLUMN IF NOT EXISTS
    done BOOLEAN DEFAULT false;                   -- Thread completion flag

ALTER TABLE ai8_airborne_threads ADD COLUMN IF NOT EXISTS
    revision BIGINT DEFAULT 0;                    -- Optimistic locking

ALTER TABLE ai8_airborne_threads ADD COLUMN IF NOT EXISTS
    file_search_store_id TEXT;                    -- Gemini FileSearchStore ID

ALTER TABLE ai8_airborne_threads ADD COLUMN IF NOT EXISTS
    vector_store_id TEXT;                         -- OpenAI Vector Store ID

ALTER TABLE ai8_airborne_threads ADD COLUMN IF NOT EXISTS
    response_id TEXT;                             -- OpenAI Response ID for continuity

ALTER TABLE ai8_airborne_threads ADD COLUMN IF NOT EXISTS
    last_full_response TEXT;                      -- For command system

ALTER TABLE ai8_airborne_threads ADD COLUMN IF NOT EXISTS
    conversation_history JSONB;                   -- Cross-provider history

ALTER TABLE ai8_airborne_threads ADD COLUMN IF NOT EXISTS
    process_hash TEXT;                            -- Processing state hash

ALTER TABLE ai8_airborne_threads ADD COLUMN IF NOT EXISTS
    solstice_version TEXT;                        -- Solstice version that created/updated

-- Indexes for Solstice lookups
CREATE UNIQUE INDEX IF NOT EXISTS idx_ai8_threads_solstice_id
    ON ai8_airborne_threads(solstice_thread_id)
    WHERE solstice_thread_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_ai8_threads_original_sender
    ON ai8_airborne_threads(original_sender)
    WHERE original_sender IS NOT NULL;

COMMENT ON COLUMN ai8_airborne_threads.solstice_thread_id IS 'Solstice hash-based thread ID for lookups';
COMMENT ON COLUMN ai8_airborne_threads.original_sender IS 'First sender email in thread (referral tracking)';
COMMENT ON COLUMN ai8_airborne_threads.revision IS 'Optimistic locking version (incremented on save)';
COMMENT ON COLUMN ai8_airborne_threads.conversation_history IS 'Cross-provider context for provider switching';

-- ============================================================================
-- EMAIL4AI TENANT THREAD EXTENSIONS
-- ============================================================================

ALTER TABLE email4ai_airborne_threads ADD COLUMN IF NOT EXISTS
    solstice_thread_id TEXT UNIQUE;

ALTER TABLE email4ai_airborne_threads ADD COLUMN IF NOT EXISTS
    original_sender TEXT;

ALTER TABLE email4ai_airborne_threads ADD COLUMN IF NOT EXISTS
    original_sender_user_id BIGINT;

ALTER TABLE email4ai_airborne_threads ADD COLUMN IF NOT EXISTS
    seen_recipients TEXT[];

ALTER TABLE email4ai_airborne_threads ADD COLUMN IF NOT EXISTS
    has_replied BOOLEAN DEFAULT false;

ALTER TABLE email4ai_airborne_threads ADD COLUMN IF NOT EXISTS
    done BOOLEAN DEFAULT false;

ALTER TABLE email4ai_airborne_threads ADD COLUMN IF NOT EXISTS
    revision BIGINT DEFAULT 0;

ALTER TABLE email4ai_airborne_threads ADD COLUMN IF NOT EXISTS
    file_search_store_id TEXT;

ALTER TABLE email4ai_airborne_threads ADD COLUMN IF NOT EXISTS
    vector_store_id TEXT;

ALTER TABLE email4ai_airborne_threads ADD COLUMN IF NOT EXISTS
    response_id TEXT;

ALTER TABLE email4ai_airborne_threads ADD COLUMN IF NOT EXISTS
    last_full_response TEXT;

ALTER TABLE email4ai_airborne_threads ADD COLUMN IF NOT EXISTS
    conversation_history JSONB;

ALTER TABLE email4ai_airborne_threads ADD COLUMN IF NOT EXISTS
    process_hash TEXT;

ALTER TABLE email4ai_airborne_threads ADD COLUMN IF NOT EXISTS
    solstice_version TEXT;

CREATE UNIQUE INDEX IF NOT EXISTS idx_email4ai_threads_solstice_id
    ON email4ai_airborne_threads(solstice_thread_id)
    WHERE solstice_thread_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_email4ai_threads_original_sender
    ON email4ai_airborne_threads(original_sender)
    WHERE original_sender IS NOT NULL;

COMMENT ON COLUMN email4ai_airborne_threads.solstice_thread_id IS 'Solstice hash-based thread ID for lookups';
COMMENT ON COLUMN email4ai_airborne_threads.original_sender IS 'First sender email in thread (referral tracking)';
COMMENT ON COLUMN email4ai_airborne_threads.revision IS 'Optimistic locking version (incremented on save)';
COMMENT ON COLUMN email4ai_airborne_threads.conversation_history IS 'Cross-provider context for provider switching';

-- ============================================================================
-- ZZTEST TENANT THREAD EXTENSIONS
-- ============================================================================

ALTER TABLE zztest_airborne_threads ADD COLUMN IF NOT EXISTS
    solstice_thread_id TEXT UNIQUE;

ALTER TABLE zztest_airborne_threads ADD COLUMN IF NOT EXISTS
    original_sender TEXT;

ALTER TABLE zztest_airborne_threads ADD COLUMN IF NOT EXISTS
    original_sender_user_id BIGINT;

ALTER TABLE zztest_airborne_threads ADD COLUMN IF NOT EXISTS
    seen_recipients TEXT[];

ALTER TABLE zztest_airborne_threads ADD COLUMN IF NOT EXISTS
    has_replied BOOLEAN DEFAULT false;

ALTER TABLE zztest_airborne_threads ADD COLUMN IF NOT EXISTS
    done BOOLEAN DEFAULT false;

ALTER TABLE zztest_airborne_threads ADD COLUMN IF NOT EXISTS
    revision BIGINT DEFAULT 0;

ALTER TABLE zztest_airborne_threads ADD COLUMN IF NOT EXISTS
    file_search_store_id TEXT;

ALTER TABLE zztest_airborne_threads ADD COLUMN IF NOT EXISTS
    vector_store_id TEXT;

ALTER TABLE zztest_airborne_threads ADD COLUMN IF NOT EXISTS
    response_id TEXT;

ALTER TABLE zztest_airborne_threads ADD COLUMN IF NOT EXISTS
    last_full_response TEXT;

ALTER TABLE zztest_airborne_threads ADD COLUMN IF NOT EXISTS
    conversation_history JSONB;

ALTER TABLE zztest_airborne_threads ADD COLUMN IF NOT EXISTS
    process_hash TEXT;

ALTER TABLE zztest_airborne_threads ADD COLUMN IF NOT EXISTS
    solstice_version TEXT;

CREATE UNIQUE INDEX IF NOT EXISTS idx_zztest_threads_solstice_id
    ON zztest_airborne_threads(solstice_thread_id)
    WHERE solstice_thread_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_zztest_threads_original_sender
    ON zztest_airborne_threads(original_sender)
    WHERE original_sender IS NOT NULL;

COMMENT ON COLUMN zztest_airborne_threads.solstice_thread_id IS 'Solstice hash-based thread ID for lookups';
COMMENT ON COLUMN zztest_airborne_threads.original_sender IS 'First sender email in thread (referral tracking)';
COMMENT ON COLUMN zztest_airborne_threads.revision IS 'Optimistic locking version (incremented on save)';
COMMENT ON COLUMN zztest_airborne_threads.conversation_history IS 'Cross-provider context for provider switching';

-- ============================================================================
-- ROLLBACK INSTRUCTIONS
-- ============================================================================
-- To rollback this migration:
-- 1. DROP INDEX IF EXISTS idx_ai8_threads_solstice_id;
-- 2. DROP INDEX IF EXISTS idx_ai8_threads_original_sender;
-- 3. DROP INDEX IF EXISTS idx_email4ai_threads_solstice_id;
-- 4. DROP INDEX IF EXISTS idx_email4ai_threads_original_sender;
-- 5. DROP INDEX IF EXISTS idx_zztest_threads_solstice_id;
-- 6. DROP INDEX IF EXISTS idx_zztest_threads_original_sender;
-- 7. ALTER TABLE ai8_airborne_threads DROP COLUMN IF EXISTS solstice_thread_id, ...;
-- 8. ALTER TABLE email4ai_airborne_threads DROP COLUMN IF EXISTS solstice_thread_id, ...;
-- 9. ALTER TABLE zztest_airborne_threads DROP COLUMN IF EXISTS solstice_thread_id, ...;
