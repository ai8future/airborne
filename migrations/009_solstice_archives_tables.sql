-- ============================================================================
-- SOLSTICE ARCHIVES TABLES MIGRATION
-- ============================================================================
-- Purpose: Create archives tables for email retry storage
-- Archives store the RAW INBOUND EMAIL (not AI conversation) for retry/reprocessing
-- failed emails. This is separate from messages/threads which track AI conversation.
--
-- | Archive               | Messages/Threads          |
-- |-----------------------|---------------------------|
-- | Raw email data        | AI conversation           |
-- | Retry failed emails   | Conversation continuity   |
-- | Filesystem attachments| Token/cost tracking       |
-- | 72h TTL (debugging)   | Permanent record          |
--
-- Run: psql -d airborne -f migrations/009_solstice_archives_tables.sql
-- ============================================================================

-- ============================================================================
-- AI8 TENANT ARCHIVES TABLE
-- ============================================================================

CREATE TABLE IF NOT EXISTS ai8_airborne_archives (
    id                  UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    message_id          TEXT NOT NULL UNIQUE,       -- Email MessageID (unique constraint)
    project             TEXT NOT NULL,              -- Solstice project code
    from_email          TEXT NOT NULL,
    to_email            TEXT NOT NULL,
    cc                  TEXT,
    subject             TEXT NOT NULL,
    text_body           TEXT,
    html_body           TEXT,
    headers             TEXT,                       -- Raw email headers
    envelope            TEXT,                       -- SMTP envelope data
    raw_form_values     JSONB,                      -- Raw webhook form values
    in_reply_to         TEXT,                       -- Threading header
    references_header   TEXT,                       -- Threading header
    archived_at         TIMESTAMPTZ DEFAULT NOW(),
    processed_at        TIMESTAMPTZ,
    status              TEXT DEFAULT 'pending',     -- pending, success, failed
    thread_id           UUID,                       -- Link to thread if processed
    attachment_paths    TEXT[],                     -- Filesystem paths to archived attachments
    attachment_count    INT DEFAULT 0,
    error               TEXT,
    retry_count         INT DEFAULT 0,
    created_at          TIMESTAMPTZ DEFAULT NOW(),
    updated_at          TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_ai8_archives_status
    ON ai8_airborne_archives(status);

CREATE INDEX IF NOT EXISTS idx_ai8_archives_archived
    ON ai8_airborne_archives(archived_at DESC);

CREATE INDEX IF NOT EXISTS idx_ai8_archives_project
    ON ai8_airborne_archives(project);

CREATE INDEX IF NOT EXISTS idx_ai8_archives_from_email
    ON ai8_airborne_archives(from_email);

CREATE INDEX IF NOT EXISTS idx_ai8_archives_pending_retry
    ON ai8_airborne_archives(status, retry_count)
    WHERE status = 'failed';

COMMENT ON TABLE ai8_airborne_archives IS 'AI8 tenant email archive for retry/debugging';
COMMENT ON COLUMN ai8_airborne_archives.attachment_paths IS 'Filesystem paths where attachments are stored';
COMMENT ON COLUMN ai8_airborne_archives.raw_form_values IS 'Original webhook form data for full reconstruction';

-- ============================================================================
-- EMAIL4AI TENANT ARCHIVES TABLE
-- ============================================================================

CREATE TABLE IF NOT EXISTS email4ai_airborne_archives (
    id                  UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    message_id          TEXT NOT NULL UNIQUE,
    project             TEXT NOT NULL,
    from_email          TEXT NOT NULL,
    to_email            TEXT NOT NULL,
    cc                  TEXT,
    subject             TEXT NOT NULL,
    text_body           TEXT,
    html_body           TEXT,
    headers             TEXT,
    envelope            TEXT,
    raw_form_values     JSONB,
    in_reply_to         TEXT,
    references_header   TEXT,
    archived_at         TIMESTAMPTZ DEFAULT NOW(),
    processed_at        TIMESTAMPTZ,
    status              TEXT DEFAULT 'pending',
    thread_id           UUID,
    attachment_paths    TEXT[],
    attachment_count    INT DEFAULT 0,
    error               TEXT,
    retry_count         INT DEFAULT 0,
    created_at          TIMESTAMPTZ DEFAULT NOW(),
    updated_at          TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_email4ai_archives_status
    ON email4ai_airborne_archives(status);

CREATE INDEX IF NOT EXISTS idx_email4ai_archives_archived
    ON email4ai_airborne_archives(archived_at DESC);

CREATE INDEX IF NOT EXISTS idx_email4ai_archives_project
    ON email4ai_airborne_archives(project);

CREATE INDEX IF NOT EXISTS idx_email4ai_archives_from_email
    ON email4ai_airborne_archives(from_email);

CREATE INDEX IF NOT EXISTS idx_email4ai_archives_pending_retry
    ON email4ai_airborne_archives(status, retry_count)
    WHERE status = 'failed';

COMMENT ON TABLE email4ai_airborne_archives IS 'Email4AI tenant email archive for retry/debugging';

-- ============================================================================
-- ZZTEST TENANT ARCHIVES TABLE
-- ============================================================================

CREATE TABLE IF NOT EXISTS zztest_airborne_archives (
    id                  UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    message_id          TEXT NOT NULL UNIQUE,
    project             TEXT NOT NULL,
    from_email          TEXT NOT NULL,
    to_email            TEXT NOT NULL,
    cc                  TEXT,
    subject             TEXT NOT NULL,
    text_body           TEXT,
    html_body           TEXT,
    headers             TEXT,
    envelope            TEXT,
    raw_form_values     JSONB,
    in_reply_to         TEXT,
    references_header   TEXT,
    archived_at         TIMESTAMPTZ DEFAULT NOW(),
    processed_at        TIMESTAMPTZ,
    status              TEXT DEFAULT 'pending',
    thread_id           UUID,
    attachment_paths    TEXT[],
    attachment_count    INT DEFAULT 0,
    error               TEXT,
    retry_count         INT DEFAULT 0,
    created_at          TIMESTAMPTZ DEFAULT NOW(),
    updated_at          TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_zztest_archives_status
    ON zztest_airborne_archives(status);

CREATE INDEX IF NOT EXISTS idx_zztest_archives_archived
    ON zztest_airborne_archives(archived_at DESC);

CREATE INDEX IF NOT EXISTS idx_zztest_archives_project
    ON zztest_airborne_archives(project);

CREATE INDEX IF NOT EXISTS idx_zztest_archives_from_email
    ON zztest_airborne_archives(from_email);

CREATE INDEX IF NOT EXISTS idx_zztest_archives_pending_retry
    ON zztest_airborne_archives(status, retry_count)
    WHERE status = 'failed';

COMMENT ON TABLE zztest_airborne_archives IS 'Test tenant email archive for retry/debugging';

-- ============================================================================
-- TRIGGERS FOR AUTO-UPDATING updated_at
-- ============================================================================

CREATE OR REPLACE FUNCTION ai8_update_archive_timestamp()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trigger_ai8_archive_updated ON ai8_airborne_archives;
CREATE TRIGGER trigger_ai8_archive_updated
    BEFORE UPDATE ON ai8_airborne_archives
    FOR EACH ROW
    EXECUTE FUNCTION ai8_update_archive_timestamp();

CREATE OR REPLACE FUNCTION email4ai_update_archive_timestamp()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trigger_email4ai_archive_updated ON email4ai_airborne_archives;
CREATE TRIGGER trigger_email4ai_archive_updated
    BEFORE UPDATE ON email4ai_airborne_archives
    FOR EACH ROW
    EXECUTE FUNCTION email4ai_update_archive_timestamp();

CREATE OR REPLACE FUNCTION zztest_update_archive_timestamp()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trigger_zztest_archive_updated ON zztest_airborne_archives;
CREATE TRIGGER trigger_zztest_archive_updated
    BEFORE UPDATE ON zztest_airborne_archives
    FOR EACH ROW
    EXECUTE FUNCTION zztest_update_archive_timestamp();

-- ============================================================================
-- ROLLBACK INSTRUCTIONS
-- ============================================================================
-- To rollback this migration:
-- 1. DROP TRIGGER IF EXISTS trigger_ai8_archive_updated ON ai8_airborne_archives;
-- 2. DROP TRIGGER IF EXISTS trigger_email4ai_archive_updated ON email4ai_airborne_archives;
-- 3. DROP TRIGGER IF EXISTS trigger_zztest_archive_updated ON zztest_airborne_archives;
-- 4. DROP FUNCTION IF EXISTS ai8_update_archive_timestamp();
-- 5. DROP FUNCTION IF EXISTS email4ai_update_archive_timestamp();
-- 6. DROP FUNCTION IF EXISTS zztest_update_archive_timestamp();
-- 7. DROP TABLE IF EXISTS ai8_airborne_archives;
-- 8. DROP TABLE IF EXISTS email4ai_airborne_archives;
-- 9. DROP TABLE IF EXISTS zztest_airborne_archives;
