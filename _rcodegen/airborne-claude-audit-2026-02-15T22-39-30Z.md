Date Created: 2026-02-15T22:39:30Z
TOTAL_SCORE: 58/100

# Airborne Comprehensive Audit Report

**Project:** `github.com/ai8future/airborne` v1.8.3
**Auditor:** Claude Code (Claude:Opus 4.6)
**Scope:** Full codebase — Go backend (cmd/, internal/, gen/), Next.js dashboard, configuration, infrastructure, migrations, CI/CD

---

## Score Breakdown

| Category | Weight | Score | Notes |
|----------|--------|-------|-------|
| Security | 30% | 14/30 | 8 CRITICAL/HIGH security issues (admin no-auth, API keys in URLs, XSS, SSRF, no TLS enforcement) |
| Bug-Prone Code | 25% | 14/25 | Broken migration (006), race conditions, unbounded memory reads, response double-read |
| Code Quality | 20% | 15/20 | Good structure overall; some 1300-line monolith components and duplicated interfaces |
| Infrastructure | 15% | 9/15 | No .dockerignore, 63MB binaries in git, YAML indentation bug, no migration transactions |
| Best Practices | 10% | 6/10 | Deprecated APIs, stale comments, hardcoded tenant IDs, predictable temp paths |
| **TOTAL** | **100%** | **58/100** | |

---

## Executive Summary

Airborne is a well-structured multi-tenant LLM gateway with good foundational patterns (chassis-go framework, constant-time token comparison, Lua-based atomic rate limiting, proper gRPC interceptor chains). However, **the admin HTTP server has zero authentication** — any network-reachable client can read all conversation history, trigger AI API calls, and upload files. Combined with API keys exposed in URL query parameters, `http.DefaultClient` with no timeouts, and XSS via `dangerouslySetInnerHTML`, there are significant security gaps that must be addressed before any production deployment.

The migration system has a **broken migration (006)** referencing non-existent tables, and the YAML config has an **indentation bug** that silently breaks RAG configuration. These are reliability concerns that could cause outages.

---

## CRITICAL Findings (8)

### C1. Admin HTTP Server Has No Authentication
- **File:** `internal/admin/server.go:88-146`
- **Severity:** CRITICAL | **Category:** Security
- **Description:** The admin HTTP server registers endpoints (`/admin/activity`, `/admin/debug/`, `/admin/thread/`, `/admin/test`, `/admin/chat`, `/admin/upload`) with zero authentication middleware. Any network-reachable client can read conversation history, debug data (including raw AI request/response JSON), trigger AI API calls (at the operator's cost), and upload files. The gRPC server has proper auth interceptors, but the admin HTTP server is completely open.

```diff
--- a/internal/admin/server.go
+++ b/internal/admin/server.go
@@ -86,6 +86,20 @@

 	mux := http.NewServeMux()

+	// Auth middleware - require Bearer token for all admin endpoints except health
+	authMiddleware := func(next http.Handler) http.Handler {
+		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
+			if r.URL.Path == "/admin/health" || r.URL.Path == "/admin/healthz" || r.URL.Path == "/admin/version" {
+				next.ServeHTTP(w, r)
+				return
+			}
+			token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
+			if token == "" || subtle.ConstantTimeCompare([]byte(token), []byte(s.authToken)) != 1 {
+				http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
+				return
+			}
+			next.ServeHTTP(w, r)
+		})
+	}
+
 	// Register routes...
```

---

### C2. API Keys Exposed in URL Query Parameters
- **File:** `internal/provider/gemini/filestore.go:122,202,282,317,337,454,465,570,619,661,716`
- **Severity:** CRITICAL | **Category:** Security — Credential exposure
- **Description:** Throughout `filestore.go`, the Gemini API key is appended to URLs as `?key=<API_KEY>`. This means the key appears in HTTP access logs, proxy logs, browser history, referrer headers, and network monitoring. API keys should be sent via the `x-goog-api-key` header instead.

```diff
--- a/internal/provider/gemini/filestore.go
+++ b/internal/provider/gemini/filestore.go
@@ -119,7 +119,7 @@
 func uploadToFilesAPI(ctx context.Context, apiKey string, filename string, mimeType string, content []byte) (string, error) {
-	uploadURL := fmt.Sprintf("https://generativelanguage.googleapis.com/upload/v1beta/files?key=%s", apiKey)
+	uploadURL := "https://generativelanguage.googleapis.com/upload/v1beta/files"

 	// ... later when creating the request:
 	initReq, err := http.NewRequestWithContext(ctx, http.MethodPost, uploadURL, bytes.NewReader(metadataJSON))
+	initReq.Header.Set("x-goog-api-key", apiKey)
 	initReq.Header.Set("Content-Type", "application/json")
```

Apply the same pattern at all 11 locations where `?key=%s` appears.

---

### C3. `http.DefaultClient` Used Without Timeouts
- **File:** `internal/provider/gemini/filestore.go:147,173,226,289,337,488,503,582,631,668,726`
- **Severity:** CRITICAL | **Category:** Security/Reliability — DoS vector
- **Description:** All HTTP calls in `filestore.go` use `http.DefaultClient`, which has no timeout. A slow or unresponsive Gemini API could hang goroutines indefinitely, exhausting server resources. The rest of the codebase correctly uses `call.Client` with timeouts.

```diff
--- a/internal/provider/gemini/filestore.go
+++ b/internal/provider/gemini/filestore.go
@@ -16,6 +16,12 @@
+// fileStoreHTTPClient is a shared HTTP client with timeouts for FileSearchStore operations.
+var fileStoreHTTPClient = &http.Client{
+	Timeout: 120 * time.Second,
+}
+
 const (
 	fileSearchBaseURL = "https://generativelanguage.googleapis.com/v1beta"
```

Then replace every `http.DefaultClient.Do(...)` with `fileStoreHTTPClient.Do(...)`.

---

### C4. Encryption Keys Stored as Plaintext in Database
- **File:** `migrations/008_solstice_jobs_tables.sql:27,67,107`
- **Severity:** CRITICAL | **Category:** Security
- **Description:** The `encryption_key` column in all `*_airborne_jobs` tables stores AES encryption keys as plaintext `TEXT`. Anyone with database read access (SQL injection, backup tapes, DB admin, Supabase dashboard) can read these keys and decrypt the corresponding R2 data, completely defeating the crypto-shredding design.

```diff
--- a/migrations/008_solstice_jobs_tables.sql
+++ b/migrations/008_solstice_jobs_tables.sql
@@ -25,7 +25,7 @@
     r2_prefix           TEXT,
-    encryption_key      TEXT,                       -- AES key (deleting = crypto-shred)
+    encryption_key_encrypted BYTEA,                 -- AES key, encrypted with master KEK (never store plaintext)
     origin_host         TEXT,
```

---

### C5. Migration 006 References Non-Existent Tables
- **File:** `migrations/006_add_grounding_costs.sql:7-16`
- **Severity:** CRITICAL | **Category:** Bug
- **Description:** Migration 006 runs `ALTER TABLE messages ...` and `ALTER TABLE activity ...`. These tables do not exist — the actual tables are tenant-prefixed (`ai8_airborne_messages`, etc.) from migration 004 onward. This migration will **fail** with "relation does not exist" when applied.

```diff
--- a/migrations/006_add_grounding_costs.sql
+++ b/migrations/006_add_grounding_costs.sql
@@ -4,15 +4,21 @@

--- Add to messages table (per-message tracking)
-ALTER TABLE messages ADD COLUMN IF NOT EXISTS grounding_queries INTEGER;
-ALTER TABLE messages ADD COLUMN IF NOT EXISTS grounding_cost_usd DOUBLE PRECISION;
+-- Add to tenant messages tables (per-message tracking)
+ALTER TABLE ai8_airborne_messages ADD COLUMN IF NOT EXISTS grounding_queries INTEGER;
+ALTER TABLE ai8_airborne_messages ADD COLUMN IF NOT EXISTS grounding_cost_usd DOUBLE PRECISION;
+ALTER TABLE email4ai_airborne_messages ADD COLUMN IF NOT EXISTS grounding_queries INTEGER;
+ALTER TABLE email4ai_airborne_messages ADD COLUMN IF NOT EXISTS grounding_cost_usd DOUBLE PRECISION;
+ALTER TABLE zztest_airborne_messages ADD COLUMN IF NOT EXISTS grounding_queries INTEGER;
+ALTER TABLE zztest_airborne_messages ADD COLUMN IF NOT EXISTS grounding_cost_usd DOUBLE PRECISION;

--- Add to activity table
-ALTER TABLE activity ADD COLUMN IF NOT EXISTS grounding_queries INTEGER DEFAULT 0;
-ALTER TABLE activity ADD COLUMN IF NOT EXISTS grounding_cost_usd DOUBLE PRECISION DEFAULT 0;
-
--- Create index
-CREATE INDEX IF NOT EXISTS idx_messages_grounding_cost ON messages(grounding_cost_usd) WHERE grounding_cost_usd IS NOT NULL;
-CREATE INDEX IF NOT EXISTS idx_activity_grounding_cost ON activity(grounding_cost_usd) WHERE grounding_cost_usd > 0;
+-- Create indexes
+CREATE INDEX IF NOT EXISTS idx_ai8_messages_grounding_cost
+    ON ai8_airborne_messages(grounding_cost_usd) WHERE grounding_cost_usd IS NOT NULL;
+CREATE INDEX IF NOT EXISTS idx_email4ai_messages_grounding_cost
+    ON email4ai_airborne_messages(grounding_cost_usd) WHERE grounding_cost_usd IS NOT NULL;
+CREATE INDEX IF NOT EXISTS idx_zztest_messages_grounding_cost
+    ON zztest_airborne_messages(grounding_cost_usd) WHERE grounding_cost_usd IS NOT NULL;
```

---

### C6. Dashboard XSS via `dangerouslySetInnerHTML`
- **File:** `dashboard/src/components/ConversationPanel.tsx:447-449`
- **Severity:** CRITICAL | **Category:** Security — XSS
- **Description:** The `renderContent()` function uses `dangerouslySetInnerHTML={{ __html: renderedHtml }}` with HTML returned from the backend markdown rendering service. If the backend does not sanitize, or if an attacker can inject content into a conversation, arbitrary JavaScript will execute in the admin dashboard.

```diff
--- a/dashboard/src/components/ConversationPanel.tsx
+++ b/dashboard/src/components/ConversationPanel.tsx
@@ -1,5 +1,6 @@
 "use client";

+import DOMPurify from "dompurify";
 import { useState, useEffect, useRef, useCallback, Component, ReactNode } from "react";
@@ -446,7 +447,7 @@
     if (viewMode === "formatted" && renderedHtml) {
       return (
         <div
           className="prose prose-sm max-w-none dark:prose-invert"
-          dangerouslySetInnerHTML={{ __html: renderedHtml }}
+          dangerouslySetInnerHTML={{ __html: DOMPurify.sanitize(renderedHtml) }}
         />
       );
     }
```

Also run: `cd dashboard && npm install dompurify @types/dompurify`

---

### C7. SSRF via Unsanitized Route Parameters in Dashboard API
- **File:** `dashboard/src/app/api/threads/[threadId]/route.ts:16`, `dashboard/src/app/api/debug/[id]/route.ts:16`
- **Severity:** CRITICAL | **Category:** Security — SSRF/Path Traversal
- **Description:** Route parameters (`threadId`, `id`) are interpolated directly into URLs fetched server-side: `` `${AIRBORNE_ADMIN_URL}/admin/thread/${threadId}` ``. A crafted threadId like `../../secret-endpoint` could cause the server to make requests to unintended paths.

```diff
--- a/dashboard/src/app/api/threads/[threadId]/route.ts
+++ b/dashboard/src/app/api/threads/[threadId]/route.ts
@@ -9,6 +9,11 @@
   const { threadId } = await params;

   if (!threadId) {
     return NextResponse.json({ error: "thread_id required" }, { status: 400 });
   }
+
+  // Validate threadId is a UUID to prevent path traversal
+  if (!/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i.test(threadId)) {
+    return NextResponse.json({ error: "invalid thread_id format" }, { status: 400 });
+  }
```

Apply same pattern to `debug/[id]/route.ts`.

---

### C8. Unsanitized `limit` Query Parameter Injected into Backend URL
- **File:** `dashboard/src/app/api/activity/route.ts:11`
- **Severity:** CRITICAL | **Category:** Security — Injection
- **Description:** The `limit` parameter from the query string is directly interpolated into the backend URL without validation: `` `${AIRBORNE_ADMIN_URL}/admin/activity?limit=${limit}` ``. A malicious value could inject additional query parameters.

```diff
--- a/dashboard/src/app/api/activity/route.ts
+++ b/dashboard/src/app/api/activity/route.ts
@@ -6,7 +6,12 @@
 export async function GET(request: NextRequest) {
   const searchParams = request.nextUrl.searchParams;
-  const limit = searchParams.get("limit") || "50";
+  const rawLimit = searchParams.get("limit") || "50";
+  const limit = parseInt(rawLimit, 10);
+  if (isNaN(limit) || limit < 1 || limit > 1000) {
+    return NextResponse.json({ error: "invalid limit parameter" }, { status: 400 });
+  }
   const tenantId = searchParams.get("tenant_id");
```

---

## HIGH Findings (10)

### H1. Race Condition in `getGRPCClient` Lazy Initialization
- **File:** `internal/admin/server.go:449-468`
- **Severity:** HIGH | **Category:** Bug — Data race
- **Description:** `getGRPCClient()` checks `s.grpcClient != nil` and sets fields without synchronization. Concurrent HTTP requests could race, creating multiple leaked connections.

```diff
--- a/internal/admin/server.go
+++ b/internal/admin/server.go
@@ -36,6 +36,7 @@
 type Server struct {
+	grpcMu      sync.Mutex
 	grpcConn    *grpc.ClientConn
 	grpcClient  pb.AirborneServiceClient
@@ -448,6 +449,8 @@
 func (s *Server) getGRPCClient() (pb.AirborneServiceClient, error) {
+	s.grpcMu.Lock()
+	defer s.grpcMu.Unlock()
 	if s.grpcClient != nil {
 		return s.grpcClient, nil
 	}
```

---

### H2. `os.ExpandEnv` Fallback Can Leak Sensitive Environment Variables
- **File:** `internal/config/config.go:357-372`
- **Severity:** HIGH | **Category:** Security — Information leak
- **Description:** The `expandEnv` function falls through to `os.ExpandEnv(s)` for any string that doesn't match `ENV=` or `${VAR}` patterns. If a config value accidentally contains `$AWS_SECRET_ACCESS_KEY` or similar, it will be silently expanded.

```diff
--- a/internal/config/config.go
+++ b/internal/config/config.go
@@ -368,5 +368,8 @@
 		return os.Getenv(varName)
 	}

-	// Handle $VAR syntax and passthrough non-variable strings
-	return os.ExpandEnv(s)
+	// Do NOT blindly call os.ExpandEnv - it could leak arbitrary env vars.
+	if strings.Contains(s, "$") {
+		slog.Warn("config value contains $ but doesn't match known patterns, returning as-is", "value_prefix", s[:min(len(s), 20)])
+	}
+	return s
 }
```

---

### H3. `httpcapture.Transport` Reads Entire Response Body Into Memory
- **File:** `internal/httpcapture/transport.go:74-87`
- **Severity:** HIGH | **Category:** Bug — Unbounded memory allocation
- **Description:** Reads entire response body with `io.ReadAll(resp.Body)` with no size limit. A malicious provider API could return a huge response, causing OOM.

```diff
--- a/internal/httpcapture/transport.go
+++ b/internal/httpcapture/transport.go
@@ -71,8 +71,10 @@
+	const maxCaptureSize = 10 * 1024 * 1024 // 10 MB limit
+
 	if resp.Body != nil {
-		body, err := io.ReadAll(resp.Body)
+		limitedReader := io.LimitReader(resp.Body, maxCaptureSize)
+		body, err := io.ReadAll(limitedReader)
 		if err != nil {
```

---

### H4. TLS Disabled by Default in Configuration
- **File:** `configs/airborne.yaml:8-11`
- **Severity:** HIGH | **Category:** Security
- **Description:** TLS is disabled by default (`enabled: false`) with empty cert/key paths. The gRPC server will accept plaintext connections carrying API keys to LLM providers.

```diff
--- a/configs/airborne.yaml
+++ b/configs/airborne.yaml
@@ -6,9 +6,9 @@
 tls:
-  enabled: false
-  cert_file: ""
-  key_file: ""
+  enabled: ${TLS_ENABLED:-false}
+  cert_file: "${TLS_CERT_FILE}"
+  key_file: "${TLS_KEY_FILE}"
```

---

### H5. CI Workflow Uses GITHUB_TOKEN for Cross-Repo Checkout
- **File:** `.github/workflows/docker-build.yml:29`
- **Severity:** HIGH | **Category:** Security/CI
- **Description:** The default `GITHUB_TOKEN` cannot access other private repositories. The `pricing_db` checkout step will fail silently or error.

```diff
--- a/.github/workflows/docker-build.yml
+++ b/.github/workflows/docker-build.yml
@@ -26,7 +26,7 @@
         with:
           repository: ai8future/pricing_db
           path: pricing_db
-          token: ${{ secrets.GITHUB_TOKEN }}
+          token: ${{ secrets.PRICING_DB_TOKEN }}
```

---

### H6. YAML Indentation Error on `rag:` Key
- **File:** `configs/airborne.yaml:72`
- **Severity:** HIGH | **Category:** Bug
- **Description:** The `rag:` key has a leading space (` rag:` instead of `rag:`), making it a child of `startup_mode` instead of a top-level key. This silently breaks all RAG configuration.

```diff
--- a/configs/airborne.yaml
+++ b/configs/airborne.yaml
@@ -69,7 +69,7 @@

 # RAG settings
- rag:
+rag:
   enabled: false
```

---

### H7. System Prompt Exposed in Client Bundle
- **File:** `dashboard/src/components/ConversationPanel.tsx:645`
- **Severity:** HIGH | **Category:** Security — Information disclosure
- **Description:** A default system prompt string is hardcoded in the frontend source code. This is shipped to the browser and visible to any user inspecting the JavaScript bundle.

---

### H8. No File Size/Type Validation on Upload
- **File:** `dashboard/src/app/api/upload/route.ts:12-23`
- **Severity:** HIGH | **Category:** Security — Unrestricted upload
- **Description:** The upload endpoint forwards files to the backend without checking file size or MIME type. An attacker could upload arbitrarily large files.

```diff
--- a/dashboard/src/app/api/upload/route.ts
+++ b/dashboard/src/app/api/upload/route.ts
@@ -14,6 +14,17 @@
     const file = formData.get("file") as File | null;
+    const MAX_FILE_SIZE = 50 * 1024 * 1024; // 50MB
+    const ALLOWED_TYPES = [
+      "application/pdf", "text/plain", "text/csv", "text/markdown",
+      "image/png", "image/jpeg", "image/gif", "image/webp",
+    ];

     if (!file) {
       return NextResponse.json({ error: "file is required" }, { status: 400 });
     }
+
+    if (file.size > MAX_FILE_SIZE) {
+      return NextResponse.json({ error: "file too large (max 50MB)" }, { status: 413 });
+    }
+    if (!ALLOWED_TYPES.includes(file.type)) {
+      return NextResponse.json({ error: "unsupported file type" }, { status: 415 });
+    }
```

---

### H9. No CSRF Protection on Dashboard POST Routes
- **File:** `dashboard/src/app/api/chat/route.ts`, `dashboard/src/app/api/upload/route.ts`
- **Severity:** HIGH | **Category:** Security — CSRF
- **Description:** Dashboard POST API routes have no CSRF tokens or origin validation. A malicious page could trigger API calls from an authenticated browser.

---

### H10. 63 MB of Compiled Binaries Committed to Git
- **File:** Root-level `airborne` (44MB), `airborne-cli` (9MB), `airborne-freeze` (9MB)
- **Severity:** HIGH | **Category:** Practice
- **Description:** Platform-specific compiled binaries bloat the git history permanently. `.gitignore` has `bin/` but these are in the project root.

```diff
--- a/.gitignore
+++ b/.gitignore
@@ -27,6 +27,11 @@
 .gocache/
 bin/
+
+# Compiled binaries (root level)
+/airborne
+/airborne-cli
+/airborne-freeze
 *.exe~
```

Then: `git rm --cached airborne airborne-cli airborne-freeze`

---

## MEDIUM Findings (18)

### M1. Dockerfile EXPOSE Port Mismatch
- **File:** `Dockerfile:55`
- **Description:** EXPOSE 50051 but actual gRPC port is 50612 per configs.

```diff
-EXPOSE 50051
+EXPOSE 50612
```

### M2. Missing .dockerignore (Main Project)
- **File:** `.dockerignore` (missing)
- **Description:** No .dockerignore; `COPY . .` copies .git (100+MB), compiled binaries (63MB), coverage files, and potentially secrets into Docker context.

### M3. Missing .dockerignore (Dashboard)
- **File:** `dashboard/.dockerignore` (missing)
- **Description:** Dashboard build copies `node_modules/` and `.next/` into context.

### M4. Docker Build Caching Disabled
- **File:** `.github/workflows/docker-build.yml:69`
- **Description:** `no-cache: true` forces full rebuilds every CI run.

```diff
-          no-cache: true
+          cache-from: type=gha
+          cache-to: type=gha,mode=max
```

### M5. docker-compose.yml Missing Resource Limits
- **File:** `docker-compose.yml`
- **Description:** No CPU/memory limits on the airborne container.

### M6. Migrations Lack Transactional Wrapping
- **File:** All 9 migration files
- **Description:** None use `BEGIN; ... COMMIT;`. Partial failures leave database in inconsistent state.

### M7. Migrations Have No Down/Rollback Scripts
- **File:** `migrations/` (all)
- **Description:** Rollback SQL is only in comments. No automated rollback mechanism.

### M8. Stale `useEffect` Dependency (eslint-disable)
- **File:** `dashboard/src/components/ConversationPanel.tsx:280-284`
- **Description:** `useEffect` has eslint-disable for missing deps — `dataFetched` stale closure risk.

### M9. Unmemoized `threadList` Causes Unnecessary Re-renders
- **File:** `dashboard/src/components/ConversationPanel.tsx:742-746`
- **Description:** `threadList` is recomputed every render with new object identity, triggering useEffect.

### M10. `formatTokens(0)` Returns "-" Instead of "0"
- **File:** `dashboard/src/components/ActivityPanel.tsx:45-46`
- **Description:** `if (!n) return "-"` treats `0` as falsy. Zero is a valid token count.

```diff
-    if (!n) return "-";
+    if (n === undefined || n === null) return "-";
```

### M11. Polling Restarts on Pause Toggle
- **File:** `dashboard/src/app/page.tsx:55-68`
- **Description:** `paused` in useEffect dependency array causes interval teardown + immediate fetch on pause toggle.

### M12. Duplicate `ActivityEntry` Interface (3 files)
- **File:** `page.tsx`, `ActivityPanel.tsx`, `ConversationPanel.tsx`
- **Description:** Same interface defined 3 times with slight field differences.

### M13. `Math.random()` UUID vs `crypto.randomUUID()`
- **File:** `dashboard/src/components/ConversationPanel.tsx:612-618`
- **Description:** Thread UUID uses `Math.random()` while request ID uses `crypto.randomUUID()`.

```diff
-function generateUUID(): string {
-  return 'xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx'.replace(/[xy]/g, function(c) {
-    const r = Math.random() * 16 | 0;
-    const v = c === 'x' ? r : (r & 0x3 | 0x8);
-    return v.toString(16);
-  });
-}
+function generateUUID(): string {
+  return crypto.randomUUID();
+}
```

### M14. Backend Errors Masked as HTTP 200
- **File:** `dashboard/src/app/api/activity/route.ts`, `threads/[threadId]/route.ts`
- **Description:** Backend failures return 200 with error in JSON body, making monitoring useless.

### M15. Admin Error Messages Leak Internal Details
- **File:** `internal/admin/server.go:214-218,344-353,411-422`
- **Description:** Raw `err.Error()` returned to clients can contain DB connection strings, table names, SQL errors.

### M16. CLI Client Doesn't URL-encode `tenant_id`
- **File:** `internal/cli/client.go:131-132`
- **Description:** `tenantID` concatenated directly into URL without encoding.

### M17. go.mod Contains Relative Path Replace Directive
- **File:** `go.mod:27`
- **Description:** `replace github.com/ai8future/pricing_db => ../pricing_db` breaks builds for anyone without sibling directory.

### M18. CI Actions Referenced by Tag, Not SHA
- **File:** `.github/workflows/docker-build.yml:21,25,41,44,50,62`
- **Description:** GitHub Actions pinned to tags (e.g., `@v4`) are vulnerable to supply-chain attacks if tags are moved.

---

## LOW Findings (15)

| # | File | Description |
|---|------|-------------|
| L1 | `Dockerfile:55` + `configs/` | EXPOSE port mismatch is misleading documentation |
| L2 | `configs/frozen.json` | World-readable permissions unlike other config files |
| L3 | `configs/frozen.json:14` | Redis password empty in frozen config snapshot |
| L4 | `.gitignore` + tracked files | `coverage.out` tracked despite `*.out` in .gitignore |
| L5 | `migrations/004:8` | Comment header references wrong filename (003 vs 004) |
| L6 | `migrations/005,008,009` | No CHECK constraints on status columns |
| L7 | `deployments/docker/`, `systemd/` | Empty placeholder directories |
| L8 | `dashboard/.gitignore:9` | Only ignores `.env*.local`, not plain `.env` |
| L9 | `dashboard/next.config.ts` | No CSP or security headers configured |
| L10 | `dashboard/src/components/DebugModal.tsx` | 486-line component never imported (dead code) |
| L11 | `dashboard/src/components/TestPanel.tsx` | Component never imported (dead code) |
| L12 | `dashboard/src/components/DebugModal.tsx:86-89` | `new URL()` can throw on malformed URIs |
| L13 | `internal/cli/commands.go:196-261` | Watch command `seen` map grows without bound |
| L14 | `internal/config/config.go:193` | Frozen config skips validation |
| L15 | `cmd/airborne/main.go:209` | Deprecated `grpc.DialContext` usage |

---

## Positive Observations

The codebase demonstrates several strong patterns worth noting:

1. **Constant-time token comparison** (`subtle.ConstantTimeCompare`) in `auth/static.go:72` — prevents timing attacks
2. **Atomic Lua scripts** for Redis rate limiting — prevents race conditions in rate limit checks
3. **Proper gRPC interceptor chain** — Auth, tenant, rate-limit interceptors are well-structured
4. **chassis-go framework adoption** — `call.Client` with retries and timeouts used consistently (except in filestore.go)
5. **Input validation module** (`internal/validation/`) — URL validation, file type checks exist as a framework
6. **Idempotent request handling** — `request_id` support for retry safety in chat endpoints
7. **Good test infrastructure** — `miniredis` for Redis testing, structured test files present
8. **Multi-tenant architecture** — Per-tenant table isolation (not just row-level filtering)

---

## Recommended Priority

1. **Immediate (P0):** C1 (Admin auth), C2 (API keys in URLs), C3 (HTTP timeouts), C6 (XSS)
2. **This sprint (P1):** C4 (Encryption keys), C5 (Broken migration), H1-H3 (Race condition, env leak, memory)
3. **Next sprint (P2):** All remaining HIGH and MEDIUM issues
4. **Backlog (P3):** LOW issues

---

*Report generated by Claude Code (Claude:Opus 4.6) on 2026-02-15T22:39:30Z*
*Total findings: 8 CRITICAL, 10 HIGH, 18 MEDIUM, 15 LOW = 51 issues*
