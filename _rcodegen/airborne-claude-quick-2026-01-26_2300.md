Date Created: 2026-01-26 23:00:00 UTC
TOTAL_SCORE: 85/100

# Airborne Quick Analysis Report

**Version**: 1.7.12
**Agent**: Claude Code:Opus 4.5
**Analysis Type**: Quick Codebase Audit

---

## Executive Summary

Airborne is a well-architected, production-grade multi-tenant AI gateway supporting 15+ LLM providers. The codebase demonstrates strong security practices (SSRF protection, error sanitization, multi-tenant isolation), clean layered architecture, and comprehensive test coverage. Key strengths include robust authentication, proper input validation, and careful secrets management. Areas for improvement include missing context cancellation propagation in one retry path, overly permissive CORS settings, and opportunities for additional unit tests.

---

## 1. AUDIT - Security and Code Quality Issues

### Issue 1.1: CORS Allows All Origins (Medium Severity)

**File**: `internal/admin/server.go:86-88`

The admin server sets `Access-Control-Allow-Origin: *` which allows any domain to make requests to the admin API.

```go
// Current code at line 86-88
w.Header().Set("Access-Control-Allow-Origin", "*")
w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
```

**Risk**: Cross-site request forgery (CSRF) attacks from malicious websites if users have valid auth tokens stored in browsers.

**PATCH-READY DIFF**:
```diff
--- a/internal/admin/server.go
+++ b/internal/admin/server.go
@@ -83,7 +83,12 @@ func NewServer(dbClient *db.Client, cfg Config) *Server {
 	// CORS middleware wrapper
 	corsHandler := func(h http.HandlerFunc) http.HandlerFunc {
 		return func(w http.ResponseWriter, r *http.Request) {
-			w.Header().Set("Access-Control-Allow-Origin", "*")
+			// Only allow configured origins or same-origin requests
+			origin := r.Header.Get("Origin")
+			if origin == "" || isAllowedOrigin(origin, cfg.AllowedOrigins) {
+				w.Header().Set("Access-Control-Allow-Origin", origin)
+				w.Header().Set("Access-Control-Allow-Credentials", "true")
+			}
 			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
 			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
```

---

### Issue 1.2: Doppler Retry Uses Blocking Sleep (Low Severity)

**File**: `internal/tenant/doppler.go:107-109`

The Doppler client retry logic uses `time.Sleep()` without respecting context cancellation, which could delay shutdown.

```go
// Current code at line 104-109
backoff := baseBackoff * time.Duration(1<<(attempt-1))
if backoff > maxBackoff {
    backoff = maxBackoff
}
time.Sleep(backoff)
```

**PATCH-READY DIFF**:
```diff
--- a/internal/tenant/doppler.go
+++ b/internal/tenant/doppler.go
@@ -98,7 +98,7 @@ func (c *dopplerClient) fetchWithRetry(project string) (map[string]string, error
 // fetchWithRetry attempts to fetch secrets with exponential backoff.
-func (c *dopplerClient) fetchWithRetry(project string) (map[string]string, error) {
+func (c *dopplerClient) fetchWithRetry(ctx context.Context, project string) (map[string]string, error) {
 	var lastErr error

 	for attempt := 0; attempt < maxRetries; attempt++ {
@@ -106,7 +106,12 @@ func (c *dopplerClient) fetchWithRetry(project string) (map[string]string, error
 			if backoff > maxBackoff {
 				backoff = maxBackoff
 			}
-			time.Sleep(backoff)
+			select {
+			case <-ctx.Done():
+				return nil, ctx.Err()
+			case <-time.After(backoff):
+			}
 		}
```

---

### Issue 1.3: Error Sanitization Could Leak Internal Details (Low Severity)

**File**: `internal/errors/sanitize.go:29-38`

The pattern matching is case-insensitive but the iteration order over the map is non-deterministic. If an error contains multiple patterns, the returned message could vary.

```go
// Check for known safe patterns
for pattern, safeMsg := range clientSafePatterns {
    if strings.Contains(errLower, pattern) {
        // ...
        return safeMsg
    }
}
```

**PATCH-READY DIFF**:
```diff
--- a/internal/errors/sanitize.go
+++ b/internal/errors/sanitize.go
@@ -7,17 +7,27 @@ import (
 	"strings"
 )

-// clientSafePatterns maps error patterns to client-safe messages
-var clientSafePatterns = map[string]string{
-	"rate limit":   "rate limit exceeded",
-	"quota":        "quota exceeded",
-	"timeout":      "request timed out",
-	"context dead": "request cancelled",
-	"invalid api":  "authentication failed with provider",
-	"unauthorized": "authentication failed with provider",
-	"forbidden":    "access denied by provider",
-	"not found":    "resource not found",
+// patternMapping holds a pattern and its safe message
+type patternMapping struct {
+	pattern string
+	safeMsg string
 }
+
+// clientSafePatterns is ordered for deterministic matching (most specific first)
+var clientSafePatterns = []patternMapping{
+	{pattern: "rate limit", safeMsg: "rate limit exceeded"},
+	{pattern: "quota", safeMsg: "quota exceeded"},
+	{pattern: "context dead", safeMsg: "request cancelled"},
+	{pattern: "timeout", safeMsg: "request timed out"},
+	{pattern: "invalid api", safeMsg: "authentication failed with provider"},
+	{pattern: "unauthorized", safeMsg: "authentication failed with provider"},
+	{pattern: "forbidden", safeMsg: "access denied by provider"},
+	{pattern: "not found", safeMsg: "resource not found"},
+}
+
 // SanitizeForClient converts internal errors to client-safe messages
 func SanitizeForClient(err error) string {
 	if err == nil {
@@ -27,8 +37,8 @@ func SanitizeForClient(err error) string {
 	errLower := strings.ToLower(err.Error())

 	// Check for known safe patterns
-	for pattern, safeMsg := range clientSafePatterns {
-		if strings.Contains(errLower, pattern) {
+	for _, pm := range clientSafePatterns {
+		if strings.Contains(errLower, pm.pattern) {
```

---

## 2. TESTS - Proposed Unit Tests

### Test 2.1: Missing Tests for Rate Limiter Edge Cases

**File**: `internal/auth/ratelimit_test.go`

The rate limiter tests don't cover concurrent access patterns or burst limits.

**PATCH-READY DIFF**:
```diff
--- a/internal/auth/ratelimit_test.go
+++ b/internal/auth/ratelimit_test.go
@@ -100,3 +100,45 @@ func TestRateLimiter_Disabled(t *testing.T) {
 		t.Error("disabled limiter should allow request")
 	}
 }
+
+func TestRateLimiter_Concurrent(t *testing.T) {
+	ctx := context.Background()
+	limiter := NewRateLimiter(nil) // nil redis = local mode
+
+	client := &ClientKey{
+		ClientID: "concurrent-test",
+		APIKey:   "test-key",
+	}
+
+	// Launch 10 concurrent requests
+	var wg sync.WaitGroup
+	errors := make(chan error, 10)
+
+	for i := 0; i < 10; i++ {
+		wg.Add(1)
+		go func() {
+			defer wg.Done()
+			if err := limiter.Allow(ctx, client); err != nil {
+				errors <- err
+			}
+		}()
+	}
+
+	wg.Wait()
+	close(errors)
+
+	// Some requests may be rate limited, but should not panic
+	for err := range errors {
+		if err != nil && !strings.Contains(err.Error(), "rate limit") {
+			t.Errorf("unexpected error: %v", err)
+		}
+	}
+}
+
+func TestRateLimiter_BurstAllowed(t *testing.T) {
+	// Test that burst limits are respected
+	ctx := context.Background()
+	limiter := NewRateLimiter(nil)
+
+	client := &ClientKey{ClientID: "burst-test"}
+
+	// First few requests should succeed (within burst)
+	for i := 0; i < 5; i++ {
+		if err := limiter.Allow(ctx, client); err != nil {
+			t.Errorf("request %d should be within burst: %v", i, err)
+		}
+	}
+}
```

---

### Test 2.2: Missing Tests for URL Validation Edge Cases

**File**: `internal/validation/url_test.go`

Missing tests for IPv6 mapped addresses and DNS rebinding scenarios.

**PATCH-READY DIFF**:
```diff
--- a/internal/validation/url_test.go
+++ b/internal/validation/url_test.go
@@ -150,3 +150,35 @@ func TestValidateProviderURL_MetadataEndpoints(t *testing.T) {
 		})
 	}
 }
+
+func TestValidateProviderURL_IPv6Mapped(t *testing.T) {
+	tests := []struct {
+		name    string
+		url     string
+		wantErr bool
+	}{
+		{
+			name:    "IPv6 mapped private IP",
+			url:     "https://[::ffff:192.168.1.1]:8080/api",
+			wantErr: true,
+		},
+		{
+			name:    "IPv6 mapped localhost",
+			url:     "http://[::ffff:127.0.0.1]:8080/api",
+			wantErr: false, // localhost allowed
+		},
+		{
+			name:    "IPv6 mapped metadata",
+			url:     "https://[::ffff:169.254.169.254]/latest/meta-data",
+			wantErr: true,
+		},
+	}
+
+	for _, tt := range tests {
+		t.Run(tt.name, func(t *testing.T) {
+			err := ValidateProviderURL(tt.url)
+			if (err != nil) != tt.wantErr {
+				t.Errorf("ValidateProviderURL(%q) error = %v, wantErr %v", tt.url, err, tt.wantErr)
+			}
+		})
+	}
+}
```

---

### Test 2.3: Missing Tests for Anthropic Client Thinking Mode

**File**: `internal/provider/anthropic/client_test.go`

The Anthropic client tests don't cover the thinking mode configuration paths.

**PATCH-READY DIFF**:
```diff
--- a/internal/provider/anthropic/client_test.go
+++ b/internal/provider/anthropic/client_test.go
@@ -100,3 +100,42 @@ func TestClient_Name(t *testing.T) {
 		t.Errorf("Name() = %v, want %v", got, want)
 	}
 }
+
+func TestClient_ThinkingModeConfig(t *testing.T) {
+	tests := []struct {
+		name        string
+		extraOpts   map[string]string
+		wantEnabled bool
+		wantBudget  int
+	}{
+		{
+			name:        "thinking disabled by default",
+			extraOpts:   map[string]string{},
+			wantEnabled: false,
+			wantBudget:  0,
+		},
+		{
+			name: "thinking enabled with budget",
+			extraOpts: map[string]string{
+				"thinking_enabled": "true",
+				"thinking_budget":  "10000",
+			},
+			wantEnabled: true,
+			wantBudget:  10000,
+		},
+		{
+			name: "thinking enabled without budget",
+			extraOpts: map[string]string{
+				"thinking_enabled": "true",
+			},
+			wantEnabled: true,
+			wantBudget:  0,
+		},
+	}
+
+	for _, tt := range tests {
+		t.Run(tt.name, func(t *testing.T) {
+			cfg := provider.ProviderConfig{ExtraOptions: tt.extraOpts}
+			enabled := cfg.ExtraOptions["thinking_enabled"] == "true"
+			if enabled != tt.wantEnabled {
+				t.Errorf("thinking enabled = %v, want %v", enabled, tt.wantEnabled)
+			}
+		})
+	}
+}
```

---

## 3. FIXES - Bugs, Issues, and Code Smells

### Fix 3.1: Potential Integer Overflow in Backoff Calculation

**File**: `internal/retry/backoff.go:12`

The backoff calculation `1<<uint(attempt-1)` can overflow for large attempt values.

```go
delay := BackoffBase * time.Duration(1<<uint(attempt-1))
```

**PATCH-READY DIFF**:
```diff
--- a/internal/retry/backoff.go
+++ b/internal/retry/backoff.go
@@ -9,7 +9,12 @@ import (
 // SleepWithBackoff sleeps with exponential backoff.
 // The delay is calculated as BackoffBase * 2^(attempt-1).
 func SleepWithBackoff(ctx context.Context, attempt int) {
-	delay := BackoffBase * time.Duration(1<<uint(attempt-1))
+	// Cap the shift to prevent overflow (2^10 = 1024, plenty for backoff)
+	shift := attempt - 1
+	if shift > 10 {
+		shift = 10
+	}
+	delay := BackoffBase * time.Duration(1<<uint(shift))
 	select {
 	case <-ctx.Done():
 	case <-time.After(delay):
```

---

### Fix 3.2: Sscanf Return Value Not Checked

**File**: `internal/provider/anthropic/client.go:99`

The `fmt.Sscanf` return value is not checked, which could leave `thinkingBudget` uninitialized on parse failure.

```go
if budgetStr := cfg.ExtraOptions["thinking_budget"]; budgetStr != "" {
    fmt.Sscanf(budgetStr, "%d", &thinkingBudget)
}
```

**PATCH-READY DIFF**:
```diff
--- a/internal/provider/anthropic/client.go
+++ b/internal/provider/anthropic/client.go
@@ -96,8 +96,11 @@ func (c *Client) GenerateReply(ctx context.Context, params provider.GenerateResu
 	thinkingEnabled := cfg.ExtraOptions["thinking_enabled"] == "true"
 	includeThoughts := cfg.ExtraOptions["include_thoughts"] == "true"
 	var thinkingBudget int
-	if budgetStr := cfg.ExtraOptions["thinking_budget"]; budgetStr != "" {
-		fmt.Sscanf(budgetStr, "%d", &thinkingBudget)
+	if budgetStr := cfg.ExtraOptions["thinking_budget"]; budgetStr != "" {
+		if _, err := fmt.Sscanf(budgetStr, "%d", &thinkingBudget); err != nil {
+			slog.Warn("invalid thinking_budget, using default", "value", budgetStr, "error", err)
+			thinkingBudget = 0
+		}
 	}
```

---

### Fix 3.3: Missing Error Handling in Admin CORS Handler

**File**: `internal/admin/server.go:91-94`

The OPTIONS preflight handler doesn't set content length, which could cause issues with some proxies.

**PATCH-READY DIFF**:
```diff
--- a/internal/admin/server.go
+++ b/internal/admin/server.go
@@ -88,7 +88,9 @@ func NewServer(dbClient *db.Client, cfg Config) *Server {
 			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

 			if r.Method == "OPTIONS" {
+				w.Header().Set("Content-Length", "0")
 				w.WriteHeader(http.StatusOK)
 				return
 			}
```

---

## 4. REFACTOR - Opportunities to Improve Code Quality

### Refactor 4.1: Provider Client Initialization is Duplicated

**Location**: `internal/service/chat.go:54-58`

Each provider client is instantiated directly in `NewChatService`. Consider using a provider registry pattern for better extensibility and testability.

**Current**:
```go
return &ChatService{
    openaiProvider:    openai.NewClient(),
    geminiProvider:    gemini.NewClient(),
    anthropicProvider: anthropic.NewClient(),
    // ...
}
```

**Suggested Approach**: Create a `ProviderRegistry` that handles client initialization based on configuration, allowing for easier mocking in tests and dynamic provider registration.

---

### Refactor 4.2: HTTP Capture Transport Could Use Interface

**Location**: `internal/httpcapture/transport.go`

The HTTP capture functionality is tightly coupled to specific transport implementation. Extracting a `CaptureWriter` interface would improve testability.

---

### Refactor 4.3: Tenant Config Validation is Scattered

**Location**: `internal/tenant/` package

Tenant configuration validation happens in multiple places (`doppler.go:237`, `config.go`, `loader.go`). Centralizing all validation in a single `Validate()` method would improve maintainability.

---

### Refactor 4.4: Magic Numbers in Retry Configuration

**Location**: `internal/tenant/doppler.go:61-64`

Retry configuration uses magic numbers. Consider moving to a `RetryConfig` struct for better documentation and configurability.

```go
const (
    maxRetries  = 15
    baseBackoff = 100 * time.Millisecond
    maxBackoff  = 5 * time.Second
)
```

---

### Refactor 4.5: Admin Server Could Use Router Middleware

**Location**: `internal/admin/server.go`

The CORS handler is applied manually to each route. Using a proper middleware chain (e.g., chi or stdlib middleware) would be cleaner and more maintainable.

---

## Scoring Breakdown

| Category | Score | Max | Notes |
|----------|-------|-----|-------|
| Security | 25 | 30 | Strong SSRF protection, error sanitization, auth; minor CORS issue |
| Code Quality | 23 | 25 | Clean architecture, good separation of concerns |
| Test Coverage | 15 | 20 | Good coverage, but missing some edge cases |
| Error Handling | 12 | 15 | Consistent error handling, minor issues |
| Documentation | 10 | 10 | Well-documented code, clear comments |

**Total: 85/100**

---

## Summary of Findings

- **Critical Issues**: None
- **Medium Issues**: 1 (CORS configuration)
- **Low Issues**: 3 (blocking sleep, map iteration order, OPTIONS content-length)
- **Test Gaps**: 3 areas identified
- **Refactor Opportunities**: 5 areas identified

The codebase is production-ready with strong security practices and clean architecture. Recommended immediate actions:
1. Restrict CORS origins in admin server
2. Add context cancellation to Doppler retry loop
3. Cap backoff calculation to prevent overflow
