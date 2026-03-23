Date Created: 2026-03-21T03:03:13+0100
TOTAL_SCORE: 68/100

# Airborne Codebase Audit Report

**Auditor:** Claude Code (Opus 4.6)
**Project:** Airborne — Multi-Provider AI Gateway (Go/gRPC)
**Version Audited:** 1.8.11
**Files Analyzed:** 90+ Go source files across 20+ packages

---

## Score Breakdown

| Category | Weight | Score | Notes |
|----------|--------|-------|-------|
| Security — Authentication | 15% | 10/15 | Constant-time compare length oracle; differentiated error messages leak key state |
| Security — Authorization | 10% | 5/10 | Admin HTTP server has **zero authentication**; wildcard CORS |
| Security — Input/Output | 10% | 7/10 | Good SSRF protection but DNS rebinding not mitigated; error sanitizer is non-deterministic |
| Security — Data Layer | 10% | 6/10 | SQL table names via `fmt.Sprintf` with whitelist-only defense; test tenant in prod code |
| Code Quality — Error Handling | 10% | 7/10 | Mostly correct; some gRPC handlers return `fmt.Errorf` instead of `status.Error` |
| Code Quality — Concurrency | 10% | 5/10 | Data race on lazy gRPC client init; unbounded goroutine pool in persistence; TOCTOU in RAG |
| Code Quality — Resource Management | 10% | 7/10 | Redis client not closed on shutdown; httpcapture reads full body into memory |
| Architecture & Design | 10% | 8/10 | Clean layering, good provider abstraction, solid multi-tenancy model |
| Testing | 10% | 6/10 | Core packages well tested; admin server (largest handler) has zero tests; 14 providers untested |
| Build & Deployment | 5% | 3/5 | Lint silently skipped in CI; OTel hardcoded insecure; `go 1.25.5` doesn't exist |

**Total: 68/100**

---

## Findings by Severity

### CRITICAL (2)

#### C-1: Admin HTTP Server Has No Authentication
**File:** `internal/admin/server.go:108-118`
**Impact:** All admin endpoints (activity feed, debug payloads with raw AI request/response JSON, thread history, test/chat triggers) are accessible without any authentication. Combined with `AllowOrigins: ["*"]` CORS at line 124, any webpage can make cross-origin requests to the admin server.

An attacker on the network can:
- Read all conversation history and AI provider request/response payloads
- Trigger arbitrary AI generation calls billed to configured tenants
- Upload files via `/admin/upload`

```diff
--- a/internal/admin/server.go
+++ b/internal/admin/server.go
@@ -87,6 +87,24 @@ func NewServer(dbClient *db.Client, cfg Config) *Server {

 	mux := http.NewServeMux()

+	// Authentication middleware for admin endpoints
+	requireAuth := func(next http.HandlerFunc) http.HandlerFunc {
+		return func(w http.ResponseWriter, r *http.Request) {
+			token := r.Header.Get("Authorization")
+			token = strings.TrimPrefix(token, "Bearer ")
+			if token == "" {
+				token = r.Header.Get("X-Api-Key")
+			}
+			if token == "" || subtle.ConstantTimeCompare(
+				sha256Sum([]byte(token)),
+				sha256Sum([]byte(s.authToken)),
+			) != 1 {
+				http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
+				return
+			}
+			next(w, r)
+		}
+	}
+
 	// Register chassis-go health check endpoint with parallel dependency checks
 	healthChecks := map[string]health.Check{
 		"self": func(_ context.Context) error { return nil },
@@ -106,13 +124,13 @@ func NewServer(dbClient *db.Client, cfg Config) *Server {

 	// Register endpoints
-	mux.HandleFunc("/admin/activity", s.handleActivity)
-	mux.HandleFunc("/admin/debug/", s.handleDebug)
-	mux.HandleFunc("/admin/thread/", s.handleThread)
+	mux.HandleFunc("/admin/activity", requireAuth(s.handleActivity))
+	mux.HandleFunc("/admin/debug/", requireAuth(s.handleDebug))
+	mux.HandleFunc("/admin/thread/", requireAuth(s.handleThread))
 	mux.HandleFunc("/admin/health", s.handleHealth)        // health stays unauthenticated
 	mux.Handle("/admin/healthz", health.Handler(healthChecks))
-	mux.HandleFunc("/admin/version", s.handleVersion)
+	mux.HandleFunc("/admin/version", requireAuth(s.handleVersion))
 	maxBody := guard.MaxBody(2 * 1024 * 1024)
-	mux.Handle("/admin/test", maxBody(http.HandlerFunc(s.handleTest)))
-	mux.Handle("/admin/chat", maxBody(http.HandlerFunc(s.handleChat)))
-	mux.HandleFunc("/admin/upload", s.handleUpload)
+	mux.Handle("/admin/test", maxBody(http.HandlerFunc(requireAuth(s.handleTest))))
+	mux.Handle("/admin/chat", maxBody(http.HandlerFunc(requireAuth(s.handleChat))))
+	mux.HandleFunc("/admin/upload", requireAuth(s.handleUpload))
```

Additionally, restrict CORS:
```diff
--- a/internal/admin/server.go
+++ b/internal/admin/server.go
@@ -123,7 +123,7 @@ func NewServer(dbClient *db.Client, cfg Config) *Server {
 	handler := httpkit.Recovery(logger)(
 		guard.CORS(guard.CORSConfig{
-			AllowOrigins: []string{"*"},
+			AllowOrigins: []string{"http://localhost:*", "https://dashboard.internal"},
 			AllowMethods: []string{"GET", "POST", "OPTIONS"},
 			AllowHeaders: []string{"Content-Type", "Authorization"},
 		})(
```

---

#### C-2: Data Race on Lazy gRPC Client Initialization
**File:** `internal/admin/server.go:448-468`
**Impact:** `getGRPCClient()` is called concurrently from HTTP handler goroutines. Both `s.grpcConn` and `s.grpcClient` are read/written without synchronization, causing a data race. One connection will be leaked.

```diff
--- a/internal/admin/server.go
+++ b/internal/admin/server.go
@@ -36,6 +36,7 @@ type Server struct {
+	grpcMu      sync.Mutex
 	grpcConn    *grpc.ClientConn
 	grpcClient  pb.AirborneServiceClient

@@ -448,6 +449,8 @@ func (s *Server) getGRPCClient() (pb.AirborneServiceClient, error) {
+	s.grpcMu.Lock()
+	defer s.grpcMu.Unlock()
+
 	if s.grpcClient != nil {
 		return s.grpcClient, nil
 	}
```

---

### HIGH (8)

#### H-1: `subtle.ConstantTimeCompare` Length Oracle in Static Auth
**File:** `internal/auth/static.go:72`
**Impact:** Go's `subtle.ConstantTimeCompare` short-circuits on length inequality without constant-time comparison. An attacker can determine the correct token length by timing responses, narrowing brute-force search space.

```diff
--- a/internal/auth/static.go
+++ b/internal/auth/static.go
@@ -1,7 +1,9 @@
 import (
 	"context"
+	"crypto/hmac"
+	"crypto/sha256"
 	"crypto/subtle"
 	"strings"

@@ -70,7 +72,10 @@ func (a *StaticAuthenticator) authenticate(ctx context.Context) (context.Context
 		return nil, status.Error(codes.Unauthenticated, "missing API key")
 	}

-	if subtle.ConstantTimeCompare([]byte(token), []byte(a.adminToken)) != 1 {
+	// Use HMAC comparison to prevent length oracle:
+	// subtle.ConstantTimeCompare short-circuits on length mismatch.
+	mac := hmac.New(sha256.New, []byte("airborne-static-auth"))
+	if !hmac.Equal(mac.Sum([]byte(token)), mac.Sum([]byte(a.adminToken))) {
 		return nil, status.Error(codes.Unauthenticated, "invalid API key")
 	}
```

**Alternative (simpler — fixed-length SHA-256 comparison):**
```diff
-	if subtle.ConstantTimeCompare([]byte(token), []byte(a.adminToken)) != 1 {
+	tokenHash := sha256.Sum256([]byte(token))
+	expectedHash := sha256.Sum256([]byte(a.adminToken))
+	if subtle.ConstantTimeCompare(tokenHash[:], expectedHash[:]) != 1 {
 		return nil, status.Error(codes.Unauthenticated, "invalid API key")
 	}
```

---

#### H-2: SQL Table Names Injected via `fmt.Sprintf` — Whitelist-Only Defense
**File:** `internal/db/repository.go:14-19, 34-35, 57-60`
**Impact:** Every SQL query interpolates table names via `fmt.Sprintf`. The sole defense is a hardcoded `ValidTenantIDs` map. The deprecated `NewRepository()` at line 34 creates a repository with empty prefix, bypassing validation. If `ValidTenantIDs` is ever extended with user-controlled input, all queries become injectable.

```diff
--- a/internal/db/repository.go
+++ b/internal/db/repository.go
@@ -1,8 +1,10 @@
 import (
 	"context"
 	"errors"
 	"fmt"
 	"log/slog"
+	"regexp"
 	"strings"
 )

+var validTableIdentifier = regexp.MustCompile(`^[a-z0-9_]+$`)
+
 // ValidTenantIDs contains the list of valid tenant IDs.
 var ValidTenantIDs = map[string]bool{
 	"ai8":      true,
 	"email4ai": true,
-	"zztest":   true,
 }

-// NewRepository creates a new repository backed by the given client.
-// Deprecated: Use NewTenantRepository for tenant-specific operations.
-func NewRepository(client *Client) *Repository {
-	return &Repository{client: client, tablePrefix: "", tenantID: ""}
-}
-
 // NewTenantRepository creates a new repository scoped to a specific tenant's tables.
 func NewTenantRepository(client *Client, tenantID string) (*Repository, error) {
 	if !ValidTenantIDs[tenantID] {
 		return nil, fmt.Errorf("%w: got %q", ErrInvalidTenant, tenantID)
 	}
+	if !validTableIdentifier.MatchString(tenantID) {
+		return nil, fmt.Errorf("%w: invalid characters in tenant ID %q", ErrInvalidTenant, tenantID)
+	}
 	return &Repository{
```

---

#### H-3: Unbounded Goroutine Pool in `persistConversation`
**File:** `internal/service/chat.go:1114-1175`
**Impact:** Every chat request spawns an unconstrained `go func()` for database persistence. Under sustained load with a slow database, goroutine count grows unbounded until OOM.

```diff
--- a/internal/service/chat.go
+++ b/internal/service/chat.go
@@ Add a semaphore field to the Service struct and use it:
+import "golang.org/x/sync/semaphore"
+
 type Service struct {
 	// ... existing fields ...
+	persistSem *semaphore.Weighted // limits concurrent persistence goroutines
 }

 func NewService(...) *Service {
 	return &Service{
 		// ... existing init ...
+		persistSem: semaphore.NewWeighted(64), // max 64 concurrent DB writes
 	}
 }

 func (s *Service) persistConversation(...) {
-	go func() {
+	go func() {
+		if !s.persistSem.TryAcquire(1) {
+			slog.Warn("persistence queue full, dropping write")
+			return
+		}
+		defer s.persistSem.Release(1)
 		// ... existing persistence logic ...
 	}()
 }
```

---

#### H-4: Differentiated Auth Error Messages Enable Key-State Oracle
**File:** `internal/auth/interceptor.go:117-121`
**Impact:** Returning `"API key expired"` vs `"invalid API key"` tells attackers whether a guessed key ID exists (was found in Redis but expired) vs. never existed.

```diff
--- a/internal/auth/interceptor.go
+++ b/internal/auth/interceptor.go
@@ -117,8 +117,7 @@ func (a *Authenticator) authenticate(ctx context.Context, apiKey string) (*Clien
 		switch err {
-		case ErrKeyNotFound, ErrInvalidKey:
-			return nil, status.Error(codes.Unauthenticated, "invalid API key")
-		case ErrKeyExpired:
-			return nil, status.Error(codes.Unauthenticated, "API key expired")
+		case ErrKeyNotFound, ErrInvalidKey, ErrKeyExpired:
+			return nil, status.Error(codes.Unauthenticated, "authentication failed")
 		default:
 			return nil, status.Error(codes.Internal, "authentication error")
 		}
```

---

#### H-5: Missing Redis Client Close on Graceful Shutdown
**File:** `internal/server/grpc.go:277-282`
**Impact:** `ServerComponents.Close()` only closes DBClient. The Redis connection pool leaks on every clean shutdown, leaving zombie connections until Redis times them out.

```diff
--- a/internal/server/grpc.go
+++ b/internal/server/grpc.go
@@ -277,6 +277,9 @@ func (c *ServerComponents) Close() {
+	if c.RedisClient != nil {
+		c.RedisClient.Close()
+	}
 	if c.DBClient != nil {
 		c.DBClient.Close()
 	}
 }
```

---

#### H-6: Token-Per-Minute Rate Limiting Is Post-Hoc Only
**File:** `internal/auth/ratelimit.go:96-141`
**Impact:** `RecordTokens` is called after the AI response is served. The TPM limit check at lines 136-139 returns `ErrRateLimitExceeded`, but the request was already fully processed and billed. Abusive clients can exceed TPM limits on every request.

```diff
--- a/internal/auth/ratelimit.go
+++ b/internal/auth/ratelimit.go
@@ Recommended approach: add a pre-request token budget check
+// CheckTokenBudget verifies that the client has remaining token budget
+// BEFORE processing the request. Call this in the interceptor chain.
+func (rl *RateLimiter) CheckTokenBudget(ctx context.Context, client *ClientKey) error {
+	if client.RateLimits.TokensPerMinute == 0 {
+		return nil // no TPM limit configured
+	}
+	usage, err := rl.GetUsage(ctx, client.KeyID)
+	if err != nil {
+		return nil // fail open on monitoring errors
+	}
+	if usage.TokensPerMinute >= int64(client.RateLimits.TokensPerMinute) {
+		return ErrRateLimitExceeded
+	}
+	return nil
+}
```

---

#### H-7: `handleChatWithFile` Bypasses Auth and Rate Limiting
**File:** `internal/admin/server.go:1124-1324`
**Impact:** When the admin chat endpoint receives a `file_uri`, it creates a Gemini client directly and calls `GenerateReply` without going through the gRPC interceptor chain. Combined with the unauthenticated admin server (C-1), this is an open proxy for Gemini API calls.

*Fix:* Route the request through the gRPC client (with auth) instead of creating a direct Gemini client, or at minimum add authentication to the admin server (see C-1 fix).

---

#### H-8: Backoff Integer Overflow at High Attempt Counts
**File:** `internal/retry/backoff.go:12`
**Impact:** `1<<uint(attempt-1)` overflows at attempt >= 63 (64-bit), producing zero or negative delay. Currently safe with `MaxAttempts=3`, but latent if the constant is increased.

```diff
--- a/internal/retry/backoff.go
+++ b/internal/retry/backoff.go
@@ -9,7 +9,12 @@ import (
 // SleepWithBackoff sleeps with exponential backoff.
 func SleepWithBackoff(ctx context.Context, attempt int) {
-	delay := BackoffBase * time.Duration(1<<uint(attempt-1))
+	shift := attempt - 1
+	if shift > 30 {
+		shift = 30 // cap to prevent overflow
+	}
+	delay := BackoffBase * time.Duration(1<<uint(shift))
+	if delay > 30*time.Second {
+		delay = 30 * time.Second
+	}
 	select {
 	case <-ctx.Done():
 	case <-time.After(delay):
```

---

### MEDIUM (12)

#### M-1: DNS Rebinding Not Mitigated in URL Validation
**File:** `internal/validation/url.go:50-71`
**Impact:** `validateHostnameResolvesPublic` resolves the hostname at validation time but the actual HTTP request re-resolves DNS. An attacker can point a hostname to a public IP during validation, then rebind to `169.254.169.254` before the request is made.

*Fix:* Pin the resolved IP at validation time and use a custom `net.Dialer` that forces the pinned IP.

---

#### M-2: Error Sanitizer Uses Non-Deterministic Map Iteration
**File:** `internal/errors/sanitize.go:9-18, 29-38`
**Impact:** When multiple patterns match the same error string, the returned safe message depends on Go's random map iteration order. This makes behavior untestable and inconsistent.

```diff
--- a/internal/errors/sanitize.go
+++ b/internal/errors/sanitize.go
@@ -8,15 +8,20 @@ import (
 )

-var clientSafePatterns = map[string]string{
-	"rate limit":   "rate limit exceeded",
-	"quota":        "quota exceeded",
-	"timeout":      "request timed out",
-	"context dead": "request cancelled",
-	"invalid api":  "authentication failed with provider",
-	"unauthorized": "authentication failed with provider",
-	"forbidden":    "access denied by provider",
-	"not found":    "resource not found",
+type safePattern struct {
+	pattern string
+	message string
+}
+
+var clientSafePatterns = []safePattern{
+	{"rate limit", "rate limit exceeded"},
+	{"quota", "quota exceeded"},
+	{"timeout", "request timed out"},
+	{"context dead", "request cancelled"},
+	{"invalid api", "authentication failed with provider"},
+	{"unauthorized", "authentication failed with provider"},
+	{"forbidden", "access denied by provider"},
+	{"not found", "resource not found"},
 }

 func SanitizeForClient(err error) string {
@@ -29,8 +34,8 @@ func SanitizeForClient(err error) string {
 	errLower := strings.ToLower(err.Error())

-	for pattern, safeMsg := range clientSafePatterns {
-		if strings.Contains(errLower, pattern) {
-			slog.Debug("sanitizing error for client",
+	for _, p := range clientSafePatterns {
+		if strings.Contains(errLower, p.pattern) {
+			slog.Warn("sanitizing provider error for client",
 				"original", err.Error(),
-				"sanitized", safeMsg,
+				"sanitized", p.message,
 			)
-			return safeMsg
+			return p.message
 		}
 	}
```

---

#### M-3: FileService Endpoints Skip Tenant Interceptor
**File:** `internal/auth/tenant_interceptor.go:31-41`
**Impact:** `CreateFileStore`, `UploadFile`, `DeleteFileStore`, `GetFileStore`, `ListFileStores` are in `skipMethods` and receive no tenant context. If any handler falls back to permissive behavior when tenant context is nil, files can be operated on without tenant boundaries.

*Fix:* Remove FileService from `skipMethods` or ensure each handler independently validates tenant context.

---

#### M-4: gRPC Max Message Size 100MB Applied to All Services
**File:** `internal/server/grpc.go:151-153`
**Impact:** The 100MB limit is set globally, not scoped to `FileService`. Any RPC can receive a 100MB payload, causing memory exhaustion under load.

*Fix:* Use per-service max message size options or apply `grpc.MaxRecvMsgSize` only to the file service via a service-specific interceptor.

---

#### M-5: OTel Insecure Transport Hardcoded in Production Binary
**File:** `cmd/airborne/main.go:78`
**Impact:** `Insecure: true` is passed unconditionally to `otelinit.Init`, transmitting all traces and metrics in plaintext even in production.

*Fix:* Make this configurable via `OTEL_INSECURE` env var or the config file.

---

#### M-6: Frozen Config Directory Created with 0755 Permissions
**File:** `cmd/airborne-freeze/main.go:90`
**Impact:** `os.MkdirAll(..., 0755)` creates the output directory world-readable. While the JSON file itself is 0600, any local user can list directory contents.

```diff
-	os.MkdirAll(filepath.Dir(outputPath), 0755)
+	os.MkdirAll(filepath.Dir(outputPath), 0700)
```

---

#### M-7: No Backoff Jitter (Thundering Herd)
**File:** `internal/retry/backoff.go:9-17`
**Impact:** Pure exponential backoff without jitter. Concurrent failures retry at identical intervals, amplifying bursts against rate-limited providers.

```diff
--- a/internal/retry/backoff.go
+++ b/internal/retry/backoff.go
@@ -4,6 +4,7 @@ import (
 	"context"
+	"math/rand"
 	"time"
 )

 func SleepWithBackoff(ctx context.Context, attempt int) {
 	delay := BackoffBase * time.Duration(1<<uint(attempt-1))
+	// Add ±25% jitter to prevent thundering herd
+	jitter := time.Duration(rand.Int63n(int64(delay) / 2)) - delay/4
+	delay += jitter
 	select {
```

---

#### M-8: Chunker Slices Bytes, Not Runes (UTF-8 Corruption)
**File:** `internal/rag/chunker/chunker.go:79-118`
**Impact:** `text[start:end]` slices bytes, not Unicode codepoints. For multi-byte characters (CJK, emoji), a split at a byte boundary produces invalid UTF-8 chunks.

```diff
--- a/internal/rag/chunker/chunker.go
+++ b/internal/rag/chunker/chunker.go
@@ Consider converting to rune-based slicing:
+	runes := []rune(text)
+	// ... use runes[start:end] throughout instead of text[start:end] ...
+	// ... convert back: chunkText := string(runes[start:end]) ...
```

---

#### M-9: CA Certificate Written to Predictable `/tmp` Path
**File:** `internal/db/postgres.go:141-155`
**Impact:** CA cert written to `/tmp/airborne-certs/supabase-ca.crt` — predictable and readable by any local user on shared systems.

```diff
-	certDir := "/tmp/airborne-certs"
+	certDir, err := os.MkdirTemp("", "airborne-certs-*")
+	if err != nil {
+		return "", fmt.Errorf("create cert dir: %w", err)
+	}
```

---

#### M-10: Doppler API Error Body Leaked Verbatim in Error Messages
**File:** `internal/tenant/doppler.go:147-149`
**Impact:** Raw Doppler API error body injected into returned error. May contain project structure, token scope, or internal system details.

```diff
-	return nil, resp.StatusCode, fmt.Errorf("doppler API error (status %d): %s", resp.StatusCode, string(body))
+	bodyStr := string(body)
+	if len(bodyStr) > 256 {
+		bodyStr = bodyStr[:256] + "...(truncated)"
+	}
+	slog.Debug("doppler API error details", "status", resp.StatusCode, "body", bodyStr)
+	return nil, resp.StatusCode, fmt.Errorf("doppler API error (status %d)", resp.StatusCode)
```

---

#### M-11: Doppler Retry Blocks Startup for Up to 51 Seconds
**File:** `internal/tenant/doppler.go:62-67`
**Impact:** 15 retries with exponential backoff up to 5s cap means ~51 seconds of synchronous blocking during `sync.Once` initialization. All goroutines calling `initDopplerClient` block.

*Fix:* Reduce `maxRetries` to 5 (max ~7s blocking) or make initialization async with a readiness gate.

---

#### M-12: `handleDebug` and `handleThread` Expose Raw DB Errors
**File:** `internal/admin/server.go:350-352, 417-419`
**Impact:** Raw database errors (potentially containing SQL fragments) returned to HTTP clients. Already amplified by the lack of auth (C-1).

```diff
-	"error": err.Error(),
+	"error": "internal server error",
```

---

### LOW (12)

| ID | File | Line(s) | Issue |
|----|------|---------|-------|
| L-1 | `internal/auth/ratelimit.go` | 63-92 | Zero rate limits silently treated as "use default"; if default is also 0, infinite capacity |
| L-2 | `internal/auth/ratelimit.go` | 204-213 | Malformed Redis rate-limit values silently treated as 0 |
| L-3 | `internal/auth/tenant_interceptor.go` | 191-213 | Race in streaming `RecvMsg` — `alreadySet` read outside lock, then re-checked inside |
| L-4 | `internal/service/files.go` | 319-325, 360-366 | Upload failures return 200 with `status: "failed"` instead of gRPC error codes |
| L-5 | `internal/service/files.go` | 205-212 | Rate limiting silently bypassed when `client == nil` |
| L-6 | `internal/db/repository.go` | 673-674 | `"message not found"` returned as plain error, not `status.NotFound` |
| L-7 | `internal/tenant/manager.go` | 46-110 | Uses `fmt.Fprintf(os.Stderr)` instead of `slog`, bypassing structured logging |
| L-8 | `internal/httpcapture/transport.go` | 74-83 | `io.ReadAll(resp.Body)` reads full body into memory with no size cap |
| L-9 | `Makefile` | 62-66 | `lint` target exits 0 when `golangci-lint` is missing — false green in CI |
| L-10 | `docker-compose.yml` | 12 | `AIRBORNE_ADMIN_TOKEN` defaults to empty string |
| L-11 | `go.mod` | 3 | Declares `go 1.25.5` which does not exist as of March 2026 |
| L-12 | `Dockerfile` | 29 | Missing `-ldflags "-s -w"` — debug symbols in production image |

---

### INFO (6)

| ID | File | Line(s) | Issue |
|----|------|---------|-------|
| I-1 | `internal/auth/static.go` | 77-81 | All static-auth callers share `ClientID: "admin"` — no per-caller audit trail |
| I-2 | `internal/auth/keys.go` | 97 | `bcrypt.DefaultCost` (10) — acceptable now but should be versioned for future increases |
| I-3 | `internal/db/models.go` | 34-35 | `Message.Role` is a free string with no enum enforcement at model level |
| I-4 | `internal/validation/limits.go` | 77-94 | Request ID pattern allows single-character IDs — collision risk in high-volume |
| I-5 | `internal/imagegen/openai.go` | 26 | Creates a new OpenAI SDK client per request — should be pooled |
| I-6 | N/A | N/A | Test coverage gaps: `internal/admin/` (0 tests), 14 compat providers (0 tests), `internal/cli/` (0 tests), `internal/db/postgres.go` (0 tests) |

---

## Test Coverage Summary

| Package | Has Tests | Coverage Quality |
|---------|-----------|-----------------|
| `internal/auth/` | Yes (6 test files) | Good — covers interceptor chain, keys, static, rate limiting, tenant |
| `internal/config/` | Yes (2 test files) | Good — config loading, env overrides, startup mode |
| `internal/service/` | Yes (4 test files) | Moderate — chat, files, admin, config builder tested |
| `internal/validation/` | Yes (1 test file) | Good — limits tested |
| `internal/rag/` | Yes (3 test files) | Good — service, chunker, validation tested |
| `internal/retry/` | Yes (1 test file) | Moderate |
| `internal/provider/openai/` | Yes | Moderate |
| `internal/provider/anthropic/` | Yes | Moderate |
| `internal/provider/gemini/` | Yes | Moderate |
| `internal/provider/compat/` | Yes | Moderate |
| **`internal/admin/`** | **No** | **NONE — largest handler file in project** |
| **`internal/cli/`** | **No** | **NONE** |
| **`internal/db/postgres.go`** | **No** | **NONE — connection/migration logic untested** |
| **14 compat providers** | **No** | **NONE — cerebras, cohere, deepinfra, deepseek, etc.** |

---

## Architecture Observations (Positive)

1. **Clean layered design** — Clear separation of transport (gRPC), service, provider, and data layers
2. **Provider abstraction** — The `Provider` interface with per-provider implementations is well-designed and extensible
3. **Multi-tenancy model** — Per-tenant configs, secret isolation, DB table prefixing is thoughtful
4. **Interceptor chain** — Recovery → Tracing → Logging → Tenant → Auth follows best practices
5. **SSRF protection** — `ssrfcheck.IsBlockedIP` integration with chassis-go-addons is correct
6. **Secret resolution** — `ENV=`/`FILE=` reference patterns with `FILE=` path traversal protection
7. **Frozen config** — Production config snapshot with `ReplaceSecretsWithReferences` is a solid operational pattern
8. **Error sanitization** — The principle of sanitizing errors before client response is correct (implementation issues noted above)
9. **Atomic rate limiting** — Redis Lua scripts for RPM/RPD counters prevent race conditions

---

## Remediation Priority

**Immediate (P0):**
1. C-1: Add authentication to admin HTTP server
2. C-2: Fix data race in `getGRPCClient`
3. H-1: Fix constant-time compare length oracle
4. H-2: Add secondary SQL identifier validation

**This Sprint (P1):**
5. H-3: Add goroutine pool for persistence
6. H-4: Unify auth error messages
7. H-5: Close Redis client on shutdown
8. M-2: Make error sanitizer deterministic
9. M-7: Add jitter to backoff

**Next Sprint (P2):**
10. M-1: DNS rebinding mitigation
11. M-4: Scope max message size to FileService
12. M-8: Fix UTF-8 chunker
13. M-9: Use temp dir for CA cert
14. I-6: Add tests for admin server and compat providers

---

*End of audit report.*
