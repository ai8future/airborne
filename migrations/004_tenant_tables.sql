-- ============================================================================
-- AIRBORNE TENANT-PREFIXED TABLES MIGRATION
-- ============================================================================
-- Purpose: Replace single multi-tenant tables with per-tenant table sets
-- This provides table-level isolation instead of row-level tenant_id filtering
-- Run: psql -d airborne -f migrations/003_tenant_tables.sql
-- ============================================================================

-- ============================================================================
-- AI8 TENANT TABLES
-- ============================================================================

-- ----------------------------------------------------------------------------
-- AI8 THREADS: Conversation containers (no tenant_id needed - isolation at table level)
-- ----------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS ai8_airborne_threads (
    id              UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id         TEXT NOT NULL,

    -- Last-used provider info (for continuity)
    provider        TEXT,                           -- openai, anthropic, gemini
    model           TEXT,                           -- gpt-4o, claude-sonnet-4, etc.

    -- Thread lifecycle
    status          TEXT NOT NULL DEFAULT 'active', -- active, archived, deleted
    message_count   INT NOT NULL DEFAULT 0,         -- Auto-incremented by trigger

    -- Timestamps
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    -- Extensible metadata (feature flags, custom fields)
    metadata        JSONB
);

-- Indexes for common query patterns
CREATE INDEX IF NOT EXISTS idx_ai8_threads_user ON ai8_airborne_threads(user_id);
CREATE INDEX IF NOT EXISTS idx_ai8_threads_updated ON ai8_airborne_threads(updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_ai8_threads_status ON ai8_airborne_threads(status) WHERE status = 'active';

COMMENT ON TABLE ai8_airborne_threads IS 'AI8 tenant conversation containers';
COMMENT ON COLUMN ai8_airborne_threads.provider IS 'Last-used provider for thread continuity';
COMMENT ON COLUMN ai8_airborne_threads.message_count IS 'Cached count, maintained by trigger';

-- ----------------------------------------------------------------------------
-- AI8 MESSAGES: All conversation messages (user, assistant, system)
-- ----------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS ai8_airborne_messages (
    id                  UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    thread_id           UUID NOT NULL REFERENCES ai8_airborne_threads(id) ON DELETE CASCADE,

    -- Message content
    role                TEXT NOT NULL,              -- user, assistant, system
    content             TEXT NOT NULL,

    -- Provider metadata (NULL for user/system messages)
    provider            TEXT,                       -- openai, anthropic, gemini
    model               TEXT,                       -- specific model used
    response_id         TEXT,                       -- OpenAI previousResponseID

    -- Usage metrics (NULL for user/system messages)
    input_tokens        INT,
    output_tokens       INT,
    total_tokens        INT,
    cost_usd            DECIMAL(10, 6),             -- Microsecond precision for costs
    processing_time_ms  INT,                        -- End-to-end latency

    -- Rich context
    citations           JSONB,                      -- Web/file search citations

    -- Timestamps
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    -- Extensible metadata
    metadata            JSONB,

    -- Debug fields (from migration 002)
    system_prompt       TEXT,
    raw_request_json    JSONB,
    raw_response_json   JSONB,
    rendered_html       TEXT,

    -- Constraints
    CONSTRAINT ai8_valid_role CHECK (role IN ('user', 'assistant', 'system'))
);

-- Indexes for message retrieval
CREATE INDEX IF NOT EXISTS idx_ai8_messages_thread ON ai8_airborne_messages(thread_id, created_at);
CREATE INDEX IF NOT EXISTS idx_ai8_messages_role ON ai8_airborne_messages(thread_id, role);
CREATE INDEX IF NOT EXISTS idx_ai8_messages_created ON ai8_airborne_messages(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_ai8_messages_assistant_recent ON ai8_airborne_messages(created_at DESC) WHERE role = 'assistant';
CREATE INDEX IF NOT EXISTS idx_ai8_messages_has_debug ON ai8_airborne_messages(created_at DESC) WHERE raw_request_json IS NOT NULL;

COMMENT ON TABLE ai8_airborne_messages IS 'AI8 tenant messages with full metrics';
COMMENT ON COLUMN ai8_airborne_messages.cost_usd IS 'Calculated from pricing table at persistence time';
COMMENT ON COLUMN ai8_airborne_messages.citations IS 'Array of {type, url, title, file_id, snippet}';

-- ============================================================================
-- EMAIL4AI TENANT TABLES
-- ============================================================================

-- ----------------------------------------------------------------------------
-- EMAIL4AI THREADS: Conversation containers
-- ----------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS email4ai_airborne_threads (
    id              UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id         TEXT NOT NULL,

    -- Last-used provider info (for continuity)
    provider        TEXT,                           -- openai, anthropic, gemini
    model           TEXT,                           -- gpt-4o, claude-sonnet-4, etc.

    -- Thread lifecycle
    status          TEXT NOT NULL DEFAULT 'active', -- active, archived, deleted
    message_count   INT NOT NULL DEFAULT 0,         -- Auto-incremented by trigger

    -- Timestamps
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    -- Extensible metadata (feature flags, custom fields)
    metadata        JSONB
);

-- Indexes for common query patterns
CREATE INDEX IF NOT EXISTS idx_email4ai_threads_user ON email4ai_airborne_threads(user_id);
CREATE INDEX IF NOT EXISTS idx_email4ai_threads_updated ON email4ai_airborne_threads(updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_email4ai_threads_status ON email4ai_airborne_threads(status) WHERE status = 'active';

COMMENT ON TABLE email4ai_airborne_threads IS 'Email4AI tenant conversation containers';
COMMENT ON COLUMN email4ai_airborne_threads.provider IS 'Last-used provider for thread continuity';
COMMENT ON COLUMN email4ai_airborne_threads.message_count IS 'Cached count, maintained by trigger';

-- ----------------------------------------------------------------------------
-- EMAIL4AI MESSAGES: All conversation messages (user, assistant, system)
-- ----------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS email4ai_airborne_messages (
    id                  UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    thread_id           UUID NOT NULL REFERENCES email4ai_airborne_threads(id) ON DELETE CASCADE,

    -- Message content
    role                TEXT NOT NULL,              -- user, assistant, system
    content             TEXT NOT NULL,

    -- Provider metadata (NULL for user/system messages)
    provider            TEXT,                       -- openai, anthropic, gemini
    model               TEXT,                       -- specific model used
    response_id         TEXT,                       -- OpenAI previousResponseID

    -- Usage metrics (NULL for user/system messages)
    input_tokens        INT,
    output_tokens       INT,
    total_tokens        INT,
    cost_usd            DECIMAL(10, 6),             -- Microsecond precision for costs
    processing_time_ms  INT,                        -- End-to-end latency

    -- Rich context
    citations           JSONB,                      -- Web/file search citations

    -- Timestamps
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    -- Extensible metadata
    metadata            JSONB,

    -- Debug fields (from migration 002)
    system_prompt       TEXT,
    raw_request_json    JSONB,
    raw_response_json   JSONB,
    rendered_html       TEXT,

    -- Constraints
    CONSTRAINT email4ai_valid_role CHECK (role IN ('user', 'assistant', 'system'))
);

-- Indexes for message retrieval
CREATE INDEX IF NOT EXISTS idx_email4ai_messages_thread ON email4ai_airborne_messages(thread_id, created_at);
CREATE INDEX IF NOT EXISTS idx_email4ai_messages_role ON email4ai_airborne_messages(thread_id, role);
CREATE INDEX IF NOT EXISTS idx_email4ai_messages_created ON email4ai_airborne_messages(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_email4ai_messages_assistant_recent ON email4ai_airborne_messages(created_at DESC) WHERE role = 'assistant';
CREATE INDEX IF NOT EXISTS idx_email4ai_messages_has_debug ON email4ai_airborne_messages(created_at DESC) WHERE raw_request_json IS NOT NULL;

COMMENT ON TABLE email4ai_airborne_messages IS 'Email4AI tenant messages with full metrics';
COMMENT ON COLUMN email4ai_airborne_messages.cost_usd IS 'Calculated from pricing table at persistence time';
COMMENT ON COLUMN email4ai_airborne_messages.citations IS 'Array of {type, url, title, file_id, snippet}';

-- ============================================================================
-- ZZTEST TENANT TABLES (for testing)
-- ============================================================================

-- ----------------------------------------------------------------------------
-- ZZTEST THREADS: Conversation containers
-- ----------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS zztest_airborne_threads (
    id              UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id         TEXT NOT NULL,

    -- Last-used provider info (for continuity)
    provider        TEXT,                           -- openai, anthropic, gemini
    model           TEXT,                           -- gpt-4o, claude-sonnet-4, etc.

    -- Thread lifecycle
    status          TEXT NOT NULL DEFAULT 'active', -- active, archived, deleted
    message_count   INT NOT NULL DEFAULT 0,         -- Auto-incremented by trigger

    -- Timestamps
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    -- Extensible metadata (feature flags, custom fields)
    metadata        JSONB
);

-- Indexes for common query patterns
CREATE INDEX IF NOT EXISTS idx_zztest_threads_user ON zztest_airborne_threads(user_id);
CREATE INDEX IF NOT EXISTS idx_zztest_threads_updated ON zztest_airborne_threads(updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_zztest_threads_status ON zztest_airborne_threads(status) WHERE status = 'active';

COMMENT ON TABLE zztest_airborne_threads IS 'Test tenant conversation containers';
COMMENT ON COLUMN zztest_airborne_threads.provider IS 'Last-used provider for thread continuity';
COMMENT ON COLUMN zztest_airborne_threads.message_count IS 'Cached count, maintained by trigger';

-- ----------------------------------------------------------------------------
-- ZZTEST MESSAGES: All conversation messages (user, assistant, system)
-- ----------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS zztest_airborne_messages (
    id                  UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    thread_id           UUID NOT NULL REFERENCES zztest_airborne_threads(id) ON DELETE CASCADE,

    -- Message content
    role                TEXT NOT NULL,              -- user, assistant, system
    content             TEXT NOT NULL,

    -- Provider metadata (NULL for user/system messages)
    provider            TEXT,                       -- openai, anthropic, gemini
    model               TEXT,                       -- specific model used
    response_id         TEXT,                       -- OpenAI previousResponseID

    -- Usage metrics (NULL for user/system messages)
    input_tokens        INT,
    output_tokens       INT,
    total_tokens        INT,
    cost_usd            DECIMAL(10, 6),             -- Microsecond precision for costs
    processing_time_ms  INT,                        -- End-to-end latency

    -- Rich context
    citations           JSONB,                      -- Web/file search citations

    -- Timestamps
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    -- Extensible metadata
    metadata            JSONB,

    -- Debug fields (from migration 002)
    system_prompt       TEXT,
    raw_request_json    JSONB,
    raw_response_json   JSONB,
    rendered_html       TEXT,

    -- Constraints
    CONSTRAINT zztest_valid_role CHECK (role IN ('user', 'assistant', 'system'))
);

-- Indexes for message retrieval
CREATE INDEX IF NOT EXISTS idx_zztest_messages_thread ON zztest_airborne_messages(thread_id, created_at);
CREATE INDEX IF NOT EXISTS idx_zztest_messages_role ON zztest_airborne_messages(thread_id, role);
CREATE INDEX IF NOT EXISTS idx_zztest_messages_created ON zztest_airborne_messages(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_zztest_messages_assistant_recent ON zztest_airborne_messages(created_at DESC) WHERE role = 'assistant';
CREATE INDEX IF NOT EXISTS idx_zztest_messages_has_debug ON zztest_airborne_messages(created_at DESC) WHERE raw_request_json IS NOT NULL;

COMMENT ON TABLE zztest_airborne_messages IS 'Test tenant messages with full metrics';
COMMENT ON COLUMN zztest_airborne_messages.cost_usd IS 'Calculated from pricing table at persistence time';
COMMENT ON COLUMN zztest_airborne_messages.citations IS 'Array of {type, url, title, file_id, snippet}';

-- ============================================================================
-- TRIGGERS FOR AI8 TENANT
-- ============================================================================

-- Auto-update updated_at timestamp for AI8 threads
CREATE OR REPLACE FUNCTION ai8_update_thread_timestamp()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trigger_ai8_thread_updated ON ai8_airborne_threads;
CREATE TRIGGER trigger_ai8_thread_updated
    BEFORE UPDATE ON ai8_airborne_threads
    FOR EACH ROW
    EXECUTE FUNCTION ai8_update_thread_timestamp();

-- Auto-increment message count for AI8
CREATE OR REPLACE FUNCTION ai8_increment_message_count()
RETURNS TRIGGER AS $$
BEGIN
    UPDATE ai8_airborne_threads
    SET message_count = message_count + 1,
        updated_at = NOW()
    WHERE id = NEW.thread_id;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trigger_ai8_message_inserted ON ai8_airborne_messages;
CREATE TRIGGER trigger_ai8_message_inserted
    AFTER INSERT ON ai8_airborne_messages
    FOR EACH ROW
    EXECUTE FUNCTION ai8_increment_message_count();

-- ============================================================================
-- TRIGGERS FOR EMAIL4AI TENANT
-- ============================================================================

-- Auto-update updated_at timestamp for Email4AI threads
CREATE OR REPLACE FUNCTION email4ai_update_thread_timestamp()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trigger_email4ai_thread_updated ON email4ai_airborne_threads;
CREATE TRIGGER trigger_email4ai_thread_updated
    BEFORE UPDATE ON email4ai_airborne_threads
    FOR EACH ROW
    EXECUTE FUNCTION email4ai_update_thread_timestamp();

-- Auto-increment message count for Email4AI
CREATE OR REPLACE FUNCTION email4ai_increment_message_count()
RETURNS TRIGGER AS $$
BEGIN
    UPDATE email4ai_airborne_threads
    SET message_count = message_count + 1,
        updated_at = NOW()
    WHERE id = NEW.thread_id;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trigger_email4ai_message_inserted ON email4ai_airborne_messages;
CREATE TRIGGER trigger_email4ai_message_inserted
    AFTER INSERT ON email4ai_airborne_messages
    FOR EACH ROW
    EXECUTE FUNCTION email4ai_increment_message_count();

-- ============================================================================
-- TRIGGERS FOR ZZTEST TENANT
-- ============================================================================

-- Auto-update updated_at timestamp for zztest threads
CREATE OR REPLACE FUNCTION zztest_update_thread_timestamp()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trigger_zztest_thread_updated ON zztest_airborne_threads;
CREATE TRIGGER trigger_zztest_thread_updated
    BEFORE UPDATE ON zztest_airborne_threads
    FOR EACH ROW
    EXECUTE FUNCTION zztest_update_thread_timestamp();

-- Auto-increment message count for zztest
CREATE OR REPLACE FUNCTION zztest_increment_message_count()
RETURNS TRIGGER AS $$
BEGIN
    UPDATE zztest_airborne_threads
    SET message_count = message_count + 1,
        updated_at = NOW()
    WHERE id = NEW.thread_id;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trigger_zztest_message_inserted ON zztest_airborne_messages;
CREATE TRIGGER trigger_zztest_message_inserted
    AFTER INSERT ON zztest_airborne_messages
    FOR EACH ROW
    EXECUTE FUNCTION zztest_increment_message_count();

-- ============================================================================
-- VIEWS FOR CROSS-TENANT QUERIES (Admin Dashboard)
-- ============================================================================

-- Activity feed view combining all tenants
CREATE OR REPLACE VIEW airborne_tenant_activity_feed AS
SELECT
    m.id,
    m.thread_id,
    'ai8' AS tenant_id,
    t.user_id,
    m.content,
    m.provider,
    m.model,
    m.input_tokens,
    m.output_tokens,
    m.total_tokens,
    m.cost_usd,
    m.processing_time_ms,
    m.citations,
    m.created_at,
    (
        SELECT COALESCE(SUM(cost_usd), 0)
        FROM ai8_airborne_messages
        WHERE thread_id = m.thread_id
    ) AS thread_cost_usd
FROM ai8_airborne_messages m
JOIN ai8_airborne_threads t ON m.thread_id = t.id
WHERE m.role = 'assistant'

UNION ALL

SELECT
    m.id,
    m.thread_id,
    'email4ai' AS tenant_id,
    t.user_id,
    m.content,
    m.provider,
    m.model,
    m.input_tokens,
    m.output_tokens,
    m.total_tokens,
    m.cost_usd,
    m.processing_time_ms,
    m.citations,
    m.created_at,
    (
        SELECT COALESCE(SUM(cost_usd), 0)
        FROM email4ai_airborne_messages
        WHERE thread_id = m.thread_id
    ) AS thread_cost_usd
FROM email4ai_airborne_messages m
JOIN email4ai_airborne_threads t ON m.thread_id = t.id
WHERE m.role = 'assistant'

UNION ALL

SELECT
    m.id,
    m.thread_id,
    'zztest' AS tenant_id,
    t.user_id,
    m.content,
    m.provider,
    m.model,
    m.input_tokens,
    m.output_tokens,
    m.total_tokens,
    m.cost_usd,
    m.processing_time_ms,
    m.citations,
    m.created_at,
    (
        SELECT COALESCE(SUM(cost_usd), 0)
        FROM zztest_airborne_messages
        WHERE thread_id = m.thread_id
    ) AS thread_cost_usd
FROM zztest_airborne_messages m
JOIN zztest_airborne_threads t ON m.thread_id = t.id
WHERE m.role = 'assistant';

COMMENT ON VIEW airborne_tenant_activity_feed IS 'Combined activity feed for admin dashboard across all tenants';

-- ============================================================================
-- MIGRATE EXISTING DATA (if any exists)
-- ============================================================================

-- Migrate ai8 data from old tables (if they exist and have data)
INSERT INTO ai8_airborne_threads (id, user_id, provider, model, status, message_count, created_at, updated_at, metadata)
SELECT id, user_id, provider, model, status, message_count, created_at, updated_at, metadata
FROM airborne_threads
WHERE tenant_id = 'ai8'
ON CONFLICT (id) DO NOTHING;

INSERT INTO ai8_airborne_messages (id, thread_id, role, content, provider, model, response_id, input_tokens, output_tokens, total_tokens, cost_usd, processing_time_ms, citations, created_at, metadata, system_prompt, raw_request_json, raw_response_json)
SELECT m.id, m.thread_id, m.role, m.content, m.provider, m.model, m.response_id, m.input_tokens, m.output_tokens, m.total_tokens, m.cost_usd, m.processing_time_ms, m.citations, m.created_at, m.metadata, m.system_prompt, m.raw_request_json, m.raw_response_json
FROM airborne_messages m
JOIN airborne_threads t ON m.thread_id = t.id
WHERE t.tenant_id = 'ai8'
ON CONFLICT (id) DO NOTHING;

-- Migrate email4ai data from old tables (if they exist and have data)
INSERT INTO email4ai_airborne_threads (id, user_id, provider, model, status, message_count, created_at, updated_at, metadata)
SELECT id, user_id, provider, model, status, message_count, created_at, updated_at, metadata
FROM airborne_threads
WHERE tenant_id = 'email4ai'
ON CONFLICT (id) DO NOTHING;

INSERT INTO email4ai_airborne_messages (id, thread_id, role, content, provider, model, response_id, input_tokens, output_tokens, total_tokens, cost_usd, processing_time_ms, citations, created_at, metadata, system_prompt, raw_request_json, raw_response_json)
SELECT m.id, m.thread_id, m.role, m.content, m.provider, m.model, m.response_id, m.input_tokens, m.output_tokens, m.total_tokens, m.cost_usd, m.processing_time_ms, m.citations, m.created_at, m.metadata, m.system_prompt, m.raw_request_json, m.raw_response_json
FROM airborne_messages m
JOIN airborne_threads t ON m.thread_id = t.id
WHERE t.tenant_id = 'email4ai'
ON CONFLICT (id) DO NOTHING;

-- ============================================================================
-- DROP OLD TABLES AND VIEWS (after migration is verified)
-- ============================================================================
-- NOTE: Uncomment these lines only after verifying the migration was successful
--
-- DROP VIEW IF EXISTS airborne_activity_feed;
-- DROP VIEW IF EXISTS airborne_thread_summary;
-- DROP TRIGGER IF EXISTS trigger_message_inserted ON airborne_messages;
-- DROP TRIGGER IF EXISTS trigger_thread_updated ON airborne_threads;
-- DROP TABLE IF EXISTS airborne_messages;
-- DROP TABLE IF EXISTS airborne_threads;
-- DROP FUNCTION IF EXISTS update_thread_timestamp();
-- DROP FUNCTION IF EXISTS increment_message_count();

-- ============================================================================
-- ROLLBACK INSTRUCTIONS
-- ============================================================================
-- To rollback this migration:
-- 1. DROP VIEW IF EXISTS airborne_tenant_activity_feed;
-- 2. DROP TRIGGER IF EXISTS trigger_ai8_message_inserted ON ai8_airborne_messages;
-- 3. DROP TRIGGER IF EXISTS trigger_ai8_thread_updated ON ai8_airborne_threads;
-- 4. DROP TRIGGER IF EXISTS trigger_email4ai_message_inserted ON email4ai_airborne_messages;
-- 5. DROP TRIGGER IF EXISTS trigger_email4ai_thread_updated ON email4ai_airborne_threads;
-- 6. DROP TRIGGER IF EXISTS trigger_zztest_message_inserted ON zztest_airborne_messages;
-- 7. DROP TRIGGER IF EXISTS trigger_zztest_thread_updated ON zztest_airborne_threads;
-- 8. DROP FUNCTION IF EXISTS ai8_update_thread_timestamp();
-- 9. DROP FUNCTION IF EXISTS ai8_increment_message_count();
-- 10. DROP FUNCTION IF EXISTS email4ai_update_thread_timestamp();
-- 11. DROP FUNCTION IF EXISTS email4ai_increment_message_count();
-- 12. DROP FUNCTION IF EXISTS zztest_update_thread_timestamp();
-- 13. DROP FUNCTION IF EXISTS zztest_increment_message_count();
-- 14. DROP TABLE IF EXISTS ai8_airborne_messages;
-- 15. DROP TABLE IF EXISTS ai8_airborne_threads;
-- 16. DROP TABLE IF EXISTS email4ai_airborne_messages;
-- 17. DROP TABLE IF EXISTS email4ai_airborne_threads;
-- 18. DROP TABLE IF EXISTS zztest_airborne_messages;
-- 19. DROP TABLE IF EXISTS zztest_airborne_threads;
