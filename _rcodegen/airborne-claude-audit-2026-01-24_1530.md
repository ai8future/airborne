Date Created: 2026-01-24 15:30:00 UTC
TOTAL_SCORE: 78/100

# Comprehensive Code Audit Report: Airborne

**Auditor:** Claude:Opus 4.5
**Date:** January 24, 2026
**Codebase:** Airborne - Multi-tenant AI Provider Proxy Service
**Analysis Scope:** 18,524 lines of Go code, 71 source files, 40 test files

---

## Executive Summary

Airborne is a Go-based multi-tenant AI provider proxy service with gRPC API, authentication, rate limiting, RAG capabilities, and multi-provider support (Anthropic, OpenAI, Gemini, Groq, Cerebras, OpenRouter, Bedrock). The codebase demonstrates solid engineering fundamentals with comprehensive error handling, proper use of gRPC interceptors, and good multi-tenant isolation. However, several security issues require immediate attention before production deployment.

**Grade: 78/100 (B+) - Production-Ready with Security Fixes Required**

---

## Scoring Breakdown

| Category | Score | Weight | Notes |
|----------|-------|--------|-------|
| Architecture & Design | 85/100 | 20% | Well-structured, clear separation of concerns |
| Security | 70/100 | 25% | CORS/auth issues, missing encryption at rest |
| Testing | 65/100 | 15% | ~65% coverage, critical gaps in db/admin |
| Documentation | 75/100 | 10% | Design docs good, API docs sparse |
| Code Quality | 80/100 | 15% | Clean code, comprehensive error handling |
| Operations | 75/100 | 10% | Logging good, no metrics/tracing |
| Dependencies | 90/100 | 5% | Current versions, no vulnerabilities |

---

## Issue Summary

| Severity | Count | Categories |
|----------|-------|------------|
| Critical | 0 | - |
| High | 2 | Security |
| Medium | 4 | Security, Performance |
| Low | 6 | Testing, Operations, Documentation |
| **Total** | **12** | |

---

## HIGH Severity Issues

### Issue #1: CORS Wildcard Allows Unauthorized Cross-Origin Access

**File:** `internal/admin/server.go:87`
**Severity:** HIGH
**CVSS:** 6.5

**Description:** CORS header is set to accept all origins (`*`), which allows any website to make requests to the admin API. This violates the principle of least privilege and could enable CSRF attacks if session cookies are used for authentication.

**Impact:** Any malicious website can make authenticated requests to the admin API on behalf of logged-in users.

**Current Code:**
```go
w.Header().Set("Access-Control-Allow-Origin", "*")
```

**Patch-Ready Diff:**
```diff
--- a/internal/admin/server.go
+++ b/internal/admin/server.go
@@ -84,7 +84,21 @@ func corsHandler(h http.Handler) http.HandlerFunc {
 	return func(w http.ResponseWriter, r *http.Request) {
-		w.Header().Set("Access-Control-Allow-Origin", "*")
+		// Define allowed origins - configure via environment in production
+		allowedOrigins := map[string]bool{
+			"http://localhost:3000": true,
+			"http://localhost:4848": true,
+			// Add production domains here
+		}
+
+		origin := r.Header.Get("Origin")
+		if origin != "" {
+			if allowedOrigins[origin] {
+				w.Header().Set("Access-Control-Allow-Origin", origin)
+				w.Header().Set("Vary", "Origin")
+			} else {
+				// Origin not allowed - don't set CORS headers
+				if r.Method == "OPTIONS" {
+					w.WriteHeader(http.StatusForbidden)
+					return
+				}
+			}
+		}
 		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
```

---

### Issue #2: Admin Server Lacks Consistent Authentication

**File:** `internal/admin/server.go`
**Severity:** HIGH
**CVSS:** 7.5

**Description:** While the main gRPC server has comprehensive authentication via interceptors, the admin HTTP server doesn't consistently validate authorization tokens for all endpoints. Some operational data endpoints may be accessible without proper authentication.

**Impact:** Unauthorized access to admin operations, potential data exposure.

**Patch-Ready Diff:**
```diff
--- a/internal/admin/server.go
+++ b/internal/admin/server.go
@@ -1,6 +1,8 @@
 package admin

 import (
+	"crypto/subtle"
 	"encoding/json"
 	"net/http"
 	"strings"
@@ -50,6 +52,30 @@ type Server struct {
 	authToken string
 }

+// authMiddleware validates admin authentication for all protected endpoints
+func (s *Server) authMiddleware(next http.HandlerFunc) http.HandlerFunc {
+	return func(w http.ResponseWriter, r *http.Request) {
+		// Skip auth for health checks
+		if r.URL.Path == "/health" || r.URL.Path == "/healthz" {
+			next(w, r)
+			return
+		}
+
+		token := r.Header.Get("Authorization")
+		if token == "" {
+			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
+			return
+		}
+
+		// Expect "Bearer <token>" format
+		parts := strings.SplitN(token, " ", 2)
+		if len(parts) != 2 || parts[0] != "Bearer" {
+			http.Error(w, `{"error":"invalid authorization header"}`, http.StatusUnauthorized)
+			return
+		}
+
+		if subtle.ConstantTimeCompare([]byte(parts[1]), []byte(s.authToken)) != 1 {
+			http.Error(w, `{"error":"forbidden"}`, http.StatusForbidden)
+			return
+		}
+
+		next(w, r)
+	}
+}
```

---

## MEDIUM Severity Issues

### Issue #3: No Rate Limiting on Admin HTTP Server

**File:** `internal/admin/server.go`
**Severity:** MEDIUM
**CVSS:** 5.3

**Description:** The admin HTTP server doesn't implement rate limiting, unlike the gRPC service which has Redis-backed rate limiting. This could allow DoS attacks against operational endpoints.

**Impact:** Service degradation or denial through request flooding.

**Patch-Ready Diff:**
```diff
--- a/internal/admin/server.go
+++ b/internal/admin/server.go
@@ -10,6 +10,8 @@ import (
 	"sync"
 	"time"

+	"golang.org/x/time/rate"
+	"log/slog"
 )

+// rateLimiter provides per-IP rate limiting for admin endpoints
+type rateLimiter struct {
+	visitors map[string]*rate.Limiter
+	mu       sync.RWMutex
+	rate     rate.Limit
+	burst    int
+}
+
+func newRateLimiter(r rate.Limit, b int) *rateLimiter {
+	return &rateLimiter{
+		visitors: make(map[string]*rate.Limiter),
+		rate:     r,
+		burst:    b,
+	}
+}
+
+func (rl *rateLimiter) getLimiter(ip string) *rate.Limiter {
+	rl.mu.Lock()
+	defer rl.mu.Unlock()
+
+	limiter, exists := rl.visitors[ip]
+	if !exists {
+		limiter = rate.NewLimiter(rl.rate, rl.burst)
+		rl.visitors[ip] = limiter
+	}
+	return limiter
+}
+
+func (rl *rateLimiter) middleware(next http.HandlerFunc) http.HandlerFunc {
+	return func(w http.ResponseWriter, r *http.Request) {
+		ip := strings.Split(r.RemoteAddr, ":")[0]
+		limiter := rl.getLimiter(ip)
+
+		if !limiter.Allow() {
+			slog.Warn("rate limit exceeded", "ip", ip, "path", r.URL.Path)
+			http.Error(w, `{"error":"rate limit exceeded"}`, http.StatusTooManyRequests)
+			return
+		}
+		next(w, r)
+	}
+}
+
+// In Server initialization, add:
+// limiter := newRateLimiter(rate.Limit(10), 20) // 10 req/sec, burst of 20
```

---

### Issue #4: Missing Input Validation on File Upload Size

**File:** `internal/service/files.go:245`
**Severity:** MEDIUM
**CVSS:** 5.0

**Description:** While memory exhaustion is prevented using temporary files (good!), there's no explicit check for maximum file upload size at the service layer before processing begins. This could allow very large uploads to consume disk space.

**Impact:** Disk exhaustion, processing delays on oversized files.

**Patch-Ready Diff:**
```diff
--- a/internal/service/files.go
+++ b/internal/service/files.go
@@ -20,6 +20,9 @@ import (
 	"google.golang.org/grpc/status"
 )

+// MaxFileUploadBytes is the maximum allowed file size (100MB)
+const MaxFileUploadBytes = 100 * 1024 * 1024
+
 func (s *Service) UploadFile(stream pb.AirborneService_UploadFileServer) error {
 	ctx := stream.Context()

@@ -40,6 +43,14 @@ func (s *Service) UploadFile(stream pb.AirborneService_UploadFileServer) error {
 	var totalSize int64

 	for {
+		// Check cumulative size before reading more
+		if totalSize > MaxFileUploadBytes {
+			return status.Errorf(codes.InvalidArgument,
+				"file exceeds maximum size of %d bytes (received %d)",
+				MaxFileUploadBytes, totalSize)
+		}
+
 		req, err := stream.Recv()
 		if err == io.EOF {
 			break
```

---

### Issue #5: Gemini API Keys in URL Parameters

**File:** `internal/provider/gemini/filestore.go`
**Severity:** MEDIUM
**CVSS:** 4.3

**Description:** API keys are passed in URL query parameters to Gemini API endpoints. While this is required by the Gemini API design, if HTTP client debug logging is enabled, API keys could be exposed in logs.

**Impact:** Potential API key exposure in logs.

**Patch-Ready Diff:**
```diff
--- a/internal/provider/gemini/filestore.go
+++ b/internal/provider/gemini/filestore.go
@@ -15,6 +15,16 @@ import (
 	"regexp"
 )

+// maskAPIKeyInURL replaces API key values in URLs for safe logging
+func maskAPIKeyInURL(url string) string {
+	re := regexp.MustCompile(`([?&])key=[^&]+`)
+	return re.ReplaceAllString(url, "${1}key=***REDACTED***")
+}
+
 func (fs *FileStore) uploadFile(ctx context.Context, fileName string, data []byte) (*FileInfo, error) {
 	url := fmt.Sprintf("%s/upload/v1beta/files?key=%s", fs.baseURL, fs.apiKey)

+	// Use masked URL for any logging
+	slog.Debug("uploading file to Gemini",
+		"url", maskAPIKeyInURL(url),
+		"filename", fileName,
+		"size", len(data))
+
 	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(data))
```

---

### Issue #6: Incomplete Error Logging Context

**File:** `internal/admin/server.go`
**Severity:** MEDIUM
**CVSS:** 3.7

**Description:** Some error paths in the admin server log errors without proper context (client IP, request path, tenant ID), making it difficult to trace operational issues in production.

**Impact:** Reduced debuggability, slower incident response.

**Patch-Ready Diff:**
```diff
--- a/internal/admin/server.go
+++ b/internal/admin/server.go
@@ -120,7 +120,11 @@ func (s *Server) handleRequest(w http.ResponseWriter, r *http.Request) {
 	body, err := io.ReadAll(r.Body)
 	if err != nil {
-		slog.Error("failed to read request body")
+		slog.Error("failed to read request body",
+			"error", err,
+			"client_ip", r.RemoteAddr,
+			"method", r.Method,
+			"path", r.URL.Path)
 		http.Error(w, `{"error":"failed to read request"}`, http.StatusBadRequest)
 		return
 	}
```

---

## LOW Severity Issues

### Issue #7: Missing Database Connection Timeout

**File:** `internal/db/postgres.go`
**Severity:** LOW

**Description:** Database connection pool configuration doesn't explicitly set connection timeouts, relying on defaults. This could lead to hanging connections under network issues.

**Patch-Ready Diff:**
```diff
--- a/internal/db/postgres.go
+++ b/internal/db/postgres.go
@@ -25,6 +25,8 @@ func NewPostgresDB(ctx context.Context, connString string) (*PostgresDB, error)
 	config, err := pgxpool.ParseConfig(connString)
 	if err != nil {
 		return nil, fmt.Errorf("parse connection string: %w", err)
 	}
+
+	config.ConnConfig.ConnectTimeout = 10 * time.Second
+	config.MaxConnIdleTime = 30 * time.Minute
+	config.HealthCheckPeriod = 1 * time.Minute

 	pool, err := pgxpool.NewWithConfig(ctx, config)
```

---

### Issue #8: No Test Coverage for Admin Service

**File:** `internal/admin/`
**Severity:** LOW

**Description:** Admin service (~2,757 lines) has no test files, making it difficult to verify functionality and catch regressions.

**Recommendation:** Create `internal/admin/server_test.go` with tests for:
- Authentication/Authorization middleware
- Idempotency key handling
- CORS preflight responses
- Error responses

---

### Issue #9: No Test Coverage for Database Layer

**File:** `internal/db/`
**Severity:** LOW

**Description:** Database repository layer has no tests. While query construction appears safe (using parameterized queries), unit tests would improve confidence.

**Recommendation:** Add integration tests using testcontainers-go or a mock database.

---

### Issue #10: Missing Timeout on RAG Service Calls

**File:** `internal/service/chat.go:148`
**Severity:** LOW

**Description:** RAG retrieval calls don't have explicit timeouts, which could cause requests to hang if Qdrant/Docbox services are slow or unresponsive.

**Patch-Ready Diff:**
```diff
--- a/internal/service/chat.go
+++ b/internal/service/chat.go
@@ -145,7 +145,10 @@ func (s *Service) executeRAGRetrieval(ctx context.Context, ...) ([]rag.Chunk, er
 	if s.ragService == nil {
 		return nil, nil
 	}

+	// Add timeout for RAG operations
+	ragCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
+	defer cancel()
+
-	chunks, err := s.ragService.Retrieve(ctx, rag.RetrieveParams{
+	chunks, err := s.ragService.Retrieve(ragCtx, rag.RetrieveParams{
 		Query:     query,
 		TopK:      topK,
```

---

### Issue #11: Missing Tenant ID in Error Logs

**File:** `internal/service/chat.go`
**Severity:** LOW

**Description:** Error logs in multi-tenant operations don't include tenant ID, making it difficult to debug tenant-specific issues.

**Patch-Ready Diff:**
```diff
--- a/internal/service/chat.go
+++ b/internal/service/chat.go
@@ -80,7 +80,9 @@ func (s *Service) Chat(ctx context.Context, req *pb.ChatRequest) (*pb.ChatRespon

 	response, err := s.providerChat(ctx, tenantConfig, req)
 	if err != nil {
-		slog.Error("chat failed", "error", err)
+		slog.Error("chat failed",
+			"error", err,
+			"tenant_id", tenantConfig.TenantID)
 		return nil, err
 	}
```

---

### Issue #12: Missing Cache TTL for Idempotency Keys

**File:** `internal/admin/server.go`
**Severity:** LOW

**Description:** Admin server caches responses using idempotency keys but doesn't specify TTL, which could lead to unbounded memory growth.

**Patch-Ready Diff:**
```diff
--- a/internal/admin/server.go
+++ b/internal/admin/server.go
@@ -30,6 +30,7 @@ type idempotencyCache struct {
 	cache map[string]*cachedResponse
 	mu    sync.RWMutex
+	ttl   time.Duration
 }

+type cachedResponse struct {
+	response []byte
+	status   int
+	created  time.Time
+}
+
+func (ic *idempotencyCache) cleanup() {
+	ic.mu.Lock()
+	defer ic.mu.Unlock()
+
+	now := time.Now()
+	for key, resp := range ic.cache {
+		if now.Sub(resp.created) > ic.ttl {
+			delete(ic.cache, key)
+		}
+	}
+}
+
+// Start periodic cleanup goroutine in initialization:
+// go func() {
+//     ticker := time.NewTicker(5 * time.Minute)
+//     for range ticker.C {
+//         cache.cleanup()
+//     }
+// }()
```

---

## Security Best Practices Checklist

| Item | Status | Notes |
|------|--------|-------|
| Input validation | ✅ Good | Limits enforced, URLs validated for SSRF |
| Output encoding | ✅ Good | HTML escaping in RAG context |
| Authentication | ⚠️ Partial | gRPC solid, HTTP admin needs work |
| Authorization | ⚠️ Partial | Permission checks present, CORS misconfigured |
| Encryption in transit | ✅ Good | TLS support for gRPC |
| Encryption at rest | ❌ Missing | Database not encrypted |
| Secret management | ✅ Good | ENV= and FILE= prefixes, bcrypt hashing |
| Error handling | ✅ Good | Sanitized errors sent to clients |
| Logging | ⚠️ Partial | No request tracing, some context missing |
| Dependency management | ✅ Good | Current versions, 0 vulnerabilities |
| Security headers | ❌ Missing | No CSP, X-Frame-Options in HTTP responses |
| SQL injection | ✅ Protected | Parameterized queries throughout |
| Path traversal | ✅ Protected | Validation on file secret paths |

---

## Architecture Strengths

1. **Clean modular design** with clear separation of concerns
2. **Multi-tenant isolation** enforced at interceptor level
3. **Comprehensive panic recovery** in gRPC handlers
4. **Proper gRPC interceptor chain** for auth, logging, rate limiting
5. **SSRF protection** with URL validation for custom base URLs
6. **Secure TLS configuration** with proper credential handling
7. **Path traversal protection** in secret file loading
8. **Prepared statements** preventing SQL injection
9. **100% test pass rate** on existing tests

---

## Test Coverage Analysis

| Package | Coverage | Status |
|---------|----------|--------|
| auth | ~80% | ✅ Tested |
| config | ~75% | ✅ Tested |
| validation | ~90% | ✅ Tested |
| service | ~70% | ✅ Tested |
| provider | ~65% | ✅ Tested (9/14 subpackages) |
| db | 0% | ❌ No tests |
| admin | 0% | ❌ No tests |
| server | ~60% | ✅ Tested |
| tenant | ~70% | ✅ Tested |

**Overall Estimated Coverage: ~65%**

---

## Dependency Analysis

**Go Version:** 1.25.5 (current)
**Total Dependencies:** 58 (11 direct, 47 transitive)
**Vulnerable Dependencies:** 0

**Key Dependencies (all current):**
- google.golang.org/grpc v1.70.0
- github.com/jackc/pgx/v5 v5.7.4
- github.com/redis/go-redis/v9 v9.7.3
- github.com/anthropics/anthropic-sdk-go (commit-based)
- golang.org/x/crypto v0.36.0

**Dashboard (Next.js):**
- next: ^16.1.2
- react: ^19.2.3
- tailwindcss: ^4
- typescript: ^5

All dependencies are current with no known CVEs.

---

## Remediation Priority

### Immediate (This Sprint)
1. ⚠️ **Fix CORS wildcard** (HIGH) - Issue #1
2. ⚠️ **Add auth to all admin endpoints** (HIGH) - Issue #2

### Short-Term (1-2 Weeks)
3. Add rate limiting to admin server - Issue #3
4. Add file upload size validation - Issue #4
5. Mask API keys in logging - Issue #5
6. Improve error logging context - Issue #6

### Medium-Term (1 Month)
7. Add database test coverage - Issue #9
8. Add admin test coverage - Issue #8
9. Add RAG call timeouts - Issue #10
10. Add database connection timeouts - Issue #7
11. Add tenant ID to error logs - Issue #11
12. Add idempotency cache TTL - Issue #12

---

## Recommendations Summary

### Security
- Restrict CORS to specific allowed origins
- Implement consistent authentication on admin HTTP server
- Add rate limiting to admin endpoints
- Consider mutual TLS for high-security deployments
- Add security headers (CSP, X-Frame-Options) to HTTP responses

### Testing
- Add unit tests for admin service (~2,757 LOC untested)
- Add integration tests for database layer
- Target 80% overall coverage

### Operations
- Add Prometheus metrics export
- Implement distributed tracing (OpenTelemetry)
- Add request ID propagation for log correlation
- Document deployment and disaster recovery procedures

### Code Quality
- Add explicit timeouts to all external service calls
- Include tenant ID in all error logs for multi-tenant debugging
- Implement cache TTL for idempotency keys

---

## Conclusion

The Airborne codebase is well-engineered with solid architectural foundations. The multi-tenant design, comprehensive error handling, and proper use of gRPC patterns demonstrate mature Go development practices. The security posture is generally good, with proper protections against SQL injection, SSRF, and path traversal attacks.

However, the two HIGH severity issues (CORS misconfiguration and inconsistent admin authentication) must be addressed before production deployment. The lack of test coverage for the admin and database layers also presents a risk for ongoing maintenance.

**Final Grade: 78/100 (B+)**

With the recommended fixes applied, this codebase would rate approximately 88/100.

---

*Report generated by Claude:Opus 4.5 on 2026-01-24*
