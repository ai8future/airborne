-- ============================================================================
-- SOLSTICE JOBS TABLES MIGRATION
-- ============================================================================
-- Purpose: Create jobs tables for crash-proof email processing
-- Jobs provide durability: before returning 200 OK to Postmark, email data
-- is persisted to R2 (encrypted) with the index in Supabase. If the server
-- crashes mid-processing, the recovery worker resumes from R2.
--
-- Crypto-shredding: Each job has a unique encryption key. Deleting the
-- Supabase row removes the key, making R2 data cryptographically unrecoverable.
--
-- Run: psql -d airborne -f migrations/008_solstice_jobs_tables.sql
-- ============================================================================

-- ============================================================================
-- AI8 TENANT JOBS TABLE
-- ============================================================================

CREATE TABLE IF NOT EXISTS ai8_airborne_jobs (
    id                  TEXT PRIMARY KEY,           -- Email MessageID
    project             TEXT NOT NULL,              -- Solstice project code
    status              TEXT NOT NULL DEFAULT 'pending', -- pending, processing, complete, failed
    received_at         TIMESTAMPTZ DEFAULT NOW(),
    processing_at       TIMESTAMPTZ,
    completed_at        TIMESTAMPTZ,
    r2_prefix           TEXT,                       -- R2 path for encrypted email data
    encryption_key      TEXT,                       -- AES key (deleting = crypto-shred)
    origin_host         TEXT,                       -- For local cache optimization
    local_path          TEXT,                       -- Local cache path
    retry_count         INT DEFAULT 0,
    error               TEXT,
    from_email          TEXT,
    subject             TEXT,
    created_at          TIMESTAMPTZ DEFAULT NOW(),
    updated_at          TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_ai8_jobs_status
    ON ai8_airborne_jobs(status);

CREATE INDEX IF NOT EXISTS idx_ai8_jobs_received
    ON ai8_airborne_jobs(received_at);

CREATE INDEX IF NOT EXISTS idx_ai8_jobs_stuck
    ON ai8_airborne_jobs(processing_at)
    WHERE status = 'processing';

CREATE INDEX IF NOT EXISTS idx_ai8_jobs_project
    ON ai8_airborne_jobs(project);

COMMENT ON TABLE ai8_airborne_jobs IS 'AI8 tenant crash-proof job queue (index only, data in R2)';
COMMENT ON COLUMN ai8_airborne_jobs.encryption_key IS 'Per-job AES key - delete row to crypto-shred R2 data';
COMMENT ON COLUMN ai8_airborne_jobs.r2_prefix IS 'R2 object prefix for encrypted email/attachment data';

-- ============================================================================
-- EMAIL4AI TENANT JOBS TABLE
-- ============================================================================

CREATE TABLE IF NOT EXISTS email4ai_airborne_jobs (
    id                  TEXT PRIMARY KEY,
    project             TEXT NOT NULL,
    status              TEXT NOT NULL DEFAULT 'pending',
    received_at         TIMESTAMPTZ DEFAULT NOW(),
    processing_at       TIMESTAMPTZ,
    completed_at        TIMESTAMPTZ,
    r2_prefix           TEXT,
    encryption_key      TEXT,
    origin_host         TEXT,
    local_path          TEXT,
    retry_count         INT DEFAULT 0,
    error               TEXT,
    from_email          TEXT,
    subject             TEXT,
    created_at          TIMESTAMPTZ DEFAULT NOW(),
    updated_at          TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_email4ai_jobs_status
    ON email4ai_airborne_jobs(status);

CREATE INDEX IF NOT EXISTS idx_email4ai_jobs_received
    ON email4ai_airborne_jobs(received_at);

CREATE INDEX IF NOT EXISTS idx_email4ai_jobs_stuck
    ON email4ai_airborne_jobs(processing_at)
    WHERE status = 'processing';

CREATE INDEX IF NOT EXISTS idx_email4ai_jobs_project
    ON email4ai_airborne_jobs(project);

COMMENT ON TABLE email4ai_airborne_jobs IS 'Email4AI tenant crash-proof job queue';
COMMENT ON COLUMN email4ai_airborne_jobs.encryption_key IS 'Per-job AES key - delete row to crypto-shred R2 data';

-- ============================================================================
-- ZZTEST TENANT JOBS TABLE
-- ============================================================================

CREATE TABLE IF NOT EXISTS zztest_airborne_jobs (
    id                  TEXT PRIMARY KEY,
    project             TEXT NOT NULL,
    status              TEXT NOT NULL DEFAULT 'pending',
    received_at         TIMESTAMPTZ DEFAULT NOW(),
    processing_at       TIMESTAMPTZ,
    completed_at        TIMESTAMPTZ,
    r2_prefix           TEXT,
    encryption_key      TEXT,
    origin_host         TEXT,
    local_path          TEXT,
    retry_count         INT DEFAULT 0,
    error               TEXT,
    from_email          TEXT,
    subject             TEXT,
    created_at          TIMESTAMPTZ DEFAULT NOW(),
    updated_at          TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_zztest_jobs_status
    ON zztest_airborne_jobs(status);

CREATE INDEX IF NOT EXISTS idx_zztest_jobs_received
    ON zztest_airborne_jobs(received_at);

CREATE INDEX IF NOT EXISTS idx_zztest_jobs_stuck
    ON zztest_airborne_jobs(processing_at)
    WHERE status = 'processing';

CREATE INDEX IF NOT EXISTS idx_zztest_jobs_project
    ON zztest_airborne_jobs(project);

COMMENT ON TABLE zztest_airborne_jobs IS 'Test tenant crash-proof job queue';
COMMENT ON COLUMN zztest_airborne_jobs.encryption_key IS 'Per-job AES key - delete row to crypto-shred R2 data';

-- ============================================================================
-- TRIGGERS FOR AUTO-UPDATING updated_at
-- ============================================================================

CREATE OR REPLACE FUNCTION ai8_update_job_timestamp()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trigger_ai8_job_updated ON ai8_airborne_jobs;
CREATE TRIGGER trigger_ai8_job_updated
    BEFORE UPDATE ON ai8_airborne_jobs
    FOR EACH ROW
    EXECUTE FUNCTION ai8_update_job_timestamp();

CREATE OR REPLACE FUNCTION email4ai_update_job_timestamp()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trigger_email4ai_job_updated ON email4ai_airborne_jobs;
CREATE TRIGGER trigger_email4ai_job_updated
    BEFORE UPDATE ON email4ai_airborne_jobs
    FOR EACH ROW
    EXECUTE FUNCTION email4ai_update_job_timestamp();

CREATE OR REPLACE FUNCTION zztest_update_job_timestamp()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trigger_zztest_job_updated ON zztest_airborne_jobs;
CREATE TRIGGER trigger_zztest_job_updated
    BEFORE UPDATE ON zztest_airborne_jobs
    FOR EACH ROW
    EXECUTE FUNCTION zztest_update_job_timestamp();

-- ============================================================================
-- ROLLBACK INSTRUCTIONS
-- ============================================================================
-- To rollback this migration:
-- 1. DROP TRIGGER IF EXISTS trigger_ai8_job_updated ON ai8_airborne_jobs;
-- 2. DROP TRIGGER IF EXISTS trigger_email4ai_job_updated ON email4ai_airborne_jobs;
-- 3. DROP TRIGGER IF EXISTS trigger_zztest_job_updated ON zztest_airborne_jobs;
-- 4. DROP FUNCTION IF EXISTS ai8_update_job_timestamp();
-- 5. DROP FUNCTION IF EXISTS email4ai_update_job_timestamp();
-- 6. DROP FUNCTION IF EXISTS zztest_update_job_timestamp();
-- 7. DROP TABLE IF EXISTS ai8_airborne_jobs;
-- 8. DROP TABLE IF EXISTS email4ai_airborne_jobs;
-- 9. DROP TABLE IF EXISTS zztest_airborne_jobs;
