-- ============================================================================
-- AIRBORNE BASELINE (relational chat/chat_message, multi-tenant via RLS)
-- PostgreSQL 15+. Tenant selected per-tx via GUC airborne.tenant_id.
-- ============================================================================
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

CREATE TABLE airborne_tenants (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    status      TEXT NOT NULL DEFAULT 'active',
    settings    JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT valid_tenant_slug CHECK (id ~ '^[a-z][a-z0-9_-]{0,63}$'),
    CONSTRAINT valid_tenant_status CHECK (status IN ('active','suspended'))
);

-- CONVERSATION CONTAINER
CREATE TABLE airborne_chats (
    id                  UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    tenant_id           TEXT NOT NULL REFERENCES airborne_tenants(id),
    user_id             TEXT NOT NULL,
    title               TEXT,
    model_id            TEXT,                       -- last-used model
    provider            TEXT,                       -- last-used provider
    status              TEXT NOT NULL DEFAULT 'active',
    current_message_id  UUID,                       -- head of the active branch (leaf)
    pinned              BOOLEAN NOT NULL DEFAULT false,
    folder_id           UUID,
    share_id            TEXT,
    external_ref        TEXT,                       -- opaque caller correlation id (e.g. email_ai_svc conversation uuid)
    metadata            JSONB,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT valid_chat_status CHECK (status IN ('active','archived','deleted'))
);
CREATE INDEX idx_chats_tenant_user_updated ON airborne_chats(tenant_id, user_id, updated_at DESC);
CREATE INDEX idx_chats_tenant_pinned ON airborne_chats(tenant_id, user_id, pinned) WHERE pinned;
CREATE UNIQUE INDEX idx_chats_share ON airborne_chats(share_id) WHERE share_id IS NOT NULL;
CREATE UNIQUE INDEX idx_chats_external
    ON airborne_chats(tenant_id, external_ref) WHERE external_ref IS NOT NULL;

-- MESSAGES (sole source of truth; branchable tree)
CREATE TABLE airborne_chat_messages (
    id                  UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    tenant_id           TEXT NOT NULL REFERENCES airborne_tenants(id),
    chat_id             UUID NOT NULL REFERENCES airborne_chats(id) ON DELETE CASCADE,
    parent_id           UUID REFERENCES airborne_chat_messages(id) ON DELETE CASCADE,  -- tree edge; CASCADE = deleting a message deletes its subtree
    sibling_seq         INT NOT NULL DEFAULT 0,      -- sibling order for the branch switcher; reads tie-break by (sibling_seq, created_at, id), so it need not be unique
    user_id             TEXT NOT NULL,
    role                TEXT NOT NULL,
    content             JSONB NOT NULL,             -- string or list of content blocks
    model_id            TEXT,
    provider            TEXT,
    response_id         TEXT,                       -- provider continuity id
    status              TEXT NOT NULL DEFAULT 'complete',
    status_history      JSONB,                      -- ordered generation-event timeline (web-search/thinking/tool-call)
    error               JSONB,
    input_tokens        INT,
    output_tokens       INT,
    total_tokens        INT,
    cost_usd            DECIMAL(12,6),
    grounding_queries   INT,
    grounding_cost_usd  DECIMAL(12,6),
    processing_time_ms  INT,
    usage               JSONB,                      -- raw provider usage (catch-all; typed columns above are the query surface)
    sources             JSONB,                      -- citations, shape: [{source:{id,name,type}, document:[...], metadata:[...]}]
    embeds              JSONB,                      -- inline artifacts/media rendered in the message
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT valid_role CHECK (role IN ('user','assistant','system','tool')),
    CONSTRAINT valid_msg_status CHECK (status IN ('pending','streaming','complete','error'))
);
CREATE INDEX idx_msgs_tenant_chat_created ON airborne_chat_messages(tenant_id, chat_id, created_at);
CREATE INDEX idx_msgs_tenant_chat_parent ON airborne_chat_messages(tenant_id, chat_id, parent_id, sibling_seq);  -- tree walk + ordered sibling retrieval
CREATE INDEX idx_msgs_tenant_model_created ON airborne_chat_messages(tenant_id, model_id, created_at) WHERE model_id IS NOT NULL;
CREATE INDEX idx_msgs_tenant_user_created ON airborne_chat_messages(tenant_id, user_id, created_at);

-- COLD DEBUG BLOBS (1:1, retention-friendly, keeps the hot table lean)
CREATE TABLE airborne_chat_message_debug (
    message_id          UUID PRIMARY KEY REFERENCES airborne_chat_messages(id) ON DELETE CASCADE,
    tenant_id           TEXT NOT NULL REFERENCES airborne_tenants(id),
    system_prompt       TEXT,
    raw_request_json    JSONB,
    raw_response_json   JSONB,
    rendered_html       TEXT,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- FILES + ATTACHMENT JOIN + VECTOR STORES
CREATE TABLE airborne_files (
    id                UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    tenant_id         TEXT NOT NULL REFERENCES airborne_tenants(id),
    user_id           TEXT NOT NULL,
    filename          TEXT NOT NULL,
    mime_type         TEXT,
    size_bytes        BIGINT,
    provider          TEXT,
    provider_file_id  TEXT,
    store_id          TEXT,
    status            TEXT NOT NULL DEFAULT 'uploaded',
    hash              TEXT,                          -- content hash for dedup (OWUI file.hash)
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    metadata          JSONB
);
CREATE INDEX idx_files_tenant_user ON airborne_files(tenant_id, user_id);
CREATE INDEX idx_files_tenant_hash ON airborne_files(tenant_id, hash) WHERE hash IS NOT NULL;

CREATE TABLE airborne_chat_files (
    id          UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    tenant_id   TEXT NOT NULL REFERENCES airborne_tenants(id),
    chat_id     UUID NOT NULL REFERENCES airborne_chats(id) ON DELETE CASCADE,
    message_id  UUID REFERENCES airborne_chat_messages(id) ON DELETE CASCADE,
    file_id     UUID NOT NULL REFERENCES airborne_files(id) ON DELETE CASCADE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_chat_files_tenant_chat ON airborne_chat_files(tenant_id, chat_id);
CREATE INDEX idx_chat_files_tenant_message ON airborne_chat_files(tenant_id, message_id);

CREATE TABLE airborne_chat_vector_stores (
    id          UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    tenant_id   TEXT NOT NULL REFERENCES airborne_tenants(id),
    chat_id     UUID NOT NULL REFERENCES airborne_chats(id) ON DELETE CASCADE,
    store_id    TEXT NOT NULL,
    provider    TEXT NOT NULL,
    enabled     BOOLEAN NOT NULL DEFAULT true,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_chat_stores_tenant_chat ON airborne_chat_vector_stores(tenant_id, chat_id);

-- PER-TENANT MODEL REGISTRY (Open WebUI's base_model_id + params + is_active pattern).
-- Named presets/aliases with default params and soft-disable. chat_messages.model_id
-- records whatever model actually ran and is NOT FK'd here (global base models need no
-- row); this table is for tenant-defined named configs the request path can resolve.
CREATE TABLE airborne_models (
    id            TEXT NOT NULL,                      -- model id or tenant alias, e.g. 'gpt-4o' or 'fast'
    tenant_id     TEXT NOT NULL REFERENCES airborne_tenants(id),
    base_model_id TEXT,                               -- upstream real model for an alias; NULL = itself
    name          TEXT,                               -- display name
    provider      TEXT,
    params        JSONB NOT NULL DEFAULT '{}'::jsonb,  -- default inference params (temperature, etc.)
    meta          JSONB NOT NULL DEFAULT '{}'::jsonb,  -- capabilities, description, tags
    is_active     BOOLEAN NOT NULL DEFAULT true,       -- soft-disable without deleting
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (tenant_id, id)
);
CREATE INDEX idx_models_tenant_active ON airborne_models(tenant_id, is_active);

-- updated_at maintenance
CREATE OR REPLACE FUNCTION airborne_touch_updated_at()
RETURNS TRIGGER AS $$ BEGIN NEW.updated_at = NOW(); RETURN NEW; END; $$ LANGUAGE plpgsql;
CREATE TRIGGER trg_chats_updated BEFORE UPDATE ON airborne_chats
    FOR EACH ROW EXECUTE FUNCTION airborne_touch_updated_at();
CREATE TRIGGER trg_msgs_updated BEFORE UPDATE ON airborne_chat_messages
    FOR EACH ROW EXECUTE FUNCTION airborne_touch_updated_at();
CREATE TRIGGER trg_tenants_updated BEFORE UPDATE ON airborne_tenants
    FOR EACH ROW EXECUTE FUNCTION airborne_touch_updated_at();
CREATE TRIGGER trg_models_updated BEFORE UPDATE ON airborne_models
    FOR EACH ROW EXECUTE FUNCTION airborne_touch_updated_at();

-- tenant_id immutability
CREATE OR REPLACE FUNCTION airborne_forbid_tenant_move()
RETURNS TRIGGER AS $$
BEGIN
    IF NEW.tenant_id IS DISTINCT FROM OLD.tenant_id THEN
        RAISE EXCEPTION 'tenant_id is immutable (% -> %)', OLD.tenant_id, NEW.tenant_id;
    END IF;
    RETURN NEW;
END; $$ LANGUAGE plpgsql;

-- Registry RLS: open read (needed before any tenant GUC is set), admin-mode write.
ALTER TABLE airborne_tenants ENABLE ROW LEVEL SECURITY;
ALTER TABLE airborne_tenants FORCE ROW LEVEL SECURITY;
CREATE POLICY airborne_tenants_read ON airborne_tenants FOR SELECT USING (true);
CREATE POLICY airborne_tenants_write ON airborne_tenants FOR ALL
    USING (current_setting('airborne.admin_mode', true) = 'true')
    WITH CHECK (current_setting('airborne.admin_mode', true) = 'true');

-- Per-table RLS (read: own tenant OR cross-tenant admin; write: own tenant) + immutability.
DO $$
DECLARE t TEXT;
BEGIN
    FOREACH t IN ARRAY ARRAY[
        'airborne_chats','airborne_chat_messages','airborne_chat_message_debug',
        'airborne_files','airborne_chat_files','airborne_chat_vector_stores','airborne_models'
    ] LOOP
        EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', t);
        EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', t);
        EXECUTE format($f$CREATE POLICY %1$s_read ON %1$I FOR SELECT USING (
            current_setting('airborne.cross_tenant_mode', true) = 'true'
            OR tenant_id = current_setting('airborne.tenant_id', true))$f$, t);
        EXECUTE format($f$CREATE POLICY %1$s_insert ON %1$I FOR INSERT
            WITH CHECK (tenant_id = current_setting('airborne.tenant_id', true))$f$, t);
        EXECUTE format($f$CREATE POLICY %1$s_update ON %1$I FOR UPDATE
            USING (tenant_id = current_setting('airborne.tenant_id', true))
            WITH CHECK (tenant_id = current_setting('airborne.tenant_id', true))$f$, t);
        EXECUTE format($f$CREATE POLICY %1$s_delete ON %1$I FOR DELETE
            USING (tenant_id = current_setting('airborne.tenant_id', true))$f$, t);
        EXECUTE format($f$CREATE TRIGGER %1$s_immutable_tenant BEFORE UPDATE ON %1$I
            FOR EACH ROW EXECUTE FUNCTION airborne_forbid_tenant_move()$f$, t);
    END LOOP;
END $$;

-- (No admin activity view: the feed is read directly from airborne_chat_messages
-- under WithCrossTenant in the repository, so a separate view would be dead code.)

-- Grant DML to the application role if it exists. Role creation is a deployment
-- prerequisite (managed outside this migration; the container test harness
-- creates it). The app MUST connect as this NOSUPERUSER NOBYPASSRLS role, not
-- the owner, or RLS is bypassed entirely.
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'airborne_app') THEN
        GRANT USAGE ON SCHEMA public TO airborne_app;
        GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO airborne_app;
        GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO airborne_app;
    END IF;
END $$;

-- Seed tenants (registry write requires admin mode).
SELECT set_config('airborne.admin_mode', 'true', false);
INSERT INTO airborne_tenants (id, name) VALUES
    ('ai8','AI8'), ('email4ai','Email4AI'), ('zztest','ZZ Test')
ON CONFLICT (id) DO NOTHING;
SELECT set_config('airborne.admin_mode', 'false', false);
