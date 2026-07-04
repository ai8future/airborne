# Addendum — email_ai_svc integration hooks

**Date:** 2026-07-04
**Attaches to:** `airborne-schema-overhaul.md` (the relational chat/chat_message overhaul). This is a **separate file on purpose** — that proposal is under active edit; fold these two tasks in when convenient (they slot alongside Tasks 1, 5, 6, before the Task 9 finalizer).

> **Why:** the new `email_ai_svc` — a stateless, **zero-database** email-AI orchestrator replacing `solstice` — depends on airborne owning conversation state, cost, and idempotency so it can hold no tables of its own. These two hooks are small and **piggyback on the baseline migration this overhaul is already writing** (avoids a second migration + a second pass over the service layer). Neither leaks email/RFC-5322 semantics into airborne: both are generic — an opaque correlation ref on a chat, and a generic idempotency key on generate. `email_ai_svc` owns all email semantics (Message-ID assembly, tracking code, quote parsing). Full context: `email_suite/docs/superpowers/specs/2026-07-03-email-ai-svc-design.md` §7.2 & §10.
>
> **Contract note (good news):** the overhaul deliberately keeps `GenerateReply*` **linear/stable** (branching deferred, per its Design Rationale). These hooks preserve that — A9 is schema+repo only; A10 adds *optional* fields honored server-side. `email_ai_svc` already carries both values in the existing `GenerateReplyRequest.metadata` map, so honoring metadata (A10 Step 3) makes it work with **zero client change**.

---

### Addendum Task A9: Opaque `external_ref` correlation on `airborne_chats`

Lets a caller attach/resolve a chat by its own opaque id. `email_ai_svc` passes a conversation UUID (minted into its encrypted email tracking code); when a reply arrives with its in-band quoted history stripped, airborne is the fallback that reloads the conversation by this ref. airborne treats it as an opaque string.

**Files:** Modify `migrations/001_baseline.sql` (Task 1); `internal/db/repository.go` (Task 5); `internal/service/chat.go` (Task 6).

**Interfaces:**
- Produces: `Repository.GetChatByExternalRef(ctx, externalRef string) (*Chat, bool, error)`; `CreateChat` accepts an `externalRef` field on the `Chat` (empty = today's behavior).

- [ ] **Step 1: Add the column + unique index to `airborne_chats` in `001_baseline.sql`**

In the `CREATE TABLE airborne_chats (...)` block, add after `share_id TEXT`:
```sql
    external_ref  TEXT,   -- opaque caller correlation id (e.g. email_ai_svc conversation uuid)
```
And beside `idx_chats_share`, add:
```sql
CREATE UNIQUE INDEX idx_chats_external
    ON airborne_chats(tenant_id, external_ref) WHERE external_ref IS NOT NULL;
```
(Unique per tenant so one correlation id maps to exactly one chat; RLS already scopes the lookup.)

- [ ] **Step 2: Failing test** — in `internal/db/rls_test.go`: under `WithTenant("ai8", ...)` create a chat with `ExternalRef:"conv-1"`, assert `GetChatByExternalRef(ctx,"conv-1")` returns it; assert the same ref under `WithTenant("email4ai", ...)` returns `false` (RLS isolation holds for the lookup). `go test ./internal/db/ -run TestExternalRef -v` → FAIL (undefined).

- [ ] **Step 3: Implement** — add `ExternalRef string` to the `Chat` model (Task 4); in `CreateChat` include `external_ref` in the INSERT; add:
```go
func (r *Repository) GetChatByExternalRef(ctx context.Context, externalRef string) (*Chat, bool, error) {
	var c Chat
	err := r.client.WithTenant(ctx, r.tenantID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT id, tenant_id, user_id, title, model_id, provider, status,
			        current_message_id, external_ref, created_at, updated_at
			 FROM airborne_chats WHERE external_ref = $1`, externalRef).Scan(
			&c.ID, &c.TenantID, &c.UserID, &c.Title, &c.ModelID, &c.Provider, &c.Status,
			&c.CurrentMessageID, &c.ExternalRef, &c.CreatedAt, &c.UpdatedAt)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("get chat by external_ref: %w", err)
	}
	return &c, true, nil
}
```

- [ ] **Step 4:** `go test ./internal/db/ -run 'TestExternalRef|TestRLS' -v` → PASS. **Step 5:** commit (`feat(db): opaque external_ref correlation on airborne_chats`).

---

### Addendum Task A10: Idempotent `GenerateReply` (gRPC)

A duplicate `GenerateReply` must **replay the identical prior response** instead of regenerating (which re-charges the LLM and, worse, yields a *different* reply body). Byte-identical replay is required so `email_ai_svc`'s downstream `email_svc` `idemkit` send — which fingerprints `SHA256(body)` — replays instead of returning `422`. airborne already has the pattern on its admin path (`internal/admin/server.go`: `RedisClient`, `RequestID`, 24h TTL); this extends it to the gRPC generate path.

**Files:** Modify `api/proto/airborne/v1/airborne.proto`; `internal/service/chat.go` (Task 6, the persistence/generate path).

**Interfaces:**
- Produces: `GenerateReply` honors an idempotency key + external ref from **either** a first-class field **or** the existing `metadata` map (so email_ai_svc's current metadata-based client works with no change).

- [ ] **Step 1: Add optional first-class fields to `GenerateReplyRequest`** (next free tags after the current max), then `buf generate`:
```proto
string idempotency_key = 21; // dedup key; also read from metadata["idempotency_key"]
string external_ref     = 22; // opaque chat correlation; also metadata["external_ref"]
```
(Adding fields is backward-compatible; the contract stays linear — no `parent_id`/branch selection added, per the overhaul's Design Rationale.)

- [ ] **Step 2: Failing test** — in `internal/service`, call `GenerateReply` twice with the same `idempotency_key` against a generator stub that increments a counter; assert the counter is `1` and the two responses are byte-equal (`proto.Equal`). `go test ./internal/service/ -run TestGenerateReply_Idempotent -v` → FAIL.

- [ ] **Step 3: Implement** — at the top of the `GenerateReply` handler, before provider dispatch:
```go
key := req.GetIdempotencyKey()
if key == "" { key = req.GetMetadata()["idempotency_key"] }
ref := req.GetExternalRef()
if ref == "" { ref = req.GetMetadata()["external_ref"] }

if key != "" {
	if cached, hit, err := s.idem.Get(ctx, tenantID, key); err == nil && hit {
		return cached, nil // byte-identical replay; handler does NOT run again
	}
}
// ... generate ...
// on success: s.idem.Put(ctx, tenantID, key, resp, 24*time.Hour)
// on provider error: s.idem.Release(ctx, tenantID, key)  // do NOT cache failures
```
Reuse the admin path's Redis idempotency helper (namespace the key by tenant, mirroring the A12 tenant-scoping the overhaul already applies). Then pass `ref` into the persistence path: `chat, hit := repo.GetChatByExternalRef(ctx, ref)` (A9) → reuse the chat if hit, else `repo.CreateChat(... ExternalRef: ref ...)`, so the turn persists under the caller's correlation id.

- [ ] **Step 4:** `go test ./internal/service/ -run TestGenerateReply_Idempotent -v` → PASS. **Step 5:** commit (`feat(chat): idempotent GenerateReply keyed by idempotency_key (+external_ref)`).

---

**For the Task 9 finalizer:** fold A9 + A10 into the same VERSION bump + CHANGELOG entry, and add both to the Self-Review "Spec coverage" list.
