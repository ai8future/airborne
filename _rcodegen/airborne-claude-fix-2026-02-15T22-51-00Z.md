Date Created: 2026-02-15T22:51:00Z
TOTAL_SCORE: 82/100

# Airborne Codebase Audit Report

**Agent:** Claude Code:Opus 4.6
**Project:** Airborne (Go gRPC LLM Gateway + Next.js Dashboard)
**Version:** 1.8.3
**Files Analyzed:** ~140 Go source files, ~17 TypeScript/React files, 4 proto files, Dockerfile, docker-compose.yml, Makefile, CI/CD workflow

---

## Executive Summary

Airborne is a well-architected Go gRPC microservice with strong foundations: proper error handling, timing-safe auth comparisons, structured logging, graceful shutdown via lifecycle management, and good test coverage (~46 test files). The codebase follows Go conventions consistently and demonstrates security-aware design (API keys restricted to server-side, SSRF validation, error sanitization).

However, the audit identified **12 issues** across security, correctness, configuration, and code quality categories. No critical showstoppers that would cause immediate production failures, but several medium-high severity items warrant attention.

**Score Breakdown:**
- Architecture & Design: 18/20 (clean layered architecture, good abstractions)
- Security: 16/20 (constant-time auth, SSRF protection, but XSS risk in dashboard, incomplete error sanitization)
- Correctness: 16/20 (generally solid, but rate limit logic flaw, missing LastUsed tracking)
- Configuration & Deployment: 14/20 (Dockerfile port mismatch, timeout concerns)
- Code Quality & Maintainability: 18/20 (consistent conventions, good test coverage, clean code)

---

## Issues Found

### ISSUE 1: Dockerfile EXPOSE Port Mismatch (Medium - Configuration Bug)

**File:** `Dockerfile:55`
**Also affects:** `docker-compose.yml:10`

The Dockerfile exposes port 50051, but the actual configured gRPC port in `configs/airborne.yaml` is 50612. The docker-compose.yml correctly maps 50612:50612.

**Impact:** The `EXPOSE` directive is documentation/metadata only (doesn't affect functionality), but it misleads operators and breaks tools that rely on exposed port metadata (e.g., Kubernetes service discovery, Docker networking).

```diff
--- a/Dockerfile
+++ b/Dockerfile
@@ -52,7 +52,7 @@ USER airborne

 # Expose gRPC port
-EXPOSE 50051
+EXPOSE 50612

 # Health check
```

---

### ISSUE 2: XSS Vulnerability via dangerouslySetInnerHTML (High - Security)

**File:** `dashboard/src/components/ConversationPanel.tsx:449-450`

The component renders server-provided HTML using `dangerouslySetInnerHTML` without client-side sanitization. If the markdown rendering service is compromised or returns malicious content, this creates an XSS vector.

**Current code:**
```tsx
<div
  className="text-sm leading-relaxed prose..."
  dangerouslySetInnerHTML={{ __html: renderedHtml }}
/>
```

**Recommended fix:** Add DOMPurify sanitization:

```diff
--- a/dashboard/src/components/ConversationPanel.tsx
+++ b/dashboard/src/components/ConversationPanel.tsx
+import DOMPurify from 'dompurify';

  <div
    className="text-sm leading-relaxed prose..."
-   dangerouslySetInnerHTML={{ __html: renderedHtml }}
+   dangerouslySetInnerHTML={{ __html: DOMPurify.sanitize(renderedHtml) }}
  />
```

---

### ISSUE 3: Rate Limit Token Recording Logic Flaw (Medium - Logic Bug)

**File:** `internal/auth/ratelimit.go:96-142`

The `RecordTokens` function always increments the token counter via Lua script, then checks if the limit is exceeded *after* recording. It returns `ErrRateLimitExceeded` but the tokens are already recorded. This makes the rate limit informational-only (post-hoc) rather than preventive.

The function comment says "return error but don't block - already processed" which suggests this is by design (tokens consumed before checking), but the return of `ErrRateLimitExceeded` is misleading to callers who may think the operation was blocked.

**Recommended fix:** Document the post-hoc nature clearly, or change to a warning log:

```diff
--- a/internal/auth/ratelimit.go
+++ b/internal/auth/ratelimit.go
-    // Check if over limit (return error but don't block - already processed)
+    // Informational: warn if over limit. Tokens already consumed so this
+    // does not block the current request. Pre-request checks happen in Allow().
     if int(count) > limit {
-        return ErrRateLimitExceeded
+        slog.Warn("token usage exceeds TPM limit (post-hoc)",
+            "client_id", clientID,
+            "current", count,
+            "limit", limit,
+        )
     }

     return nil
```

Note: This requires adding `"log/slog"` to the import block.

---

### ISSUE 4: Missing LastUsed Timestamp Update in Key Validation (Low - Feature Gap)

**File:** `internal/auth/keys.go:125-149`

The `ClientKey` struct has a `LastUsed *time.Time` field, but `ValidateKey` never updates it after successful authentication. This makes key activity auditing impossible.

```diff
--- a/internal/auth/keys.go
+++ b/internal/auth/keys.go
     // Verify secret
     if err := bcrypt.CompareHashAndPassword([]byte(key.SecretHash), []byte(secret)); err != nil {
         return nil, ErrInvalidKey
     }

+    // Update last used timestamp (best-effort, don't block auth on failure)
+    now := time.Now().UTC()
+    key.LastUsed = &now
+    if saveErr := s.saveKey(ctx, key); saveErr != nil {
+        slog.Warn("failed to update last_used timestamp",
+            "key_id", key.KeyID,
+            "error", saveErr,
+        )
+    }
+
     return key, nil
```

---

### ISSUE 5: Anthropic Stream Accumulation Error Not Handled (Medium - Correctness)

**File:** `internal/provider/anthropic/client.go:373-377`

When `message.Accumulate(event)` fails during streaming, the error is logged but processing continues with potentially corrupted message state. The `switch` on `event.AsAny().(type)` proceeds with the event regardless, which could produce incomplete or malformed responses.

```diff
--- a/internal/provider/anthropic/client.go
+++ b/internal/provider/anthropic/client.go
     if err := message.Accumulate(event); err != nil {
         slog.Warn("failed to accumulate stream event", "error", err)
+        continue
     }

     switch eventVariant := event.AsAny().(type) {
```

---

### ISSUE 6: SQL Table Names Constructed via String Interpolation (Low - Defensive Security)

**File:** `internal/db/repository.go` (throughout, e.g., lines 98-101, 169-176)

Table names are constructed using `fmt.Sprintf` with tenant IDs. While the tenant ID is validated against a whitelist (`ValidTenantIDs`), the pattern is fragile. If the validation is weakened or bypassed in the future, this becomes an SQL injection vector.

```go
query := fmt.Sprintf(`
    INSERT INTO %s (id, user_id, ...)
    VALUES ($1, $2, ...)
`, r.threadsTable())
```

**Recommended fix:** Add PostgreSQL identifier quoting as defense-in-depth:

```diff
--- a/internal/db/repository.go
+++ b/internal/db/repository.go
+// quoteIdent quotes a PostgreSQL identifier to prevent injection
+func quoteIdent(s string) string {
+    return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
+}
+
 func (r *Repository) threadsTable() string {
     if r.tablePrefix == "" {
-        return "airborne_threads"
+        return quoteIdent("airborne_threads")
     }
-    return r.tablePrefix + "_threads"
+    return quoteIdent(r.tablePrefix + "_threads")
 }
```

---

### ISSUE 7: Deprecated `grpc.DialContext` Usage (Low - API Deprecation)

**File:** `cmd/airborne/main.go:209`

`grpc.DialContext` is deprecated in newer gRPC versions. It should be replaced with `grpc.NewClient`.

```diff
--- a/cmd/airborne/main.go
+++ b/cmd/airborne/main.go
-    conn, err := grpc.DialContext(ctx, addr, dialOpts...)
+    conn, err := grpc.NewClient(addr, dialOpts...)
```

---

### ISSUE 8: Upload Timeout May Be Insufficient for Large Files (Low - Configuration)

**File:** `internal/admin/server.go:927,972-973`

The upload handler allows files up to 100MB (`r.ParseMultipartForm(100 << 20)`) but sets only a 2-minute timeout for the entire upload + Gemini processing cycle. On slower connections, this is insufficient.

```diff
--- a/internal/admin/server.go
+++ b/internal/admin/server.go
-    ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
+    ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
```

---

### ISSUE 9: Dashboard Fetch Polling Without In-Flight Guard (Low - UX Bug)

**File:** `dashboard/src/app/page.tsx:55-68`

The 3-second polling interval doesn't check if a previous fetch is still in-flight. Slow responses will cause request pileup.

```diff
--- a/dashboard/src/app/page.tsx
+++ b/dashboard/src/app/page.tsx
+  const fetchingRef = useRef(false);
+
   const fetchActivity = useCallback(async () => {
+    if (fetchingRef.current) return;
+    fetchingRef.current = true;
     try {
       const res = await fetch(`/api/activity?limit=50&tenant_id=${tenant}`);
       // ...
     } finally {
       setLoading(false);
+      fetchingRef.current = false;
     }
   }, [tenant]);
```

---

### ISSUE 10: Dashboard Chat API Fetch Missing Timeout (Low - Reliability)

**File:** `dashboard/src/app/api/chat/route.ts:96-117`

The retry logic in `fetchWithRetry` uses `fetch()` without an `AbortController` timeout. Requests can hang indefinitely.

```diff
--- a/dashboard/src/app/api/chat/route.ts
+++ b/dashboard/src/app/api/chat/route.ts
   for (let attempt = 0; attempt < retries; attempt++) {
     try {
+      const controller = new AbortController();
+      const timeoutId = setTimeout(() => controller.abort(), 30000);
       const response = await fetch(url, {
         ...options,
+        signal: controller.signal,
       });
+      clearTimeout(timeoutId);
```

---

### ISSUE 11: Invalid JSON Truncation in Debug Modal (Low - Display Bug)

**File:** `dashboard/src/components/ConversationPanel.tsx:341-343`

JSON is truncated at an arbitrary character position, which can produce invalid JSON that breaks syntax highlighting or parsing:

```typescript
const displayJson = responseJson && responseJson.length > 50000
  ? responseJson.substring(0, 50000) + "\n\n... [truncated]"
  : responseJson;
```

**Recommended fix:** Display truncated content in a non-JSON block, or limit by display height instead of string truncation.

---

### ISSUE 12: Hardcoded Default Tenant ID in Upload Handler (Low - Code Smell)

**File:** `internal/admin/server.go:950-952`

The upload handler defaults to a specific tenant ("email4ai") when no tenant_id is provided. This leaks business logic into infrastructure code and could cause unexpected behavior if the default tenant is removed.

```go
tenantID := r.FormValue("tenant_id")
if tenantID == "" {
    tenantID = "email4ai" // Default tenant
}
```

**Recommended fix:** Require tenant_id explicitly or use a configurable default.

---

## Positive Observations

1. **Authentication is solid:** Constant-time comparison for static tokens (`crypto/subtle.ConstantTimeCompare`), bcrypt for API key secrets, proper gRPC metadata extraction.

2. **No common anti-patterns:** No `context.TODO()`, no `panic()` in non-test code, no swallowed errors, no SQL injection via string concatenation (parameterized queries used throughout).

3. **Good resource management:** Proper `defer` patterns for cleanup, `resp.Body.Close()` consistently used, database connection pooling via pgx.

4. **Security-aware design:** API keys restricted to server-side config (line 49-53 of builder.go), error sanitization for client responses, SSRF URL validation, secret path validation with symlink resolution.

5. **Modern Go practices:** `log/slog` structured logging, context propagation, gRPC interceptor chains, OpenTelemetry integration via chassis-go.

6. **Good test coverage:** 46 test files covering auth, config, providers, RAG, validation, and services. Tests use miniredis for Redis mocking and httptest for HTTP testing.

7. **Clean architecture:** Provider abstraction with compat layer for OpenAI-compatible APIs reduces code duplication across 13+ LLM providers.

---

## Files Not Reviewed

Per CLAUDE.md guidelines, the following directories were excluded: `_studies/`, `_proposals/`, `_rcodegen/`, `_bugs_open/`, `_bugs_fixed/`. Generated protobuf code in `gen/` was also excluded from quality review.
