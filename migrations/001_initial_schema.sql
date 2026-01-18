-- ============================================================================
-- AIRBORNE MESSAGE STORAGE SCHEMA
-- ============================================================================
-- Database: PostgreSQL 15+ (Supabase compatible)
-- Purpose: Multi-tenant LLM conversation persistence with cost tracking
-- Run: psql -d airborne -f migrations/001_initial_schema.sql
-- ============================================================================

-- Enable UUID extension if not already enabled
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

-- ============================================================================
-- CORE TABLES
-- ============================================================================

-- ----------------------------------------------------------------------------
-- THREADS: Conversation containers with tenant isolation
-- ----------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS airborne_threads (
    id              UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    tenant_id       TEXT NOT NULL,
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
CREATE INDEX IF NOT EXISTS idx_threads_tenant ON airborne_threads(tenant_id);
CREATE INDEX IF NOT EXISTS idx_threads_tenant_user ON airborne_threads(tenant_id, user_id);
CREATE INDEX IF NOT EXISTS idx_threads_updated ON airborne_threads(tenant_id, updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_threads_status ON airborne_threads(tenant_id, status) WHERE status = 'active';

COMMENT ON TABLE airborne_threads IS 'Conversation containers with multi-tenant isolation';
COMMENT ON COLUMN airborne_threads.provider IS 'Last-used provider for thread continuity';
COMMENT ON COLUMN airborne_threads.message_count IS 'Cached count, maintained by trigger';

-- ----------------------------------------------------------------------------
-- MESSAGES: All conversation messages (user, assistant, system)
-- ----------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS airborne_messages (
    id                  UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    thread_id           UUID NOT NULL REFERENCES airborne_threads(id) ON DELETE CASCADE,

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

    -- Constraints
    CONSTRAINT valid_role CHECK (role IN ('user', 'assistant', 'system'))
);

-- Indexes for message retrieval
CREATE INDEX IF NOT EXISTS idx_messages_thread ON airborne_messages(thread_id, created_at);
CREATE INDEX IF NOT EXISTS idx_messages_role ON airborne_messages(thread_id, role);
CREATE INDEX IF NOT EXISTS idx_messages_created ON airborne_messages(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_messages_assistant_recent ON airborne_messages(created_at DESC) WHERE role = 'assistant';

COMMENT ON TABLE airborne_messages IS 'All messages in conversations with full metrics';
COMMENT ON COLUMN airborne_messages.cost_usd IS 'Calculated from pricing table at persistence time';
COMMENT ON COLUMN airborne_messages.citations IS 'Array of {type, url, title, file_id, snippet}';

-- ============================================================================
-- FUNCTIONS AND TRIGGERS
-- ============================================================================

-- ----------------------------------------------------------------------------
-- Auto-update updated_at timestamp
-- ----------------------------------------------------------------------------
CREATE OR REPLACE FUNCTION update_thread_timestamp()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trigger_thread_updated ON airborne_threads;
CREATE TRIGGER trigger_thread_updated
    BEFORE UPDATE ON airborne_threads
    FOR EACH ROW
    EXECUTE FUNCTION update_thread_timestamp();

-- ----------------------------------------------------------------------------
-- Auto-increment message count and update thread timestamp
-- ----------------------------------------------------------------------------
CREATE OR REPLACE FUNCTION increment_message_count()
RETURNS TRIGGER AS $$
BEGIN
    UPDATE airborne_threads
    SET message_count = message_count + 1,
        updated_at = NOW()
    WHERE id = NEW.thread_id;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trigger_message_inserted ON airborne_messages;
CREATE TRIGGER trigger_message_inserted
    AFTER INSERT ON airborne_messages
    FOR EACH ROW
    EXECUTE FUNCTION increment_message_count();

-- ============================================================================
-- USEFUL VIEWS
-- ============================================================================

-- ----------------------------------------------------------------------------
-- Activity feed view (last 50 assistant messages with thread costs)
-- ----------------------------------------------------------------------------
CREATE OR REPLACE VIEW airborne_activity_feed AS
SELECT
    m.id,
    m.thread_id,
    t.tenant_id,
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
        FROM airborne_messages
        WHERE thread_id = m.thread_id
    ) AS thread_cost_usd
FROM airborne_messages m
JOIN airborne_threads t ON m.thread_id = t.id
WHERE m.role = 'assistant'
ORDER BY m.created_at DESC
LIMIT 50;

COMMENT ON VIEW airborne_activity_feed IS 'Pre-built view for admin activity feed';

-- ----------------------------------------------------------------------------
-- Thread summary view (for listing threads with stats)
-- ----------------------------------------------------------------------------
CREATE OR REPLACE VIEW airborne_thread_summary AS
SELECT
    t.id,
    t.tenant_id,
    t.user_id,
    t.provider,
    t.model,
    t.status,
    t.message_count,
    t.created_at,
    t.updated_at,
    COALESCE(SUM(m.cost_usd), 0) AS total_cost_usd,
    COALESCE(SUM(m.input_tokens), 0) AS total_input_tokens,
    COALESCE(SUM(m.output_tokens), 0) AS total_output_tokens
FROM airborne_threads t
LEFT JOIN airborne_messages m ON t.id = m.thread_id
GROUP BY t.id;

COMMENT ON VIEW airborne_thread_summary IS 'Thread listing with aggregated cost and token stats';

-- ============================================================================
-- SAMPLE QUERIES (for reference)
-- ============================================================================

-- Get thread history (last 20 messages, chronological)
-- SELECT * FROM airborne_messages
-- WHERE thread_id = $1
-- ORDER BY created_at ASC
-- LIMIT 20;

-- Get user's recent threads
-- SELECT * FROM airborne_threads
-- WHERE tenant_id = $1 AND user_id = $2 AND status = 'active'
-- ORDER BY updated_at DESC
-- LIMIT 10;

-- Get total cost for a tenant
-- SELECT SUM(m.cost_usd) as total_cost
-- FROM airborne_messages m
-- JOIN airborne_threads t ON m.thread_id = t.id
-- WHERE t.tenant_id = $1
-- AND m.created_at >= NOW() - INTERVAL '30 days';

-- Get provider usage distribution
-- SELECT provider, COUNT(*) as message_count, SUM(cost_usd) as total_cost
-- FROM airborne_messages
-- WHERE provider IS NOT NULL
-- AND created_at >= NOW() - INTERVAL '7 days'
-- GROUP BY provider;
