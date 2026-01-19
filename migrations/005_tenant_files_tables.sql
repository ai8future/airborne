-- ============================================================================
-- AIRBORNE TENANT-PREFIXED FILES TABLES MIGRATION
-- ============================================================================
-- Purpose: Add file-related tables for each tenant
-- Tables: files, file_provider_uploads, thread_vector_stores
-- Run: psql -d airborne -f migrations/005_tenant_files_tables.sql
-- ============================================================================

-- ============================================================================
-- AI8 TENANT FILE TABLES
-- ============================================================================

-- ----------------------------------------------------------------------------
-- AI8 FILES: Uploaded files for RAG and attachments
-- ----------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS ai8_airborne_files (
    id              UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id         TEXT NOT NULL,
    filename        TEXT NOT NULL,
    mime_type       TEXT,
    size_bytes      BIGINT,
    store_id        TEXT,                           -- Vector store ID for RAG
    file_id         TEXT,                           -- Provider file ID (if uploaded to provider)
    provider        TEXT,                           -- Provider that owns the file
    status          TEXT DEFAULT 'uploaded',        -- uploaded, processing, ready, failed
    created_at      TIMESTAMPTZ DEFAULT NOW(),
    metadata        JSONB
);

CREATE INDEX IF NOT EXISTS idx_ai8_files_user ON ai8_airborne_files(user_id);
CREATE INDEX IF NOT EXISTS idx_ai8_files_store ON ai8_airborne_files(store_id);
CREATE INDEX IF NOT EXISTS idx_ai8_files_status ON ai8_airborne_files(status);
CREATE INDEX IF NOT EXISTS idx_ai8_files_created ON ai8_airborne_files(created_at DESC);

COMMENT ON TABLE ai8_airborne_files IS 'AI8 tenant uploaded files';
COMMENT ON COLUMN ai8_airborne_files.store_id IS 'Vector store ID for RAG retrieval';
COMMENT ON COLUMN ai8_airborne_files.file_id IS 'Provider-assigned file ID';

-- ----------------------------------------------------------------------------
-- AI8 FILE PROVIDER UPLOADS: Track file uploads to different providers
-- ----------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS ai8_airborne_file_provider_uploads (
    id                  UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    file_id             UUID REFERENCES ai8_airborne_files(id) ON DELETE CASCADE,
    provider            TEXT NOT NULL,              -- openai, gemini, etc.
    provider_file_id    TEXT,                       -- Provider's file ID
    provider_store_id   TEXT,                       -- Provider's vector store ID
    status              TEXT DEFAULT 'pending',     -- pending, uploading, ready, failed
    created_at          TIMESTAMPTZ DEFAULT NOW(),
    uploaded_at         TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_ai8_file_uploads_file ON ai8_airborne_file_provider_uploads(file_id);
CREATE INDEX IF NOT EXISTS idx_ai8_file_uploads_provider ON ai8_airborne_file_provider_uploads(provider);
CREATE INDEX IF NOT EXISTS idx_ai8_file_uploads_status ON ai8_airborne_file_provider_uploads(status);

COMMENT ON TABLE ai8_airborne_file_provider_uploads IS 'AI8 tenant file uploads to AI providers';

-- ----------------------------------------------------------------------------
-- AI8 THREAD VECTOR STORES: Link threads to vector stores for RAG
-- ----------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS ai8_airborne_thread_vector_stores (
    id              UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    thread_id       UUID REFERENCES ai8_airborne_threads(id) ON DELETE CASCADE,
    store_id        TEXT NOT NULL,
    provider        TEXT NOT NULL,                  -- openai, qdrant, etc.
    enabled         BOOLEAN DEFAULT true,
    created_at      TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_ai8_thread_stores_thread ON ai8_airborne_thread_vector_stores(thread_id);
CREATE INDEX IF NOT EXISTS idx_ai8_thread_stores_store ON ai8_airborne_thread_vector_stores(store_id);
CREATE INDEX IF NOT EXISTS idx_ai8_thread_stores_enabled ON ai8_airborne_thread_vector_stores(enabled) WHERE enabled = true;

COMMENT ON TABLE ai8_airborne_thread_vector_stores IS 'AI8 tenant thread-to-vector-store associations';

-- ============================================================================
-- EMAIL4AI TENANT FILE TABLES
-- ============================================================================

-- ----------------------------------------------------------------------------
-- EMAIL4AI FILES: Uploaded files for RAG and attachments
-- ----------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS email4ai_airborne_files (
    id              UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id         TEXT NOT NULL,
    filename        TEXT NOT NULL,
    mime_type       TEXT,
    size_bytes      BIGINT,
    store_id        TEXT,
    file_id         TEXT,
    provider        TEXT,
    status          TEXT DEFAULT 'uploaded',
    created_at      TIMESTAMPTZ DEFAULT NOW(),
    metadata        JSONB
);

CREATE INDEX IF NOT EXISTS idx_email4ai_files_user ON email4ai_airborne_files(user_id);
CREATE INDEX IF NOT EXISTS idx_email4ai_files_store ON email4ai_airborne_files(store_id);
CREATE INDEX IF NOT EXISTS idx_email4ai_files_status ON email4ai_airborne_files(status);
CREATE INDEX IF NOT EXISTS idx_email4ai_files_created ON email4ai_airborne_files(created_at DESC);

COMMENT ON TABLE email4ai_airborne_files IS 'Email4AI tenant uploaded files';

-- ----------------------------------------------------------------------------
-- EMAIL4AI FILE PROVIDER UPLOADS
-- ----------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS email4ai_airborne_file_provider_uploads (
    id                  UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    file_id             UUID REFERENCES email4ai_airborne_files(id) ON DELETE CASCADE,
    provider            TEXT NOT NULL,
    provider_file_id    TEXT,
    provider_store_id   TEXT,
    status              TEXT DEFAULT 'pending',
    created_at          TIMESTAMPTZ DEFAULT NOW(),
    uploaded_at         TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_email4ai_file_uploads_file ON email4ai_airborne_file_provider_uploads(file_id);
CREATE INDEX IF NOT EXISTS idx_email4ai_file_uploads_provider ON email4ai_airborne_file_provider_uploads(provider);
CREATE INDEX IF NOT EXISTS idx_email4ai_file_uploads_status ON email4ai_airborne_file_provider_uploads(status);

COMMENT ON TABLE email4ai_airborne_file_provider_uploads IS 'Email4AI tenant file uploads to AI providers';

-- ----------------------------------------------------------------------------
-- EMAIL4AI THREAD VECTOR STORES
-- ----------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS email4ai_airborne_thread_vector_stores (
    id              UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    thread_id       UUID REFERENCES email4ai_airborne_threads(id) ON DELETE CASCADE,
    store_id        TEXT NOT NULL,
    provider        TEXT NOT NULL,
    enabled         BOOLEAN DEFAULT true,
    created_at      TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_email4ai_thread_stores_thread ON email4ai_airborne_thread_vector_stores(thread_id);
CREATE INDEX IF NOT EXISTS idx_email4ai_thread_stores_store ON email4ai_airborne_thread_vector_stores(store_id);
CREATE INDEX IF NOT EXISTS idx_email4ai_thread_stores_enabled ON email4ai_airborne_thread_vector_stores(enabled) WHERE enabled = true;

COMMENT ON TABLE email4ai_airborne_thread_vector_stores IS 'Email4AI tenant thread-to-vector-store associations';

-- ============================================================================
-- ZZTEST TENANT FILE TABLES
-- ============================================================================

-- ----------------------------------------------------------------------------
-- ZZTEST FILES: Uploaded files for RAG and attachments
-- ----------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS zztest_airborne_files (
    id              UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id         TEXT NOT NULL,
    filename        TEXT NOT NULL,
    mime_type       TEXT,
    size_bytes      BIGINT,
    store_id        TEXT,
    file_id         TEXT,
    provider        TEXT,
    status          TEXT DEFAULT 'uploaded',
    created_at      TIMESTAMPTZ DEFAULT NOW(),
    metadata        JSONB
);

CREATE INDEX IF NOT EXISTS idx_zztest_files_user ON zztest_airborne_files(user_id);
CREATE INDEX IF NOT EXISTS idx_zztest_files_store ON zztest_airborne_files(store_id);
CREATE INDEX IF NOT EXISTS idx_zztest_files_status ON zztest_airborne_files(status);
CREATE INDEX IF NOT EXISTS idx_zztest_files_created ON zztest_airborne_files(created_at DESC);

COMMENT ON TABLE zztest_airborne_files IS 'Test tenant uploaded files';

-- ----------------------------------------------------------------------------
-- ZZTEST FILE PROVIDER UPLOADS
-- ----------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS zztest_airborne_file_provider_uploads (
    id                  UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    file_id             UUID REFERENCES zztest_airborne_files(id) ON DELETE CASCADE,
    provider            TEXT NOT NULL,
    provider_file_id    TEXT,
    provider_store_id   TEXT,
    status              TEXT DEFAULT 'pending',
    created_at          TIMESTAMPTZ DEFAULT NOW(),
    uploaded_at         TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_zztest_file_uploads_file ON zztest_airborne_file_provider_uploads(file_id);
CREATE INDEX IF NOT EXISTS idx_zztest_file_uploads_provider ON zztest_airborne_file_provider_uploads(provider);
CREATE INDEX IF NOT EXISTS idx_zztest_file_uploads_status ON zztest_airborne_file_provider_uploads(status);

COMMENT ON TABLE zztest_airborne_file_provider_uploads IS 'Test tenant file uploads to AI providers';

-- ----------------------------------------------------------------------------
-- ZZTEST THREAD VECTOR STORES
-- ----------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS zztest_airborne_thread_vector_stores (
    id              UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    thread_id       UUID REFERENCES zztest_airborne_threads(id) ON DELETE CASCADE,
    store_id        TEXT NOT NULL,
    provider        TEXT NOT NULL,
    enabled         BOOLEAN DEFAULT true,
    created_at      TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_zztest_thread_stores_thread ON zztest_airborne_thread_vector_stores(thread_id);
CREATE INDEX IF NOT EXISTS idx_zztest_thread_stores_store ON zztest_airborne_thread_vector_stores(store_id);
CREATE INDEX IF NOT EXISTS idx_zztest_thread_stores_enabled ON zztest_airborne_thread_vector_stores(enabled) WHERE enabled = true;

COMMENT ON TABLE zztest_airborne_thread_vector_stores IS 'Test tenant thread-to-vector-store associations';

-- ============================================================================
-- ROLLBACK INSTRUCTIONS
-- ============================================================================
-- To rollback this migration:
-- DROP TABLE IF EXISTS ai8_airborne_thread_vector_stores;
-- DROP TABLE IF EXISTS ai8_airborne_file_provider_uploads;
-- DROP TABLE IF EXISTS ai8_airborne_files;
-- DROP TABLE IF EXISTS email4ai_airborne_thread_vector_stores;
-- DROP TABLE IF EXISTS email4ai_airborne_file_provider_uploads;
-- DROP TABLE IF EXISTS email4ai_airborne_files;
-- DROP TABLE IF EXISTS zztest_airborne_thread_vector_stores;
-- DROP TABLE IF EXISTS zztest_airborne_file_provider_uploads;
-- DROP TABLE IF EXISTS zztest_airborne_files;
