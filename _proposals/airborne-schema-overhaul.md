# Airborne Schema Overhaul: Relational chat/chat_message

**Date:** July 4, 2026

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

## Summary

Airborne's schema has three compounding problems: (1) physical-table-per-tenant sprawl (`ai8_/email4ai_/zztest_` × every entity) with a hardcoded tenant list; (2) the conversation stored **twice** — normalized `airborne_messages` rows *and* a denormalized `threads.conversation_history` JSON blob added by migration `007`; and (3) a pile of dead scaffolding (13 of `007`'s 14 columns, plus the `jobs`/`archives` tables, have zero Go references). Because nothing is deployed (greenfield), this proposal replaces the whole schema with a single relational baseline modeled on Open WebUI's `chat`/`chat_message` — but taken to its **end-state**: `chat_message` is the sole source of truth (no blob, no dual-write), messages form a branchable tree via `parent_id`, analytics fields are promoted to typed indexed columns, and every table is tenant-isolated with `FORCE ROW LEVEL SECURITY`.

**Goal:** Replace the multi-table-per-tenant + dual-representation schema with one relational, RLS-isolated, branch-ready `chat`/`chat_message` model.

**Architecture:** One shared table per entity keyed by `tenant_id TEXT`, isolated by `FORCE ROW LEVEL SECURITY` on a transaction-local GUC (`airborne.tenant_id`), mirroring `delphi_api`. A conversation is one `airborne_chats` row; each message is one `airborne_chat_messages` row linked by `chat_id` (hard FK) and `parent_id` (self-FK tree). Cold debug blobs live in a 1:1 side table. Tenant identity lives in an `airborne_tenants` registry; provider secrets stay in env/secrets.

**Tech Stack:** Go 1.26, `github.com/jackc/pgx/v5` (+ `pgxpool`), chassis-go-addons `pgkit.Pool`, PostgreSQL 15+, `testcontainers-go` (new test dependency), gRPC.

## Design Rationale & Alternatives Considered

*(The "why" behind the choices below — the reasoning is otherwise only in the design conversation that produced this plan.)*

**Why relational-only, and why it looks like Open WebUI but drops the blob.** The `chat`/`chat_message` shape is borrowed from Open WebUI, but Open WebUI keeps the conversation's JSON blob (`chat.history.messages`) as the source of truth and *dual-writes* `chat_message` rows as a queryable mirror — a system caught **mid-migration** from blob to relational. That dual representation is exactly Airborne's worst existing problem (the `threads.conversation_history` blob duplicating `airborne_messages`, added by migration `007`). Open WebUI keeps the blob only because it has live data it can't rewrite atomically. **Airborne is greenfield, so we skip that debt entirely:** `chat_message` is the sole source of truth and history is reconstructed by walking `parent_id`. We adopt Open WebUI's *target*, not its transitional state — and drop its SQLite-portability choices (epoch `BigInteger` → `TIMESTAMPTZ`; composite `{chat}-{msg}` ids → plain UUIDs; all-JSON `usage` → typed, indexed columns).

**Why DB-enforced RLS instead of app-level `WHERE tenant_id`.** Three isolation strategies were weighed: (A) shared tables + `FORCE ROW LEVEL SECURITY` keyed on a tenant GUC (delphi's pattern); (B) shared tables with an app-level `WHERE tenant_id = $1` on every query; (C) do B first, add A later. We chose **A**: under B, a single forgotten `WHERE` silently leaks one tenant's rows into another's — an invisible failure until it's a breach. RLS makes the database itself refuse cross-tenant rows, so isolation doesn't depend on every query author being perfect. The critical corollary — the app must connect as a non-superuser role or RLS is bypassed — is captured in Global Constraints and Task 0. Phased option C lost its rationale once greenfield was confirmed: its whole point was de-risking a data migration that doesn't exist.

**Why a DB tenant registry instead of the hardcoded list.** Today tenants are gated by a hardcoded `ValidTenantIDs{ai8,email4ai,zztest}` map plus a config/Doppler-loaded manager — so onboarding a tenant means editing Go *and* adding tables, and Doppler is no longer used. The `airborne_tenants` table makes onboarding a single `INSERT` (no DDL, no code change), mirroring delphi's registry. Identity and secrets are deliberately kept separate: the registry is the source of truth for *which tenants exist*, while per-tenant provider API keys stay in env/secrets keyed by slug — secrets don't belong in Postgres.

**Why caller-declared tenancy is accepted as an interim state.** The gRPC caller still declares its own tenant (request field / `x-tenant-id` header), validated only for *existence* against the registry — so any authenticated caller can currently assert any tenant. This is a known, **explicitly accepted** trade-off for this round, not an oversight: the real fix (the API key cryptographically resolving the tenant, dropping the header) is a larger cross-cutting change, tracked under Deferred. RLS still contains a compromised or buggy *server* path; it does not yet defend against a *client* lying about its tenant.

**Why the schema is branch-ready but the API stays linear.** Full branching (edit/regenerate/alternate-branch trees) was chosen as the product direction. Making the *schema* tree-shaped is cheap — a `parent_id` self-FK plus a `current_message_id` head pointer. Actually *exposing* branching means changing the gRPC contract (`GenerateReply*` must carry `parent_id`/branch selection) and the chatapp client — a cross-repo change. So we build the tree-capable schema now (future-proofing, zero rework later) and write a linear chain through it for the moment (`parent_id` = previous message). Branch UX is under Deferred.

**Smaller calls, for the record:** money is `DECIMAL(12,6)` everywhere because the old schema mixed `DECIMAL` and `DOUBLE PRECISION`, and float accumulates rounding error on cost sums; raw request/response debug blobs live in a 1:1 side table so the hot `chat_messages` row stays lean and debug data can later get its own retention/TTL; the `007` god-table columns and the `jobs`/`archives` tables are dropped rather than ported because they have zero Go references.

**What the Open WebUI study changed.** A read of Open WebUI's models (`models/chats.py`, `chat_messages.py`, `files.py`, `models.py`) confirmed the core design and surfaced targeted adds, now folded in: an explicit **`sibling_seq`** (OWUI has none — sibling order is implicit via `childrenIds`/`created_at`, its one lossy spot); a **`status_history`** generation timeline and **`embeds`** on messages; a content-dedup **`hash`** on files; the standardized **`sources`** citation shape (`{source, document[], metadata[]}`); and a per-tenant **`airborne_models`** registry (OWUI's `base_model_id` + `params` + `is_active` for named presets/aliases). We deliberately did **not** adopt OWUI's per-chat JSON blob + dual-write (its tech debt, our #1 problem), its composite `{chat}-{msg}` message ids (our global id + FK is cleaner), or stored `childrenIds` (derived from `parent_id`); and message-delete stays **cascade-subtree** rather than OWUI's re-parent-on-delete. Deferred from the study: a feedback/eval table (with OWUI's frozen-`snapshot` idea), conversation-compaction summaries, and unread tracking — see Deferred. Where we're already ahead of OWUI (don't regress): `current_message_id` as a real column, first-class token/cost columns, normalized-first storage, `tenant_id` + RLS, real timestamps, and the raw request/response debug table.

## Global Constraints

- **PostgreSQL 15+** (uses `FORCE ROW LEVEL SECURITY`, recursive CTEs).
- **`tenant_id` is `TEXT`** (`ai8`, `email4ai`, `zztest`) — never `uuid`; avoids delphi's empty-GUC `::uuid` crash.
- **All RLS policies use the fail-safe 2-arg form** `current_setting('airborne.tenant_id', true)` so an unset GUC yields `NULL` → zero rows, never an error.
- **GUCs are transaction-local**: `set_config(name, value, true)` (== `SET LOCAL`).
- **The app MUST connect as a non-privileged DB role.** RLS is bypassed by superusers, `BYPASSRLS` roles, and — without `FORCE` — the table owner. DDL/migrations run as an owner/admin role; the application connects as **`airborne_app`** (`NOSUPERUSER`, `NOBYPASSRLS`, not the table owner), or **RLS enforces nothing at all**. Verify what role the deployed DSN actually authenticates as before trusting isolation.
- **Relational is the sole source of truth.** No conversation blob, no dual-write. Conversation history is reconstructed by walking `parent_id`.
- **Branch-ready, linear for now.** Schema supports a full message tree; the gRPC API keeps writing a linear chain (`parent_id` = previous message). Exposing edit/regenerate branches via proto + chatapp is explicitly deferred.
- **All money is `DECIMAL(12,6)`.** No `DOUBLE PRECISION` for currency.
- **`TIMESTAMPTZ`** for all times (never epoch `BigInteger`). UUID primary keys (no `{chat}-{msg}` composite scheme).
- **No secrets in the DB.** Per-tenant provider keys stay in env/secrets, keyed by slug.
- **Greenfield.** Migrations `001`–`009` are deleted and replaced by one baseline. No data backfill.
- **Redis untouched** (auth keys + rate limiting only; never tenant identity).
- Version bump + CHANGELOG happen once at the end (Task 9), reading `VERSION` at the last moment per repo policy.

---

## File Structure

**Migrations:**
- Create `migrations/001_baseline.sql` — registry, `chats`, `chat_messages`, `chat_message_debug`, `files`, `chat_files`, `chat_vector_stores`, `models`, RLS, triggers, grants, seeds.
- Delete `migrations/001_initial_schema.sql` … `009_solstice_archives_tables.sql`.

**Go — DB layer:**
- Create `internal/db/tenanttx.go` — `WithTenant` / `WithCrossTenant` / `WithAdmin` + registry lookups.
- Create `internal/db/tenantcache.go` — 30s cache for `IsValidTenant`.
- Rewrite `internal/db/models.go` — `Chat`, `ChatMessage` (with `ParentID`, `SiblingSeq`, JSON `Content`), `Model`, `ChatFile`, `FileRecord`, `Citation`.
- Rewrite `internal/db/repository.go` — chat/message tree CRUD + analytics via the tx helpers; keep `TenantRepository`'s cache + `(*Repository, error)` signature.
- Modify `internal/db/postgres.go` — `TenantRepository` no longer validates against a hardcoded map.
- Delete `internal/db/repository_test.go`'s obsolete `ValidTenantIDs`/`TableNames` tests; add new suite.

**Go — test harness (new):**
- Create `internal/db/testmain_test.go` — `testcontainers-go` Postgres + `001_baseline.sql` applier + `newTestClient(t)`.

**Go — service/auth:**
- Modify `internal/service/chat.go` — persist via the new model (linear `parent_id`); registry-backed tenant validation.
- Modify `internal/auth/tenant_interceptor.go`, `internal/server/grpc.go` — inject `*db.Client`, validate tenant against registry.
- Modify `internal/admin/server.go` — cross-tenant reads over the new tables via `WithCrossTenant`.
- Modify `cmd/airborne-freeze/main.go` — enumerate registry tenants if needed.

---

### Task 0: Postgres integration-test harness (prerequisite)

RLS and tree behavior cannot be unit-tested — they need a live Postgres. `internal/db` currently has none (no testcontainers/dockertest/mock anywhere in the repo), so this must exist before any RLS/repo test. **Critically, the harness must connect as a non-superuser `airborne_app` role** — the container's default user is a superuser that bypasses RLS, so tests run as the owner would show *no* isolation (false green). The migration is applied by the owner; the role is created and granted DML; `newTestClient` connects as `airborne_app`.

**Files:**
- Create: `internal/db/testmain_test.go`
- Modify: `go.mod` (add `github.com/testcontainers/testcontainers-go` + `.../modules/postgres`)

**Interfaces:**
- Produces: `func newTestClient(t *testing.T) *Client` — a `*Client` bound to an ephemeral Postgres that has `migrations/001_baseline.sql` applied; skips (`t.Skip`) if Docker is unavailable.

- [ ] **Step 1: Add the dependency**

Run: `go get github.com/testcontainers/testcontainers-go/modules/postgres@latest`

- [ ] **Step 2: Write the harness**

```go
package db

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

// appDSN connects as the non-privileged airborne_app role (RLS ENFORCED).
// ownerDSN connects as the container owner/superuser (RLS BYPASSED) — setup/cleanup only.
var appDSN, ownerDSN string

func TestMain(m *testing.M) {
	ctx := context.Background()
	container, err := tcpostgres.Run(ctx, "postgres:16-alpine",
		tcpostgres.WithDatabase("airborne"),
		tcpostgres.WithUsername("owner"), tcpostgres.WithPassword("owner"),
		tcpostgres.WithInitScripts("../../migrations/001_baseline.sql"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).WithStartupTimeout(60*time.Second)),
	)
	if err != nil {
		// Docker unavailable: skip the package's DB tests (do not fail).
		fmt.Fprintf(os.Stderr, "skipping db tests, docker unavailable: %v\n", err)
		os.Exit(0)
	}
	ownerDSN, err = container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		// Docker present but setup broke: FAIL loudly, never mask with exit 0.
		fmt.Fprintf(os.Stderr, "db test setup failed (dsn): %v\n", err)
		os.Exit(1)
	}
	// RLS only enforces against a non-superuser, non-owner role. Create it and
	// grant DML; tests connect as airborne_app so FORCE RLS actually applies.
	if err := createAppRole(ctx, ownerDSN); err != nil {
		fmt.Fprintf(os.Stderr, "db test setup failed (role): %v\n", err)
		os.Exit(1)
	}
	appDSN = strings.Replace(ownerDSN, "owner:owner@", "airborne_app:app@", 1)

	code := m.Run()
	_ = container.Terminate(ctx)
	os.Exit(code)
}

func createAppRole(ctx context.Context, ownerDSN string) error {
	pool, err := pgxpool.New(ctx, ownerDSN)
	if err != nil {
		return err
	}
	defer pool.Close()
	_, err = pool.Exec(ctx, `
		CREATE ROLE airborne_app LOGIN PASSWORD 'app' NOSUPERUSER NOBYPASSRLS;
		GRANT USAGE ON SCHEMA public TO airborne_app;
		GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO airborne_app;
		GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO airborne_app;`)
	return err
}

func newTestClient(t *testing.T) *Client {
	t.Helper()
	if appDSN == "" {
		t.Skip("no Postgres test container (Docker unavailable)")
	}
	// Connect as airborne_app (NOT the owner) so RLS is enforced. Real signature
	// is NewClient(ctx, Config) — see internal/db/postgres.go.
	c, err := NewClient(context.Background(), Config{URL: appDSN})
	if err != nil {
		t.Fatalf("connect test db as airborne_app: %v", err)
	}
	t.Cleanup(func() { truncateAll(t) })
	return c
}

// truncateAll cleans up as the owner (airborne_app lacks TRUNCATE, and RLS would
// otherwise scope a DELETE to a single tenant).
func truncateAll(t *testing.T) {
	pool, err := pgxpool.New(context.Background(), ownerDSN)
	if err != nil {
		t.Logf("cleanup connect: %v", err)
		return
	}
	defer pool.Close()
	_, err = pool.Exec(context.Background(),
		`TRUNCATE airborne_chat_files, airborne_chat_message_debug, airborne_chat_messages,
		          airborne_chat_vector_stores, airborne_files, airborne_chats RESTART IDENTITY CASCADE`)
	if err != nil {
		t.Logf("truncate: %v", err)
	}
}
```

> `NewClient(ctx, Config)` is the real constructor (`internal/db/postgres.go`); `Config{URL: …}` carries the DSN. The `WithInitScripts` path is relative to the test working dir (`internal/db`), hence `../../migrations`. The role swap assumes the DSN embeds `owner:owner@`; adjust the `strings.Replace` if the container credentials differ.

- [ ] **Step 3: Verify it compiles and skips gracefully**

Run: `go test -mod=mod ./internal/db/ -run TestMain -v`
Expected: builds; either runs a container or exits 0 cleanly when Docker is absent.

- [ ] **Step 4: Commit**

```bash
git add internal/db/testmain_test.go go.mod go.sum
git commit -m "test(db): testcontainers Postgres harness applying the baseline schema"
```

---

### Task 1: Baseline schema migration

**Files:**
- Create: `migrations/001_baseline.sql`
- Delete: `migrations/001_initial_schema.sql` … `009_solstice_archives_tables.sql`

- [ ] **Step 1: Registry + core tables**

```sql
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
    metadata            JSONB,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT valid_chat_status CHECK (status IN ('active','archived','deleted'))
);
CREATE INDEX idx_chats_tenant_user_updated ON airborne_chats(tenant_id, user_id, updated_at DESC);
CREATE INDEX idx_chats_tenant_pinned ON airborne_chats(tenant_id, user_id, pinned) WHERE pinned;
CREATE UNIQUE INDEX idx_chats_share ON airborne_chats(share_id) WHERE share_id IS NOT NULL;

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
```

- [ ] **Step 2: Triggers, RLS, grants, seeds** (append to the same file)

```sql
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
```

- [ ] **Step 3: Delete the old migrations**

```bash
git rm migrations/00[1-9]_*.sql
```

- [ ] **Step 4: Verify it applies + RLS is on**

```bash
createdb airborne_scratch 2>/dev/null
psql -d airborne_scratch -v ON_ERROR_STOP=1 -f migrations/001_baseline.sql && echo MIGRATION_OK
psql -d airborne_scratch -c "SELECT relname FROM pg_class WHERE relrowsecurity AND relkind='r' ORDER BY 1;"
dropdb airborne_scratch
```
Expected: `MIGRATION_OK`; all 6 entity tables + registry listed.

- [ ] **Step 5: Commit**

```bash
git add migrations/
git commit -m "feat(db): relational chat/chat_message baseline schema with RLS + registry"
```

---

### Task 2: Tenant-aware tx helpers + registry lookups

**Files:** Create `internal/db/tenanttx.go`, `internal/db/tenantcache.go`.

**Interfaces:** Produces `WithTenant`, `WithCrossTenant`, `WithAdmin`, `TenantExists`, `ListTenantIDs`, `IsValidTenant` (30s cache).

- [ ] **Step 1: Failing test** — set the GUC and read it back:

```go
func TestWithTenant_SetsGUC(t *testing.T) {
	c := newTestClient(t)
	var got string
	if err := c.WithTenant(context.Background(), "ai8", func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(),
			"SELECT current_setting('airborne.tenant_id', true)").Scan(&got)
	}); err != nil { t.Fatal(err) }
	if got != "ai8" { t.Fatalf("got %q", got) }
}
```

- [ ] **Step 2: Run — expect FAIL** (`WithTenant undefined`).
  Run: `go test -mod=mod ./internal/db/ -run TestWithTenant_SetsGUC -v`

- [ ] **Step 3: Implement `tenanttx.go`**

```go
package db

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

func (c *Client) WithTenant(ctx context.Context, tenantID string, fn func(pgx.Tx) error) error {
	return c.withGUCs(ctx, map[string]string{"airborne.tenant_id": tenantID}, fn)
}
func (c *Client) WithCrossTenant(ctx context.Context, fn func(pgx.Tx) error) error {
	return c.withGUCs(ctx, map[string]string{"airborne.cross_tenant_mode": "true"}, fn)
}
func (c *Client) WithAdmin(ctx context.Context, fn func(pgx.Tx) error) error {
	return c.withGUCs(ctx, map[string]string{"airborne.admin_mode": "true"}, fn)
}

func (c *Client) withGUCs(ctx context.Context, gucs map[string]string, fn func(pgx.Tx) error) (err error) {
	tx, err := c.pool.Begin(ctx)
	if err != nil { return fmt.Errorf("begin: %w", err) }
	defer func() { if err != nil { _ = tx.Rollback(ctx) } }()
	for name, val := range gucs {
		if _, err = tx.Exec(ctx, "SELECT set_config($1,$2,true)", name, val); err != nil {
			return fmt.Errorf("set guc %s: %w", name, err)
		}
	}
	if err = fn(tx); err != nil { return err }
	if err = tx.Commit(ctx); err != nil { return fmt.Errorf("commit: %w", err) }
	return nil
}

func (c *Client) TenantExists(ctx context.Context, id string) (bool, error) {
	var ok bool
	err := c.pool.QueryRow(ctx,
		"SELECT EXISTS(SELECT 1 FROM airborne_tenants WHERE id=$1 AND status='active')", id).Scan(&ok)
	return ok, err
}
func (c *Client) ListTenantIDs(ctx context.Context) ([]string, error) {
	rows, err := c.pool.Query(ctx, "SELECT id FROM airborne_tenants WHERE status='active' ORDER BY id")
	if err != nil { return nil, err }
	defer rows.Close()
	var ids []string
	for rows.Next() { var id string; if err := rows.Scan(&id); err != nil { return nil, err }; ids = append(ids, id) }
	return ids, rows.Err()
}
```

- [ ] **Step 4: Implement `tenantcache.go`** (30s cache over `ListTenantIDs`, exposing `IsValidTenant(ctx, id) (bool, error)`). Use `sync.RWMutex` + `time.Now()`; `time.Now()` is allowed in production code.

- [ ] **Step 5: Run — expect PASS.**
  Run: `go test -mod=mod ./internal/db/ -run TestWithTenant_SetsGUC -v`

- [ ] **Step 6: Commit**
```bash
git add internal/db/tenanttx.go internal/db/tenantcache.go internal/db/rls_test.go
git commit -m "feat(db): tenant-aware tx helpers + cached registry validation"
```

---

### Task 3: RLS isolation, cross-tenant, and immutability tests

**Files:** Create `internal/db/rls_test.go`.

- [ ] **Step 1: Write the tests** — insert as `ai8`; assert `email4ai` sees 0 rows; assert `WithCrossTenant` sees the row; assert inserting an `email4ai` row under the `ai8` GUC is rejected (`WITH CHECK`); assert changing `tenant_id` on update raises (immutability trigger). (Same structure as the four tests in the prior proposal, retargeted to `airborne_chats`.)

- [ ] **Step 2: Run — expect PASS.**
  Run: `go test -mod=mod ./internal/db/ -run TestRLS -v`

- [ ] **Step 3: Commit**
```bash
git add internal/db/rls_test.go
git commit -m "test(db): RLS isolation, cross-tenant read, write-check, immutability"
```

---

### Task 4: Rewrite Go models (`Chat`, `ChatMessage` tree, JSON content)

**Files:** Rewrite `internal/db/models.go`; update `internal/db/models_test.go`.

**Interfaces:** Produces:
- `type Chat struct { ID, TenantID, UserID, Title, ModelID, Provider, Status string; CurrentMessageID *string; Pinned bool; Metadata json.RawMessage; CreatedAt, UpdatedAt time.Time }`
- `type ChatMessage struct { ID, TenantID, ChatID string; ParentID *string; SiblingSeq int; UserID, Role string; Content json.RawMessage; ModelID, Provider, ResponseID *string; Status string; StatusHistory json.RawMessage; InputTokens, OutputTokens, TotalTokens *int; CostUSD, GroundingCostUSD *float64; Usage, Sources, Embeds json.RawMessage; CreatedAt, UpdatedAt time.Time }`
- `type Model struct { ID, TenantID string; BaseModelID, Name, Provider *string; Params, Meta json.RawMessage; IsActive bool; CreatedAt, UpdatedAt time.Time }` — a per-tenant model-registry row (Task 5 adds CRUD).
- Add `Hash *string` to the file record type.
- `func TextContent(s string) json.RawMessage` — wraps a plain string as the JSON content form (e.g. `{"text": s}`), so callers with a legacy string still produce valid `content`.
- **Preserve `ActivityEntry`, `DebugData`, `ThreadConversation`** (currently `models.go:66/89/224`) — Tasks 5 and 7 still return them. Keep the types, but update their fields to the new model (`ChatID`/`ModelID`; source `tenant_id` from the row; `DebugData` maps `airborne_chat_message_debug` columns). Do **not** delete them along with `Thread`/`Message`.

- [ ] **Step 1: Write model tests** — `TextContent("hi")` round-trips; `ChatMessage` with a nil `ParentID` marshals as a root.
- [ ] **Step 2: Run — expect FAIL** (types undefined).
- [ ] **Step 3: Implement the structs + `TextContent`**. Remove the old `Thread`/`Message` structs and the `Content string` field; **keep and adapt `ActivityEntry`/`DebugData`/`ThreadConversation`** (Tasks 5/7 depend on them). **NOT-NULL JSONB guard:** `content` and `airborne_models.params`/`meta` are `NOT NULL`, and a nil `json.RawMessage` marshals to SQL `NULL` (a column `DEFAULT` does *not* apply when `NULL` is explicitly passed). Ensure `content` is always set via `TextContent` (never nil), and default empty `params`/`meta` to `[]byte("{}")` in the model layer so inserts can't violate the constraint.
- [ ] **Step 4: Run — expect PASS.**
  Run: `go test -mod=mod ./internal/db/ -run 'TestChatMessage|TestTextContent' -v`
- [ ] **Step 5: Commit**
```bash
git add internal/db/models.go internal/db/models_test.go
git commit -m "refactor(db): Chat/ChatMessage models with tree + JSON content"
```

---

### Task 5: Rewrite the repository (tree CRUD + analytics)

**Files:** Rewrite `internal/db/repository.go`; modify `internal/db/postgres.go`; delete obsolete tests in `internal/db/repository_test.go`.

**Interfaces:** Produces (all tenant-scoped via `WithTenant`):
- `NewTenantRepository(c *Client, tenantID string) (*Repository, error)` — **keeps the `(…, error)` signature** (returns nil error; validation is upstream) so the cached `TenantRepository` and its 5 call sites are untouched.
- `func (r *Repository) CreateChat(ctx, chat *Chat) error`
- `func (r *Repository) AppendMessage(ctx, chatID string, parentID *string, msg *ChatMessage) (string, error)` — inserts with `parent_id`, then sets `chats.current_message_id`; both in one `WithTenant` tx. If `parentID` is nil, uses the chat's current head (linear append).
- `func (r *Repository) GetActiveBranch(ctx, chatID string) ([]ChatMessage, error)` — recursive-CTE walk from `current_message_id` to root.
- `func (r *Repository) ListChats(ctx, userID string, limit int) ([]Chat, error)`
- `func (r *Repository) GetActivityFeedAllTenants(ctx, limit int) ([]ActivityEntry, error)` — via `WithCrossTenant`.
- `func (r *Repository) GetSiblings(ctx, chatID string, parentID *string) ([]ChatMessage, error)` — children of a parent, `ORDER BY sibling_seq, created_at, id`. The trailing keys break ties deterministically if two concurrent same-parent appends compute the same `sibling_seq` (the `MAX(sibling_seq)+1` read is racy under READ COMMITTED and the index is intentionally non-unique) — so `sibling_seq` stays authoritative for explicit reordering, but ordering never depends on it being unique.
- **Model registry CRUD:** `UpsertModel(ctx, *Model) error`, `ListModels(ctx) ([]Model, error)`, `ResolveModel(ctx, id string) (*Model, error)` — the last returns the active registry row (with `base_model_id`/`params`) or `nil` if the id isn't registered. All via `WithTenant`.

- [ ] **Step 1: Delete obsolete tests + dead scaffolding**

Remove from `internal/db/repository_test.go`: `TestValidTenantIDs`, `TestNewTenantRepository_ValidTenants/_InvalidTenants`, `TestRepository_TableNames_*` (they test `ValidTenantIDs`/`tablePrefix`, which are gone). Remove `ValidTenantIDs`, `ErrInvalidTenant`, `tablePrefix`, and all `*Table()` helpers from `repository.go`. New `Repository{ client *Client; tenantID string }`. Add a test helper `func newUUID() string { return uuid.NewString() }` (used by the tests below).

Keep `postgres.go`'s `TenantRepository (*Repository, error)` **with its `c.tenantRepos` cache and double-checked locking** — only its inner `NewTenantRepository` call loses the hardcoded-map validation.

- [ ] **Step 2: Failing test for the tree round-trip**

```go
func TestAppendMessage_LinearBranchRoundTrip(t *testing.T) {
	c := newTestClient(t)
	ctx := context.Background()
	repo, _ := NewTenantRepository(c, "ai8")
	chat := &Chat{ID: newUUID(), TenantID: "ai8", UserID: "u1", Status: "active"}
	if err := repo.CreateChat(ctx, chat); err != nil { t.Fatal(err) }

	m1, _ := repo.AppendMessage(ctx, chat.ID, nil,
		&ChatMessage{ID: newUUID(), TenantID: "ai8", ChatID: chat.ID, UserID: "u1", Role: "user", Content: TextContent("hi"), Status: "complete"})
	_, _ = repo.AppendMessage(ctx, chat.ID, &m1,
		&ChatMessage{ID: newUUID(), TenantID: "ai8", ChatID: chat.ID, UserID: "u1", Role: "assistant", Content: TextContent("hello"), Status: "complete"})

	branch, err := repo.GetActiveBranch(ctx, chat.ID)
	if err != nil { t.Fatal(err) }
	if len(branch) != 2 || branch[0].Role != "user" || branch[1].Role != "assistant" {
		t.Fatalf("branch = %+v", branch)
	}
}
```

- [ ] **Step 3: Run — expect FAIL.**
  Run: `go test -mod=mod ./internal/db/ -run TestAppendMessage_LinearBranchRoundTrip -v`

- [ ] **Step 4: Implement `AppendMessage` + `GetActiveBranch`**

```go
func (r *Repository) AppendMessage(ctx context.Context, chatID string, parentID *string, m *ChatMessage) (string, error) {
	err := r.client.WithTenant(ctx, r.tenantID, func(tx pgx.Tx) error {
		if parentID == nil { // linear append: parent = current head
			var head *string
			if err := tx.QueryRow(ctx, `SELECT current_message_id FROM airborne_chats WHERE id=$1`, chatID).Scan(&head); err != nil {
				return err
			}
			parentID = head
		}
		// Deterministic sibling ordering: next seq among children of this parent.
		var seq int
		if err := tx.QueryRow(ctx,
			`SELECT COALESCE(MAX(sibling_seq)+1, 0) FROM airborne_chat_messages
			 WHERE chat_id=$1 AND parent_id IS NOT DISTINCT FROM $2`, chatID, parentID).Scan(&seq); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO airborne_chat_messages
			  (id, tenant_id, chat_id, parent_id, sibling_seq, user_id, role, content, model_id, provider,
			   response_id, status, status_history, input_tokens, output_tokens, total_tokens, cost_usd,
			   grounding_queries, grounding_cost_usd, processing_time_ms, usage, sources, embeds)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23)`,
			m.ID, r.tenantID, chatID, parentID, seq, m.UserID, m.Role, m.Content, m.ModelID, m.Provider,
			m.ResponseID, m.Status, m.StatusHistory, m.InputTokens, m.OutputTokens, m.TotalTokens, m.CostUSD,
			m.GroundingQueries, m.GroundingCostUSD, m.ProcessingTimeMs, m.Usage, m.Sources, m.Embeds); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE airborne_chats SET current_message_id=$1 WHERE id=$2`, m.ID, chatID)
		return err
	})
	return m.ID, err
}

func (r *Repository) GetActiveBranch(ctx context.Context, chatID string) ([]ChatMessage, error) {
	var out []ChatMessage
	err := r.client.WithTenant(ctx, r.tenantID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			WITH RECURSIVE branch AS (
			  SELECT m.* FROM airborne_chat_messages m
			  JOIN airborne_chats c ON c.current_message_id = m.id
			  WHERE c.id = $1
			  UNION ALL
			  SELECT p.* FROM airborne_chat_messages p
			  JOIN branch b ON p.id = b.parent_id
			)
			SELECT id, tenant_id, chat_id, parent_id, user_id, role, content, model_id, provider,
			       status, input_tokens, output_tokens, total_tokens, cost_usd, created_at
			FROM branch ORDER BY created_at ASC`, chatID)
		if err != nil { return err }
		defer rows.Close()
		for rows.Next() {
			var m ChatMessage
			if err := rows.Scan(&m.ID, &m.TenantID, &m.ChatID, &m.ParentID, &m.UserID, &m.Role,
				&m.Content, &m.ModelID, &m.Provider, &m.Status, &m.InputTokens, &m.OutputTokens,
				&m.TotalTokens, &m.CostUSD, &m.CreatedAt); err != nil {
				return err
			}
			out = append(out, m)
		}
		return rows.Err()
	})
	return out, err
}
```

Implement `CreateChat`, `ListChats`, `GetActivityFeedAllTenants` (under `WithCrossTenant`), `GetSiblings` (`ORDER BY sibling_seq, created_at, id`), and the model-registry CRUD (`UpsertModel`/`ListModels`/`ResolveModel`) by the same pattern. **Add tests:** (a) `ResolveModel` returns a registered alias's `base_model_id`/`params` and `nil` for an unregistered id; (b) two `AppendMessage` calls sharing a parent get `sibling_seq` 0 then 1, and `GetSiblings` returns them in that order.

- [ ] **Step 5: Run — expect PASS.**
  Run: `go test -mod=mod ./internal/db/ -run 'TestAppend|TestRLS|TestWithTenant' -v`

- [ ] **Step 6: Commit**
```bash
git add internal/db/repository.go internal/db/postgres.go internal/db/repository_test.go
git commit -m "refactor(db): relational chat/message repository with tree CRUD + cross-tenant admin"
```

---

### Task 6: Wire the service + auth layers to the new model

**Files:** Modify `internal/service/chat.go`, `internal/auth/tenant_interceptor.go`, `internal/server/grpc.go`, `cmd/airborne-freeze/main.go`.

- [ ] **Step 1: Registry-backed tenant validation.** Inject `*db.Client` into `TenantInterceptor`; in `resolveTenant` replace the manager-only existence check with `dbClient.IsValidTenant(ctx, tenantID)`, then load provider config from the manager. Replace `db.ValidTenantIDs[...]` at `chat.go:1064,1222` with `s.dbClient.IsValidTenant(ctx, tenantID)`.
- [ ] **Step 2: Persistence path.** Where `chat.go` persisted a conversation turn, call `repo.CreateChat` (if new) then two `repo.AppendMessage` calls (user message, then assistant message with `parentID` = the user message id). Populate tokens/cost/usage/sources on the assistant `ChatMessage`. Write debug blobs to `airborne_chat_message_debug` in the same flow if debug capture is on. **Model aliases:** before calling the provider, resolve the requested `model_id` via `repo.ResolveModel` — if it's a registered alias, substitute `base_model_id` and merge its `params` (request values override registry defaults); unregistered ids pass through unchanged. Record the *resolved* `model_id` on the assistant message.
- [ ] **Step 3: `airborne-freeze`** — source the tenant list from `dbClient.ListTenantIDs(ctx)` if it enumerates tenants.
- [ ] **Step 4: Build + targeted tests.**
  Run: `go build -mod=mod ./... && go test -mod=mod ./internal/auth/... ./internal/service/... -v`
  Expected: PASS; fix any interceptor constructor call sites that now need a `*db.Client`.
- [ ] **Step 5: Commit**
```bash
git add internal/service/chat.go internal/auth/tenant_interceptor.go internal/server/grpc.go cmd/airborne-freeze/main.go
git commit -m "refactor(service): persist via chat/chat_message model; registry-backed tenants"
```

---

### Task 7: Admin dashboard over the new tables

**Files:** Modify `internal/admin/server.go`.

- [ ] **Step 1: Preserve the admin JSON contract.** The CLI (`internal/cli/client.go:53 ActivityResponse`) and the Next.js admin dashboard consume the current response shapes. In the handlers, **map the new model back to the existing JSON field names** (keep the keys the frontend expects — e.g. `thread_id`/`model`) rather than leaking the renamed columns. Do not change the wire shape in this task; a separate effort updates the dashboard if we want new fields.
- [ ] **Step 2:** Repoint `GetActivityFeedAllTenants` / `GetDebugDataAllTenants` / `GetThreadConversationAllTenants` admin handlers to the new repository methods (single queries under `WithCrossTenant`, selecting `tenant_id` from the row — no per-tenant UNION). Debug data now joins `airborne_chat_message_debug`.
- [ ] **Step 3:** Build + run admin tests.
  Run: `go build -mod=mod ./internal/admin/... && go test -mod=mod ./internal/admin/... -v`
- [ ] **Step 4: Commit**
```bash
git add internal/admin/server.go
git commit -m "refactor(admin): cross-tenant reads over unified chat_message tables"
```

---

### Task 8: Full build, vet, test sweep

- [ ] **Step 1:** `go mod tidy && go build ./... && go vet ./...` — clean (repo currently fails default build until tidied).
- [ ] **Step 2:** `go test -mod=mod -count=1 ./... 2>&1 | tail -40` — all packages `ok`.
- [ ] **Step 3:** Leftover scan — expect no matches:
```bash
grep -rn "ValidTenantIDs\|tablePrefix\|_airborne_\|conversation_history\|airborne_threads\|airborne_messages\b" internal cmd migrations --include='*.go' --include='*.sql' | grep -v '_test.go'
```
- [ ] **Step 4:** Commit any tidy changes.

---

### Task 9: Docs, VERSION, CHANGELOG

- [ ] **Step 1:** Repoint any migration runner/docs (`Makefile`, `README.md`, `docs/`) to `migrations/001_baseline.sql`.
- [ ] **Step 2:** Read `VERSION` at the last moment; bump (revisions cap at 15 → then minor).
- [ ] **Step 3:** CHANGELOG entry describing the schema overhaul; note the coding agent + model.
- [ ] **Step 4:** `git add -A && git commit && git push`.

---

## Deferred (explicitly out of scope)

- **Branching UX end-to-end** — proto (`GenerateReply*` carrying `parent_id`/branch selection) + chatapp client. The schema is branch-ready; the API writes a linear chain this round.
- **Secure tenancy** — the caller still self-declares its tenant (validated only for existence against the registry). This is an **explicitly accepted interim trade-off**, not an oversight; the real fix — the API key resolving the tenant and dropping `x-tenant-id` — is a separate cross-repo effort.
- **Folders & sharing** — `airborne_chats` carries `folder_id`/`share_id`/`pinned` as forward-looking columns, but no `folders`/`shares` tables or org/sharing features are built. (If we'd rather not carry unused columns, drop them until the feature lands — a YAGNI call to make before implementation.)
- **Analytics / billing rollups** — per-tenant/model/day cost is aggregated on the fly via the indexed analytics columns; no materialized rollup or usage-quota tables are built (limitation #7 from the schema audit).
- **Table partitioning** — `airborne_chat_messages` is unpartitioned. Time/tenant partitioning is a scale concern to revisit once volume warrants it (limitation #6).
- **Admin dashboard / CLI new fields** — Task 7 keeps the existing JSON contract stable; actually surfacing new columns (e.g. `model_id`, branch info) to the Next.js dashboard + CLI is a follow-up.
- **Dropping Redis; moving provider secrets or API keys into Postgres** — Redis (auth keys + rate limiting) is untouched; provider secrets stay in env/secrets.
- **`pricing_db`** — left as-is this round; it's a static rate-card library, unrelated to the schema, and its local `replace`-directive cleanup is a separate concern.
- **Feedback / eval table** (from the Open WebUI study) — a decoupled `feedback` table (`type` + `data{rating,reason,comment}` + `meta{chat_id,message_id}` + a frozen `snapshot` of the conversation at rating time). No FK entanglement; trivial to add when eval/RLHF lands. Copy OWUI's `snapshot` idea specifically.
- **Conversation compaction** — chat-level `summary` + message-level `context_summary` (OWUI) for long-context rollup. Add if/when compaction is a goal.
- **`last_read_at` / unread tracking** and splitting message `content` from a structured `output` column (OWUI) — deferred UI/rendering refinements.
- **Model-registry governance depth** — the `airborne_models` table + CRUD + alias resolution land now; richer per-tenant model governance (access grants, capability gating) is a follow-up.
- **Debug-blob retention/TTL job** — the 1:1 side table enables it; the sweeper is a follow-up.

## Self-Review Notes

- **Duplication killed:** no blob, no dual-write; `chat_messages` is the sole source of truth (addresses the "stored twice" root cause). ✓
- **Prior /check fixes folded in:** D1 (keep `TenantRepository` cache + `(*Repository, error)` signature — Task 5 Step 1), D2 (Task 0 test harness — no longer "an open item"), D3 (delete obsolete `ValidTenantIDs`/`TableNames` tests — Task 5 Step 1). ✓
- **Schema-limitation coverage:** god-table split (debug side table + no dead 007 cols), money `DECIMAL` consistency, typed+indexed analytics columns, real FKs + `chat_file` join, `TIMESTAMPTZ`, `CHECK` enums, no cached `message_count` drift. ✓
- **RLS correctness:** fail-safe `current_setting(...,true)`; cross-tenant admin mode; immutability trigger; **enforced via a non-superuser `airborne_app` role** (superusers/owners bypass RLS). ✓
- **Type consistency:** `WithTenant`/`WithCrossTenant`/`WithAdmin`/`IsValidTenant`/`AppendMessage`/`GetActiveBranch` names used identically across Tasks 2–7; `ActivityEntry`/`DebugData`/`ThreadConversation` preserved (Task 4). ✓
- **Second /check fixes folded in:** F1 (non-superuser `airborne_app` role in Task 0 + Global Constraints + migration grants + `NewClient(Config)` call), F2 (preserve the three admin types — Task 4), F3 (admin JSON contract pinned — Task 7 Step 1), F4 (dropped the unused view), F5 (harness fails loudly on setup error, skips only when Docker is absent), F6 (`newUUID` defined — Task 5). ✓
- **Open WebUI study folded in:** explicit `sibling_seq` ordering, `status_history`/`embeds` on messages, file `hash`, standardized `sources` shape, and the per-tenant `airborne_models` registry (`base_model_id`/`params`/`is_active`) + alias resolution added (Tasks 1/4/5/6); OWUI's blob/dual-write, composite ids, and stored `childrenIds` deliberately skipped; message-delete stays cascade-subtree; feedback/eval + compaction + unread deferred. ✓
- **Open item:** confirm the container credentials embed `owner:owner@` for the `strings.Replace` role swap; adjust if the deployed/test DSN differs. Confirm the production DSN authenticates as a `NOSUPERUSER NOBYPASSRLS` role (else RLS is inert).
