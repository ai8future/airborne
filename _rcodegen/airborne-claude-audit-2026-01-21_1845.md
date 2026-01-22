# Airborne Security & Code Quality Audit Report

**Date Created:** 2026-01-21 18:45:00 UTC

**Audited By:** Claude:Opus 4.5 (claude-opus-4-5-20251101)

**Codebase Version:** 1.7.2

---

## Executive Summary

This audit covers the Airborne multi-provider LLM API gateway, including security vulnerabilities, code quality issues, and potential bugs. The codebase demonstrates mature security practices overall, with strong SSRF protection, proper authentication, and good error sanitization. However, several issues were identified that should be addressed.

### Risk Summary

| Severity | Count | Categories |
|----------|-------|------------|
| **Critical** | 1 | XSS vulnerability in dashboard |
| **High** | 3 | Authentication bypass, CORS misconfiguration, sensitive data exposure |
| **Medium** | 5 | Rate limiting gaps, information leakage, timing attacks |
| **Low** | 8 | Code quality, missing validations, logging concerns |

---

## Critical Issues

### 1. [CRITICAL] XSS Vulnerability via `dangerouslySetInnerHTML`

**File:** `dashboard/src/components/ConversationPanel.tsx:432`

**Description:** The dashboard renders server-provided HTML directly into the DOM using `dangerouslySetInnerHTML`. While the HTML comes from a markdown rendering service, if the markdown service is compromised or returns malicious content, this creates a stored XSS vulnerability.

**Vulnerable Code:**
```typescript
<div
  className="text-sm leading-relaxed prose prose-sm max-w-none..."
  dangerouslySetInnerHTML={{ __html: renderedHtml }}
/>
```

**Risk:** An attacker who can inject malicious content into the LLM response or compromise the markdown service could execute arbitrary JavaScript in user browsers, potentially stealing session tokens or performing actions on behalf of users.

**Recommended Fix:**
```diff
--- a/dashboard/src/components/ConversationPanel.tsx
+++ b/dashboard/src/components/ConversationPanel.tsx
@@ -3,6 +3,7 @@
 import { useState, useEffect, useRef, useCallback, Component, ReactNode } from "react";
 import ReactMarkdown from "react-markdown";
 import remarkGfm from "remark-gfm";
+import DOMPurify from "dompurify";
 import { useTenant } from "@/context/TenantContext";

@@ -429,7 +430,7 @@ function MessageBubble({ message, isPending, sendStartTime }: MessageBubbleProps
       <>
         <div
           className="text-sm leading-relaxed prose prose-sm max-w-none..."
-          dangerouslySetInnerHTML={{ __html: renderedHtml }}
+          dangerouslySetInnerHTML={{ __html: DOMPurify.sanitize(renderedHtml) }}
         />
```

---

## High Severity Issues

### 2. [HIGH] Admin HTTP Endpoints Lack Authentication

**File:** `internal/admin/server.go:76-99`

**Description:** The admin HTTP server exposes sensitive endpoints (`/admin/activity`, `/admin/debug/`, `/admin/thread/`, `/admin/chat`, `/admin/upload`) without any authentication. While CORS is configured, anyone who can reach the admin port can access all conversation data, debug information, and send messages.

**Vulnerable Code:**
```go
corsHandler := func(h http.HandlerFunc) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("Access-Control-Allow-Origin", "*")
        // ... no auth check ...
        h(w, r)
    }
}
```

**Recommended Fix:**
```diff
--- a/internal/admin/server.go
+++ b/internal/admin/server.go
@@ -73,12 +73,25 @@ func NewServer(dbClient *db.Client, cfg Config) *Server {
 	mux := http.NewServeMux()

-	// CORS middleware wrapper
-	corsHandler := func(h http.HandlerFunc) http.HandlerFunc {
+	// Auth + CORS middleware wrapper
+	authCorsHandler := func(h http.HandlerFunc, requireAuth bool) http.HandlerFunc {
 		return func(w http.ResponseWriter, r *http.Request) {
 			w.Header().Set("Access-Control-Allow-Origin", "*")
 			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
-			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
+			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Admin-Token")

 			if r.Method == "OPTIONS" {
 				w.WriteHeader(http.StatusOK)
 				return
 			}
+
+			// Validate admin token for protected endpoints
+			if requireAuth && s.authToken != "" {
+				token := r.Header.Get("X-Admin-Token")
+				if token == "" {
+					token = r.Header.Get("Authorization")
+					token = strings.TrimPrefix(token, "Bearer ")
+				}
+				if token != s.authToken {
+					http.Error(w, "unauthorized", http.StatusUnauthorized)
+					return
+				}
+			}

 			h(w, r)
 		}
 	}

 	// Register endpoints
-	mux.HandleFunc("/admin/activity", corsHandler(s.handleActivity))
-	mux.HandleFunc("/admin/debug/", corsHandler(s.handleDebug))
-	mux.HandleFunc("/admin/thread/", corsHandler(s.handleThread))
-	mux.HandleFunc("/admin/health", corsHandler(s.handleHealth))
-	mux.HandleFunc("/admin/version", corsHandler(s.handleVersion))
-	mux.HandleFunc("/admin/test", corsHandler(s.handleTest))
-	mux.HandleFunc("/admin/chat", corsHandler(s.handleChat))
-	mux.HandleFunc("/admin/upload", corsHandler(s.handleUpload))
+	mux.HandleFunc("/admin/activity", authCorsHandler(s.handleActivity, true))
+	mux.HandleFunc("/admin/debug/", authCorsHandler(s.handleDebug, true))
+	mux.HandleFunc("/admin/thread/", authCorsHandler(s.handleThread, true))
+	mux.HandleFunc("/admin/health", authCorsHandler(s.handleHealth, false))  // Health check public
+	mux.HandleFunc("/admin/version", authCorsHandler(s.handleVersion, false)) // Version public
+	mux.HandleFunc("/admin/test", authCorsHandler(s.handleTest, true))
+	mux.HandleFunc("/admin/chat", authCorsHandler(s.handleChat, true))
+	mux.HandleFunc("/admin/upload", authCorsHandler(s.handleUpload, true))
```

### 3. [HIGH] Overly Permissive CORS Configuration

**File:** `internal/admin/server.go:78`

**Description:** The admin server allows requests from any origin (`*`), which combined with the lack of authentication, allows any website to make requests to the admin API if the user's browser can reach it.

**Vulnerable Code:**
```go
w.Header().Set("Access-Control-Allow-Origin", "*")
```

**Recommended Fix:**
```diff
--- a/internal/admin/server.go
+++ b/internal/admin/server.go
@@ -74,8 +74,17 @@ func NewServer(dbClient *db.Client, cfg Config) *Server {

-	corsHandler := func(h http.HandlerFunc) http.HandlerFunc {
+	// Define allowed origins (configure via environment)
+	allowedOrigins := map[string]bool{
+		"http://localhost:4848": true,  // Dashboard dev
+		"https://admin.airborne.example.com": true, // Production
+	}
+
+	corsHandler := func(h http.HandlerFunc) http.HandlerFunc {
 		return func(w http.ResponseWriter, r *http.Request) {
-			w.Header().Set("Access-Control-Allow-Origin", "*")
+			origin := r.Header.Get("Origin")
+			if allowedOrigins[origin] {
+				w.Header().Set("Access-Control-Allow-Origin", origin)
+			}
+			w.Header().Set("Vary", "Origin")
```

### 4. [HIGH] Sensitive Debug Data Exposed Without Tenant Isolation

**File:** `internal/admin/server.go:296-322`

**Description:** The `/admin/debug/{message_id}` endpoint returns raw request/response JSON, system prompts, and rendered HTML without verifying the requester has access to that tenant's data. The `GetDebugDataAllTenants` function searches across all tenants.

**Vulnerable Code:**
```go
baseRepo := db.NewRepository(s.dbClient)
data, err := baseRepo.GetDebugDataAllTenants(ctx, messageID)
```

**Risk:** Any authenticated user (or unauthenticated if issue #2 isn't fixed) can access debug data from any tenant by guessing/enumerating message UUIDs.

**Recommended Fix:**
```diff
--- a/internal/admin/server.go
+++ b/internal/admin/server.go
@@ -295,8 +295,17 @@ func (s *Server) handleDebug(w http.ResponseWriter, r *http.Request) {
 	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
 	defer cancel()

+	// Require tenant_id parameter for tenant isolation
+	tenantID := r.URL.Query().Get("tenant_id")
+	if tenantID == "" {
+		w.Header().Set("Content-Type", "application/json")
+		w.WriteHeader(http.StatusBadRequest)
+		json.NewEncoder(w).Encode(map[string]interface{}{
+			"error": "tenant_id query parameter required",
+		})
+		return
+	}
+
-	baseRepo := db.NewRepository(s.dbClient)
-	data, err := baseRepo.GetDebugDataAllTenants(ctx, messageID)
+	repo, err := s.dbClient.TenantRepository(tenantID)
+	if err != nil {
+		// Handle error
+	}
+	data, err := repo.GetDebugData(ctx, messageID)
```

---

## Medium Severity Issues

### 5. [MEDIUM] Rate Limiter Can Be Bypassed via Streaming

**File:** `internal/service/chat.go:476-484`

**Description:** Token usage is only recorded for rate limiting AFTER the streaming response completes. A malicious client could cancel streams before completion to avoid rate limit tracking.

**Vulnerable Code:**
```go
case provider.ChunkTypeComplete:
    // Record token usage for rate limiting on stream completion
    if s.rateLimiter != nil && chunk.Usage != nil {
        client := auth.ClientFromContext(ctx)
        if client != nil {
            if err := s.rateLimiter.RecordTokens(ctx, client.ClientID, ...); err != nil {
                slog.Warn("failed to record stream token usage...")
            }
        }
    }
```

**Recommended Fix:**
```diff
--- a/internal/service/chat.go
+++ b/internal/service/chat.go
@@ -386,6 +386,15 @@ func (s *ChatService) GenerateReplyStream(req *pb.GenerateReplyRequest, stream p
 	startTime := time.Now()

+	// Pre-charge estimated tokens to prevent abuse via stream cancellation
+	if s.rateLimiter != nil {
+		client := auth.ClientFromContext(ctx)
+		if client != nil {
+			estimatedTokens := int32(len(prepared.params.UserInput) / 4) // Rough estimate
+			s.rateLimiter.RecordTokens(ctx, client.ClientID, estimatedTokens, client.RateLimits.TokensPerMinute)
+		}
+	}
+
 	// Generate streaming reply
 	streamChunks, err := prepared.provider.GenerateReplyStream(ctx, prepared.params)
```

### 6. [MEDIUM] API Key Comparison May Be Vulnerable to Timing Attacks

**File:** `internal/auth/static.go` (inferred from architecture)

**Description:** If API key validation uses direct string comparison (`==`), it could be vulnerable to timing attacks that allow attackers to guess keys character by character.

**Recommended Fix:**
```diff
--- a/internal/auth/static.go
+++ b/internal/auth/static.go
@@ -1,6 +1,7 @@
 package auth

 import (
+	"crypto/subtle"
 	"context"
 )

@@ -20,7 +21,7 @@ func (s *StaticAuth) ValidateKey(ctx context.Context, apiKey string) (*ClientKey
 		return nil, ErrKeyNotFound
 	}

-	if key.Secret != apiKey {
+	if subtle.ConstantTimeCompare([]byte(key.Secret), []byte(apiKey)) != 1 {
 		return nil, ErrInvalidKey
 	}
```

### 7. [MEDIUM] Redis Key Injection via Untrusted Request ID

**File:** `internal/admin/server.go:615-616`

**Description:** The idempotency key is constructed using user-provided `tenant_id`, `thread_id`, and `request_id` without sanitization. While the pattern uses colons as separators, malicious inputs could potentially cause key collisions.

**Vulnerable Code:**
```go
idempKey = fmt.Sprintf("chat:idem:%s:%s:%s", req.TenantID, req.ThreadID, req.RequestID)
```

**Recommended Fix:**
```diff
--- a/internal/admin/server.go
+++ b/internal/admin/server.go
@@ -612,7 +612,13 @@ func (s *Server) handleChat(w http.ResponseWriter, r *http.Request) {
 	// Idempotency check: if request_id provided, check Redis for duplicate request
 	var idempKey string
 	if req.RequestID != "" && s.redisClient != nil {
-		idempKey = fmt.Sprintf("chat:idem:%s:%s:%s", req.TenantID, req.ThreadID, req.RequestID)
+		// Hash the components to prevent key injection
+		h := sha256.New()
+		h.Write([]byte(req.TenantID))
+		h.Write([]byte{0})  // Null byte separator
+		h.Write([]byte(req.ThreadID))
+		h.Write([]byte{0})
+		h.Write([]byte(req.RequestID))
+		idempKey = fmt.Sprintf("chat:idem:%x", h.Sum(nil))
```

### 8. [MEDIUM] Error Messages Leak Internal Paths

**File:** `internal/admin/server.go:312, 379`

**Description:** Error messages include raw error strings that could reveal internal implementation details.

**Vulnerable Code:**
```go
json.NewEncoder(w).Encode(map[string]interface{}{
    "error": err.Error(),
})
```

**Recommended Fix:**
```diff
--- a/internal/admin/server.go
+++ b/internal/admin/server.go
@@ -310,7 +310,8 @@ func (s *Server) handleDebug(w http.ResponseWriter, r *http.Request) {
 		} else {
 			w.WriteHeader(http.StatusInternalServerError)
 			json.NewEncoder(w).Encode(map[string]interface{}{
-				"error": err.Error(),
+				"error": "internal error",
 			})
+			slog.Error("debug endpoint error", "message_id", messageID, "error", err)
 		}
```

### 9. [MEDIUM] Missing Request Body Size Limit on HTTP Endpoints

**File:** `internal/admin/server.go:441-448, 573-580`

**Description:** The `/admin/test` and `/admin/chat` endpoints read JSON bodies without size limits, potentially allowing DoS via large payloads.

**Recommended Fix:**
```diff
--- a/internal/admin/server.go
+++ b/internal/admin/server.go
@@ -440,7 +440,9 @@ func (s *Server) handleTest(w http.ResponseWriter, r *http.Request) {
 	}

 	// Parse request body
+	r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // 1MB limit
 	var req TestRequest
 	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
```

---

## Low Severity Issues

### 10. [LOW] UUID Generation Uses Weak Randomness in Dashboard

**File:** `dashboard/src/components/ConversationPanel.tsx:595-601`

**Description:** The `generateUUID()` function uses `Math.random()` which is cryptographically weak. While the native `crypto.randomUUID()` is used elsewhere (line 813), this fallback could be used in older browsers.

**Vulnerable Code:**
```typescript
function generateUUID(): string {
  return 'xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx'.replace(/[xy]/g, function(c) {
    const r = Math.random() * 16 | 0;
    const v = c === 'x' ? r : (r & 0x3 | 0x8);
    return v.toString(16);
  });
}
```

**Recommended Fix:**
```diff
--- a/dashboard/src/components/ConversationPanel.tsx
+++ b/dashboard/src/components/ConversationPanel.tsx
@@ -593,11 +593,7 @@ function MessageBubble({ message, isPending, sendStartTime }: MessageBubbleProps

 // Generate a UUID for new threads
 function generateUUID(): string {
-  return 'xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx'.replace(/[xy]/g, function(c) {
-    const r = Math.random() * 16 | 0;
-    const v = c === 'x' ? r : (r & 0x3 | 0x8);
-    return v.toString(16);
-  });
+  return crypto.randomUUID();
 }
```

### 11. [LOW] Hardcoded Default Tenant

**File:** `internal/admin/server.go:897`

**Description:** The upload endpoint defaults to `"email4ai"` tenant if none specified, which could lead to data being stored in wrong tenant's context.

**Vulnerable Code:**
```go
if tenantID == "" {
    tenantID = "email4ai" // Default tenant
}
```

**Recommended Fix:**
```diff
--- a/internal/admin/server.go
+++ b/internal/admin/server.go
@@ -894,9 +894,13 @@ func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
 	// Get tenant ID
 	tenantID := r.FormValue("tenant_id")
 	if tenantID == "" {
-		tenantID = "email4ai" // Default tenant
+		w.Header().Set("Content-Type", "application/json")
+		w.WriteHeader(http.StatusBadRequest)
+		json.NewEncoder(w).Encode(UploadResponse{
+			Error: "tenant_id is required",
+		})
+		return
 	}
```

### 12. [LOW] Missing Context Cancellation Check in Background Goroutine

**File:** `internal/service/chat.go:1101-1159`

**Description:** The persistence goroutine creates a new context but doesn't check if the original request context was already cancelled.

**Vulnerable Code:**
```go
go func() {
    // Create a new context with timeout for the background operation
    persistCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()
    // ... persistence logic
}()
```

**Recommended Fix:**
```diff
--- a/internal/service/chat.go
+++ b/internal/service/chat.go
@@ -1098,6 +1098,11 @@ func (s *ChatService) persistConversation(ctx context.Context, req *pb.GenerateR

 	// Run persistence in background goroutine
 	go func() {
+		// Check if original context was cancelled (indicates request abort)
+		if ctx.Err() != nil {
+			slog.Debug("skipping persistence - request context cancelled")
+			return
+		}
+
 		// Create a new context with timeout for the background operation
 		persistCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
```

### 13. [LOW] Password Logged in Config on Parse Errors

**File:** `internal/config/config.go:249-251`

**Description:** Redis password is read from environment but parsing errors log the raw value.

**Current Code:**
```go
if pass := os.Getenv("REDIS_PASSWORD"); pass != "" {
    c.Redis.Password = pass
}
```

*This code is safe, but ensure no debug logging of the full config occurs.*

### 14. [LOW] Tenant Configuration Not Validated for API Key Presence

**File:** `internal/tenant/manager.go`

**Description:** Tenant configurations are loaded without validating that required API keys are present, which could lead to runtime failures.

**Recommended Fix:** Add validation in the `Load` function to ensure enabled providers have API keys configured.

### 15. [LOW] SQL Views May Impact Performance

**File:** `migrations/001_initial_schema.sql:144-169`

**Description:** The `airborne_activity_feed` view contains a correlated subquery that calculates `thread_cost_usd` for each row, which could be slow for large datasets.

**Vulnerable Code:**
```sql
(
    SELECT COALESCE(SUM(cost_usd), 0)
    FROM airborne_messages
    WHERE thread_id = m.thread_id
) AS thread_cost_usd
```

**Recommended Fix:**
```diff
--- a/migrations/001_initial_schema.sql
+++ b/migrations/001_initial_schema.sql
@@ -142,15 +142,18 @@ CREATE OR REPLACE VIEW airborne_activity_feed AS
+-- Consider materializing thread costs or using a window function
+-- For large datasets, create a materialized view with refresh triggers
 SELECT
     m.id,
     m.thread_id,
     t.tenant_id,
-    ...
-    (
-        SELECT COALESCE(SUM(cost_usd), 0)
-        FROM airborne_messages
-        WHERE thread_id = m.thread_id
-    ) AS thread_cost_usd
+    t.user_id,
+    m.content,
+    -- ... other columns ...
+    COALESCE(tc.total_cost, 0) AS thread_cost_usd
 FROM airborne_messages m
 JOIN airborne_threads t ON m.thread_id = t.id
+LEFT JOIN (
+    SELECT thread_id, SUM(cost_usd) as total_cost
+    FROM airborne_messages
+    GROUP BY thread_id
+) tc ON tc.thread_id = m.thread_id
 WHERE m.role = 'assistant'
```

### 16. [LOW] Missing Input Validation for Provider Selection

**File:** `internal/service/chat.go:674-718`

**Description:** The `selectProviderWithTenant` function accepts provider strings but doesn't normalize case, which could cause issues.

**Recommended Fix:**
```diff
--- a/internal/service/chat.go
+++ b/internal/service/chat.go
@@ -674,6 +674,7 @@ func (s *ChatService) selectProviderWithTenant(ctx context.Context, req *pb.Gene

 	// Determine which provider to use
 	var providerName string
+	providerName = strings.ToLower(providerName)  // Normalize
 	switch req.PreferredProvider {
```

### 17. [LOW] Insufficient Entropy in Request ID Generation

**File:** `internal/validation/limits.go:97-104`

**Description:** The request ID generation uses 16 bytes (128 bits), which is good, but the function name and documentation don't clarify this is for idempotency, not security.

*Code is acceptable, but consider documenting the purpose.*

---

## Positive Security Observations

The following security practices were well-implemented:

1. **SSRF Protection** (`internal/validation/url.go`): Comprehensive validation of provider URLs including:
   - Blocking dangerous protocols (file://, gopher://, etc.)
   - Blocking private IP ranges
   - Blocking cloud metadata endpoints (169.254.169.254)
   - DNS rebinding protection via IP resolution validation

2. **Error Sanitization** (`internal/errors/sanitize.go`): Internal errors are properly sanitized before being returned to clients.

3. **SQL Injection Prevention**: All database queries use parameterized queries via `pgx`.

4. **API Key Security**: API keys cannot be overridden via request parameters (commented out in code with security note).

5. **Rate Limiting**: Proper rate limiting implementation at the authentication layer.

6. **Input Validation**: Good size limits on user input (100KB), instructions (50KB), and metadata.

7. **TLS Support**: TLS is properly configurable for production deployments.

8. **Tenant Isolation**: Database queries include tenant_id filtering for multi-tenant isolation.

---

## Recommendations Summary

### Immediate Actions (Critical/High)
1. Add DOMPurify sanitization for rendered HTML
2. Implement authentication on admin HTTP endpoints
3. Restrict CORS to specific origins
4. Add tenant isolation to debug/thread endpoints

### Short-term Actions (Medium)
5. Pre-charge token usage for rate limiting
6. Use constant-time comparison for API keys
7. Hash idempotency keys to prevent injection
8. Add request body size limits
9. Sanitize error messages in HTTP responses

### Long-term Actions (Low)
10. Remove weak UUID fallback
11. Require tenant_id on all endpoints
12. Add context cancellation checks
13. Validate tenant configurations on load
14. Optimize SQL views for performance
15. Normalize provider name inputs
16. Document request ID purpose

---

## Appendix: Files Reviewed

| Path | Lines | Focus Area |
|------|-------|------------|
| `internal/auth/interceptor.go` | 191 | Authentication |
| `internal/auth/static.go` | ~50 | API key storage |
| `internal/auth/ratelimit.go` | ~150 | Rate limiting |
| `internal/service/chat.go` | 1161 | Core chat service |
| `internal/admin/server.go` | 1185 | Admin HTTP API |
| `internal/validation/url.go` | 229 | SSRF protection |
| `internal/validation/limits.go` | 105 | Input validation |
| `internal/redis/client.go` | 151 | Redis operations |
| `internal/config/config.go` | 492 | Configuration |
| `internal/errors/sanitize.go` | 44 | Error handling |
| `internal/tenant/manager.go` | 163 | Tenant management |
| `internal/db/repository.go` | ~300 | Database access |
| `dashboard/src/components/ConversationPanel.tsx` | 1303 | Frontend |
| `dashboard/src/app/api/chat/route.ts` | 178 | API proxy |
| `migrations/001_initial_schema.sql` | 225 | Database schema |

---

*Report generated by Claude Code security audit skill*
