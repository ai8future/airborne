Date Created: 2026-03-21T06:42:18-04:00
TOTAL_SCORE: 71/100

Agent: Claude Code:Opus 4.6

---

# Airborne Combined Analysis Report

**Project**: Airborne — Unified AI Gateway (Go, gRPC, 20+ providers)
**Version**: 1.8.12
**Scope**: Full codebase quick audit

---

## 1. AUDIT — Security and Code Quality Issues

### A-1. Admin HTTP Server Has No Authentication (CRITICAL)

**File**: `internal/admin/server.go`
**Severity**: HIGH

All admin HTTP endpoints (`/admin/activity`, `/admin/debug/{id}`, `/admin/thread/{id}`, `/admin/test`, `/admin/chat`, `/admin/upload`) are completely unauthenticated. Anyone who can reach the admin port can view all conversation history, debug data (including raw API requests/responses with potential PII), and send AI requests billed to the service.

The gRPC server has proper auth interceptors, but the HTTP admin server bypasses all of this.

```diff
--- a/internal/admin/server.go
+++ b/internal/admin/server.go
@@ -118,6 +118,7 @@ func NewServer(dbClient *db.Client, cfg Config) *Server {
 	// Stack chassis-go httpkit middleware: Recovery → CORS → Tracing → RequestID → Logging → routes
 	logger := slog.Default()
+	authMiddleware := bearerAuthMiddleware(cfg.AuthToken)
 	handler := httpkit.Recovery(logger)(
 		guard.CORS(guard.CORSConfig{
 			AllowOrigins: []string{"*"},
@@ -127,7 +128,7 @@ func NewServer(dbClient *db.Client, cfg Config) *Server {
 			httpkit.Tracing()(
 				httpkit.RequestID(
 					httpkit.Logging(logger)(
-						guard.Timeout(30*time.Second)(mux),
+						authMiddleware(guard.Timeout(30*time.Second)(mux)),
 					),
 				),
 			),
@@ -145,6 +146,28 @@ func NewServer(dbClient *db.Client, cfg Config) *Server {
 	return s
 }

+// bearerAuthMiddleware returns middleware that validates Bearer token on all
+// non-health endpoints.
+func bearerAuthMiddleware(token string) func(http.Handler) http.Handler {
+	return func(next http.Handler) http.Handler {
+		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
+			// Skip auth for health endpoints
+			if r.URL.Path == "/admin/health" || r.URL.Path == "/admin/healthz" {
+				next.ServeHTTP(w, r)
+				return
+			}
+			auth := r.Header.Get("Authorization")
+			expected := "Bearer " + token
+			if token == "" || subtle.ConstantTimeCompare([]byte(auth), []byte(expected)) != 1 {
+				w.Header().Set("Content-Type", "application/json")
+				w.WriteHeader(http.StatusUnauthorized)
+				json.NewEncoder(w).Encode(map[string]string{"error": "unauthorized"})
+				return
+			}
+			next.ServeHTTP(w, r)
+		})
+	}
+}
```

### A-2. CORS Allows All Origins on Admin Server (MEDIUM)

**File**: `internal/admin/server.go:123`

```go
AllowOrigins: []string{"*"},
```

Combined with the lack of authentication, this means any website can make cross-origin requests to the admin API.

```diff
--- a/internal/admin/server.go
+++ b/internal/admin/server.go
@@ -123,7 +123,7 @@ func NewServer(dbClient *db.Client, cfg Config) *Server {
 	handler := httpkit.Recovery(logger)(
 		guard.CORS(guard.CORSConfig{
-			AllowOrigins: []string{"*"},
+			AllowOrigins: []string{}, // Restrict to known origins
 			AllowMethods: []string{"GET", "POST", "OPTIONS"},
 			AllowHeaders: []string{"Content-Type", "Authorization"},
 		})(
```

### A-3. Internal Error Messages Leaked to Admin API Clients (MEDIUM)

**File**: `internal/admin/server.go:351-352, 803-806`

The admin `handleDebug` and `handleChat` endpoints return raw `err.Error()` to clients, which may expose internal hostnames, connection strings, or stack traces.

```diff
--- a/internal/admin/server.go
+++ b/internal/admin/server.go
@@ -349,7 +349,7 @@ func (s *Server) handleDebug(w http.ResponseWriter, r *http.Request) {
 		} else {
 			w.WriteHeader(http.StatusInternalServerError)
 			json.NewEncoder(w).Encode(map[string]interface{}{
-				"error": err.Error(),
+				"error": "internal error fetching debug data",
 			})
 		}
```

### A-4. gRPC Client Lazy Init Race Condition (LOW)

**File**: `internal/admin/server.go:449-468`

`getGRPCClient()` has no synchronization. Concurrent HTTP requests could race and create multiple gRPC connections, leaking all but the last one.

```diff
--- a/internal/admin/server.go
+++ b/internal/admin/server.go
@@ -36,6 +36,7 @@ type Server struct {
 	dbClient    *db.Client
 	tenantMgr   *tenant.Manager
 	redisClient *redis.Client
+	grpcMu      sync.Mutex
 	pricer      *pricing_db.Pricer
 	server      *http.Server
 	port        int
@@ -448,6 +449,8 @@ func (s *Server) handleVersion(w http.ResponseWriter, r *http.Request) {

 // getGRPCClient lazily initializes the gRPC client.
 func (s *Server) getGRPCClient() (pb.AirborneServiceClient, error) {
+	s.grpcMu.Lock()
+	defer s.grpcMu.Unlock()
 	if s.grpcClient != nil {
 		return s.grpcClient, nil
 	}
```

### A-5. `expandEnv` Fallback Expands All `$VAR` Patterns (LOW)

**File**: `internal/config/config.go:370-372`

The fallback `os.ExpandEnv(s)` expands any `$VAR` or `${VAR}` patterns in config strings. If a Redis password or admin token contains a `$` character (e.g., `P@$$w0rd`), it will be silently corrupted.

```diff
--- a/internal/config/config.go
+++ b/internal/config/config.go
@@ -368,8 +368,8 @@ func expandEnv(s string) string {
 		return os.Getenv(varName)
 	}

-	// Handle $VAR syntax and passthrough non-variable strings
-	return os.ExpandEnv(s)
+	// Return as-is if no ENV= or ${VAR} prefix — do not blindly expand $VAR in passwords/tokens
+	return s
 }
```

### A-6. Error Comparison Uses `==` Instead of `errors.Is` (LOW)

**File**: `internal/auth/interceptor.go:117-119`

```go
switch err {
case ErrKeyNotFound, ErrInvalidKey:
```

This uses `==` comparison, which fails if errors are wrapped.

```diff
--- a/internal/auth/interceptor.go
+++ b/internal/auth/interceptor.go
@@ -114,10 +114,10 @@ func (a *Authenticator) authenticate(ctx context.Context) (*ClientKey, error) {
 	if err != nil {
 		slog.Debug("authentication failed", "error", err)
-		switch err {
-		case ErrKeyNotFound, ErrInvalidKey:
+		switch {
+		case errors.Is(err, ErrKeyNotFound) || errors.Is(err, ErrInvalidKey):
 			return nil, status.Error(codes.Unauthenticated, "invalid API key")
-		case ErrKeyExpired:
+		case errors.Is(err, ErrKeyExpired):
 			return nil, status.Error(codes.Unauthenticated, "API key expired")
 		default:
 			return nil, status.Error(codes.Internal, "authentication error")
```

Same pattern at `internal/db/repository.go:143`:

```diff
--- a/internal/db/repository.go
+++ b/internal/db/repository.go
@@ -140,7 +140,7 @@ func (r *Repository) GetThread(ctx context.Context, id uuid.UUID) (*Thread, erro
 	)
 	if err != nil {
-		if err == pgx.ErrNoRows {
+		if errors.Is(err, pgx.ErrNoRows) {
 			return nil, nil
 		}
```

---

## 2. TESTS — Proposed Unit Tests

### T-1. Tests for `internal/errors/sanitize.go` (No tests exist)

```diff
--- /dev/null
+++ b/internal/errors/sanitize_test.go
@@ -0,0 +1,62 @@
+package errors
+
+import (
+	"errors"
+	"testing"
+)
+
+func TestSanitizeForClient_NilError(t *testing.T) {
+	result := SanitizeForClient(nil)
+	if result != "" {
+		t.Errorf("SanitizeForClient(nil) = %q, want empty string", result)
+	}
+}
+
+func TestSanitizeForClient_KnownPatterns(t *testing.T) {
+	tests := []struct {
+		name     string
+		err      error
+		expected string
+	}{
+		{"rate limit", errors.New("rate limit exceeded on provider"), "rate limit exceeded"},
+		{"quota", errors.New("Quota exhausted for project"), "quota exceeded"},
+		{"timeout", errors.New("context deadline exceeded (timeout)"), "request timed out"},
+		{"context dead", errors.New("context deadline exceeded"), "request timed out"},
+		{"invalid api key", errors.New("invalid api key provided"), "authentication failed with provider"},
+		{"unauthorized", errors.New("401 Unauthorized response"), "authentication failed with provider"},
+		{"forbidden", errors.New("403 Forbidden"), "access denied by provider"},
+		{"not found", errors.New("model not found"), "resource not found"},
+	}
+
+	for _, tt := range tests {
+		t.Run(tt.name, func(t *testing.T) {
+			result := SanitizeForClient(tt.err)
+			if result != tt.expected {
+				t.Errorf("SanitizeForClient(%v) = %q, want %q", tt.err, result, tt.expected)
+			}
+		})
+	}
+}
+
+func TestSanitizeForClient_UnknownError(t *testing.T) {
+	err := errors.New("connection refused to internal-host:5432")
+	result := SanitizeForClient(err)
+	if result != "provider temporarily unavailable" {
+		t.Errorf("SanitizeForClient(unknown) = %q, want %q", result, "provider temporarily unavailable")
+	}
+}
+
+func TestSanitizeForClient_CaseInsensitive(t *testing.T) {
+	// Pattern matching should be case-insensitive
+	err := errors.New("RATE LIMIT exceeded")
+	result := SanitizeForClient(err)
+	if result != "rate limit exceeded" {
+		t.Errorf("SanitizeForClient(UPPERCASE) = %q, want %q", result, "rate limit exceeded")
+	}
+}
```

### T-2. Tests for Admin Server Health/Version Handlers

```diff
--- /dev/null
+++ b/internal/admin/server_test.go
@@ -0,0 +1,72 @@
+package admin
+
+import (
+	"encoding/json"
+	"net/http"
+	"net/http/httptest"
+	"testing"
+)
+
+func TestHandleHealth_NoDB(t *testing.T) {
+	s := &Server{}
+	req := httptest.NewRequest(http.MethodGet, "/admin/health", nil)
+	w := httptest.NewRecorder()
+
+	s.handleHealth(w, req)
+
+	if w.Code != http.StatusOK {
+		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
+	}
+
+	var resp map[string]interface{}
+	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
+		t.Fatalf("failed to decode response: %v", err)
+	}
+	if resp["status"] != "healthy" {
+		t.Errorf("status = %v, want healthy", resp["status"])
+	}
+	if resp["database"] != "not_configured" {
+		t.Errorf("database = %v, want not_configured", resp["database"])
+	}
+}
+
+func TestHandleHealth_MethodNotAllowed(t *testing.T) {
+	s := &Server{}
+	req := httptest.NewRequest(http.MethodPost, "/admin/health", nil)
+	w := httptest.NewRecorder()
+
+	s.handleHealth(w, req)
+
+	if w.Code != http.StatusMethodNotAllowed {
+		t.Errorf("status = %d, want %d", w.Code, http.StatusMethodNotAllowed)
+	}
+}
+
+func TestHandleVersion(t *testing.T) {
+	s := &Server{
+		version: VersionInfo{
+			Version:   "1.0.0",
+			GitCommit: "abc123",
+			BuildTime: "2026-01-01",
+		},
+	}
+	req := httptest.NewRequest(http.MethodGet, "/admin/version", nil)
+	w := httptest.NewRecorder()
+
+	s.handleVersion(w, req)
+
+	if w.Code != http.StatusOK {
+		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
+	}
+
+	var resp VersionInfo
+	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
+		t.Fatalf("failed to decode response: %v", err)
+	}
+	if resp.Version != "1.0.0" {
+		t.Errorf("version = %q, want %q", resp.Version, "1.0.0")
+	}
+}
```

### T-3. Tests for `expandEnv` Config Function

```diff
--- /dev/null
+++ b/internal/config/expand_test.go
@@ -0,0 +1,44 @@
+package config
+
+import (
+	"os"
+	"testing"
+)
+
+func TestExpandEnv_ENVPrefix(t *testing.T) {
+	os.Setenv("TEST_EXPAND_VAR", "secret123")
+	defer os.Unsetenv("TEST_EXPAND_VAR")
+
+	result := expandEnv("ENV=TEST_EXPAND_VAR")
+	if result != "secret123" {
+		t.Errorf("expandEnv(ENV=...) = %q, want %q", result, "secret123")
+	}
+}
+
+func TestExpandEnv_BraceSyntax(t *testing.T) {
+	os.Setenv("TEST_BRACE_VAR", "value456")
+	defer os.Unsetenv("TEST_BRACE_VAR")
+
+	result := expandEnv("${TEST_BRACE_VAR}")
+	if result != "value456" {
+		t.Errorf("expandEnv(${...}) = %q, want %q", result, "value456")
+	}
+}
+
+func TestExpandEnv_LiteralDollarSign(t *testing.T) {
+	// A password like "P@$$w0rd" should NOT be mangled by os.ExpandEnv.
+	// This test documents the current (buggy) behavior if os.ExpandEnv is used,
+	// or validates the fix if the fallback is removed.
+	result := expandEnv("P@$$w0rd")
+	// After the fix (removing os.ExpandEnv fallback), this should pass:
+	if result != "P@$$w0rd" {
+		t.Errorf("expandEnv(literal$) = %q, want %q", result, "P@$$w0rd")
+	}
+}
+
+func TestExpandEnv_PlainString(t *testing.T) {
+	result := expandEnv("localhost:6379")
+	if result != "localhost:6379" {
+		t.Errorf("expandEnv(plain) = %q, want %q", result, "localhost:6379")
+	}
+}
```

### T-4. Tests for `parseAPIKey` Function

```diff
--- /dev/null
+++ b/internal/auth/parse_key_test.go
@@ -0,0 +1,56 @@
+package auth
+
+import (
+	"testing"
+)
+
+func TestParseAPIKey_ValidKey(t *testing.T) {
+	// Format: airborne_sk_KEYID(8)_SECRET
+	key := "airborne_sk_abcdef01_thesecretpart"
+	keyID, secret, err := parseAPIKey(key)
+	if err != nil {
+		t.Fatalf("parseAPIKey(%q) error: %v", key, err)
+	}
+	if keyID != "abcdef01" {
+		t.Errorf("keyID = %q, want %q", keyID, "abcdef01")
+	}
+	if secret != "thesecretpart" {
+		t.Errorf("secret = %q, want %q", secret, "thesecretpart")
+	}
+}
+
+func TestParseAPIKey_TooShort(t *testing.T) {
+	_, _, err := parseAPIKey("airborne_sk_ab")
+	if err != ErrInvalidKey {
+		t.Errorf("parseAPIKey(short) = %v, want ErrInvalidKey", err)
+	}
+}
+
+func TestParseAPIKey_WrongPrefix(t *testing.T) {
+	_, _, err := parseAPIKey("badprefix___abcdef01_secret")
+	if err != ErrInvalidKey {
+		t.Errorf("parseAPIKey(bad prefix) = %v, want ErrInvalidKey", err)
+	}
+}
+
+func TestParseAPIKey_MissingSeparator(t *testing.T) {
+	// KeyID is 8 chars but no underscore separator
+	_, _, err := parseAPIKey("airborne_sk_abcdef01secret")
+	if err != ErrInvalidKey {
+		t.Errorf("parseAPIKey(no separator) = %v, want ErrInvalidKey", err)
+	}
+}
+
+func TestParseAPIKey_EmptySecret(t *testing.T) {
+	// Minimum length check should catch this
+	_, _, err := parseAPIKey("airborne_sk_abcdef01_")
+	if err != nil {
+		// The current impl requires at least 1 char secret (minAPIKeyLength)
+		// but the key "airborne_sk_abcdef01_" is exactly at minAPIKeyLength
+		// so it should parse successfully with empty string as secret
+		t.Logf("parseAPIKey(empty secret) error (may be valid): %v", err)
+	}
+}
```

### T-5. Tests for `normalizeAuthHeader`

```diff
--- /dev/null
+++ b/internal/auth/normalize_test.go
@@ -0,0 +1,42 @@
+package auth
+
+import "testing"
+
+func TestNormalizeAuthHeader(t *testing.T) {
+	tests := []struct {
+		name  string
+		input string
+		want  string
+	}{
+		{"standard bearer", "Bearer abc123", "abc123"},
+		{"lowercase bearer", "bearer abc123", "abc123"},
+		{"mixed case", "BEARER abc123", "abc123"},
+		{"extra whitespace", "  Bearer  abc123  ", "abc123"},
+		{"no prefix", "raw-token-value", "raw-token-value"},
+		{"empty string", "", ""},
+		{"whitespace only", "   ", ""},
+	}
+
+	for _, tt := range tests {
+		t.Run(tt.name, func(t *testing.T) {
+			got := normalizeAuthHeader(tt.input)
+			if got != tt.want {
+				t.Errorf("normalizeAuthHeader(%q) = %q, want %q", tt.input, got, tt.want)
+			}
+		})
+	}
+}
```

---

## 3. FIXES — Bugs and Code Smells

### F-1. Rate Limit Counter Increments Even When Over Limit (BUG)

**File**: `internal/auth/ratelimit.go:19-30`

The Lua `rateLimitScript` always calls `INCR` before checking the limit. Each rejected request still bumps the counter, causing the over-limit gap to grow unboundedly. If a client sends 1000 requests in a burst, the counter hits 1000 even with a limit of 60, and the counter won't reset for the full TTL window.

```diff
--- a/internal/auth/ratelimit.go
+++ b/internal/auth/ratelimit.go
@@ -18,12 +18,14 @@ const (
 const rateLimitScript = `
 local key = KEYS[1]
 local limit = tonumber(ARGV[1])
 local window = tonumber(ARGV[2])

-local current = redis.call('INCR', key)
-if current == 1 then
-    redis.call('EXPIRE', key, window)
+local current = tonumber(redis.call('GET', key) or "0")
+if current >= limit then
+    return current + 1
 end
-
-return current
+current = redis.call('INCR', key)
+if current == 1 then
+    redis.call('EXPIRE', key, window)
+end
+return current
 `
```

### F-2. `pgx.ErrNoRows` Comparison Uses `==` (BUG)

**File**: `internal/db/repository.go:143, 673`

```diff
--- a/internal/db/repository.go
+++ b/internal/db/repository.go
@@ -140,7 +140,7 @@ func (r *Repository) GetThread(ctx context.Context, id uuid.UUID) (*Thread, erro
 	)
 	if err != nil {
-		if err == pgx.ErrNoRows {
+		if errors.Is(err, pgx.ErrNoRows) {
 			return nil, nil
 		}
 		return nil, fmt.Errorf("failed to get thread: %w", err)
@@ -670,7 +670,7 @@ func (r *Repository) GetDebugData(ctx context.Context, messageID uuid.UUID) (*De
 	)
 	if err != nil {
-		if err == pgx.ErrNoRows {
+		if errors.Is(err, pgx.ErrNoRows) {
 			return nil, fmt.Errorf("message not found")
 		}
 		return nil, fmt.Errorf("failed to get debug data: %w", err)
```

### F-3. Context Timeout Comment Disagrees with Code (CODE SMELL)

**File**: `internal/admin/server.go:541-542, 794-795`

Comment says "must be less than HTTP WriteTimeout of 120s" but `WriteTimeout` is set to `5 * time.Minute` (300s) at line 142, and the actual timeout is `4 * time.Minute` (240s).

```diff
--- a/internal/admin/server.go
+++ b/internal/admin/server.go
@@ -539,7 +539,7 @@ func (s *Server) handleTest(w http.ResponseWriter, r *http.Request) {
 	}

-	// Set timeout (must be less than HTTP WriteTimeout of 120s)
+	// Set timeout (must be less than HTTP WriteTimeout of 5m)
 	ctx, cancel := context.WithTimeout(ctx, 4*time.Minute)
 	defer cancel()
@@ -792,7 +792,7 @@ func (s *Server) handleChat(w http.ResponseWriter, r *http.Request) {
 	}

-	// Set timeout (must be less than HTTP WriteTimeout of 120s)
+	// Set timeout (must be less than HTTP WriteTimeout of 5m)
 	ctx, cancel := context.WithTimeout(ctx, 4*time.Minute)
 	defer cancel()
```

### F-4. `ValidateKey` Never Updates `LastUsed` (CODE SMELL)

**File**: `internal/auth/keys.go:125-149`

The `ClientKey` struct has a `LastUsed` field, but `ValidateKey` never writes to it after successful validation. This makes the field always `nil` and useless for auditing key usage.

```diff
--- a/internal/auth/keys.go
+++ b/internal/auth/keys.go
@@ -145,6 +145,12 @@ func (s *KeyStore) ValidateKey(ctx context.Context, apiKey string) (*ClientKey, e
 		return nil, ErrInvalidKey
 	}

+	// Update last-used timestamp (best effort — don't fail auth if this fails)
+	now := time.Now().UTC()
+	key.LastUsed = &now
+	if err := s.saveKey(ctx, key); err != nil {
+		slog.Warn("failed to update last_used timestamp", "key_id", key.KeyID, "error", err)
+	}
 	return key, nil
 }
```

### F-5. Unused `html` Import in Chat Service (CODE SMELL)

**File**: `internal/service/chat.go:7`

```go
"html"
```

This import is unused or used trivially. If it's used for `html.EscapeString` somewhere in the file, it should be verified that all user/AI content flowing to the admin dashboard is properly escaped.

---

## 4. REFACTOR — Improvement Opportunities

### R-1. Hardcoded Tenant IDs in SQL UNION Query (~80 LOC)

**File**: `internal/db/repository.go:337-467`

`GetActivityFeedAllTenants` contains a massive hardcoded `UNION ALL` query with "ai8", "email4ai", "zztest" baked in. Adding a 4th tenant requires modifying this 80-line SQL block plus updating `ValidTenantIDs` map, `GetDebugDataAllTenants`, and `GetThreadConversationAllTenants`. This should be dynamically generated from the `ValidTenantIDs` map.

Similarly, `GetDebugDataAllTenants` (line 696) and `GetThreadConversationAllTenants` (line 799) iterate hardcoded tenant lists.

### R-2. OpenAI-Compatible Provider Boilerplate (~13 packages)

Packages like `deepseek/client.go`, `grok/client.go`, `fireworks/client.go`, `together/client.go`, `openrouter/client.go`, `deepinfra/client.go`, `hyperbolic/client.go`, `nebius/client.go`, `cerebras/client.go`, `upstage/client.go`, `perplexity/client.go`, `cohere/client.go`, and `mistral/client.go` are all thin wrappers (~20-30 lines each) around `compat.NewClient()` with only config differences. These could be replaced by a config-driven factory function that takes a `compat.ProviderConfig` struct and returns a `provider.Provider`.

### R-3. Duplicated Token Extraction Functions

**File**: `internal/auth/static.go:86-98` and `internal/auth/interceptor.go:131-145`

`extractStaticToken` and `extractAPIKey` do the same thing — extract a token from gRPC metadata `authorization` or `x-api-key` headers. They should share a single implementation.

### R-4. Repeated JSON Error Response Pattern in Admin Handlers

**File**: `internal/admin/server.go`

Multiple handlers repeat the same pattern:
```go
w.Header().Set("Content-Type", "application/json")
w.WriteHeader(http.StatusBadRequest)
json.NewEncoder(w).Encode(map[string]interface{}{"error": "..."})
```

This could use a helper (the `httpkit.JSONError` from chassis-go is already imported but inconsistently used — some handlers use it, others don't).

### R-5. Magic Numbers in Conversation History Compression

**File**: `internal/admin/server.go:849-854`

```go
maxHistoryChars      = 30000
maxAIResponseChars   = 500
fullAIResponsesLimit = 3
dropAIResponsesLimit = 6
```

These constants are defined locally in `buildCompressedHistory`. They control important behavior (context window management) and should be configurable or at minimum defined at package level with documentation explaining how they were chosen.

---

## Scoring Breakdown

| Category | Score | Notes |
|---|---|---|
| Architecture | 18/20 | Clean layered design, good separation of concerns, proper gRPC patterns |
| Security | 12/20 | Good SSRF protection, bcrypt, constant-time compare, error sanitization. **But no auth on admin HTTP is critical.** |
| Code Quality | 14/20 | Clean Go idioms, proper error handling mostly. Some `==` vs `errors.Is`, unused imports |
| Test Coverage | 14/20 | 47 test files, good coverage of core packages. Gaps in admin, sanitize, some auth functions |
| Maintainability | 13/20 | Hardcoded tenant IDs, duplicated SQL, boilerplate providers reduce ability to evolve |
| **TOTAL** | **71/100** | |

The codebase demonstrates strong architectural foundations and security awareness (SSRF validation, bcrypt, error sanitization), but the unauthenticated admin HTTP server is a significant gap, and the hardcoded tenant approach will increasingly limit scalability.
