Date Created: 2026-01-18 21:20:44 +0100
TOTAL_SCORE: 74/100

# Airborne Code Audit (Security + Quality)

## Scope & Method
- Static review of core server, auth, admin, db, provider, config, and deployment files.
- Time-boxed audit; no tests executed; no code changes performed.

## Executive Summary
- Strengths: SSRF validation for custom base_url, request size limits, constant-time token checks, optional TLS, non-root container image.
- Key risks: Admin HTTP server is unauthenticated with permissive CORS when enabled, raw provider payloads are persisted and exposed, admin gRPC client forces insecure transport, and some outbound HTTP calls lack timeouts.

## Findings (ordered by severity)

### Critical
1) Unauthenticated admin HTTP server with permissive CORS and wide binding exposes sensitive operations.
   - Evidence: internal/admin/server.go:51-79, internal/admin/server.go:67-71
   - Impact: If `admin.enabled` is true, any reachable client can read activity feeds, fetch raw debug payloads, and trigger `/admin/test` LLM requests; data exfiltration and abuse risk.
   - Recommendation: Require admin token for `/admin/activity`, `/admin/debug`, `/admin/test`; restrict bind to localhost or a configured allowlist; consider disabling debug endpoint unless explicitly enabled.

### High
2) Raw provider request/response JSON and system prompts are persisted by default.
   - Evidence: internal/provider/openai/client.go:108-113; internal/service/chat.go:1005-1012; internal/db/repository.go:365-381; internal/admin/server.go:253-277
   - Impact: Sensitive prompts, user content, and provider payloads are stored in the database and exposed via debug endpoint; increases compliance and incident blast radius.
   - Recommendation: Gate debug capture/storage behind explicit config; redact or encrypt; enable only for admin/debug sessions.

3) Admin HTTP server gRPC client always uses insecure transport.
   - Evidence: internal/admin/server.go:308-310
   - Impact: When gRPC TLS is enabled or admin server runs off-host, requests either fail or expose auth token in plaintext.
   - Recommendation: Use TLS credentials when TLS is enabled; optionally enforce localhost-only usage unless explicitly configured.

### Medium
4) Gemini FileSearch HTTP calls use `http.DefaultClient` without timeouts.
   - Evidence: internal/provider/gemini/filestore.go:109-116 (and other calls in file)
   - Impact: Potential hangs and resource exhaustion on slow or stalled upstreams.
   - Recommendation: Use a shared `http.Client` with timeouts and/or enforce per-request context deadlines.

### Low
5) SQL query logging includes full args (may contain prompts, user data).
   - Evidence: internal/db/postgres.go:89-93
   - Impact: Sensitive data can leak to logs when `database.log_queries` is enabled.
   - Recommendation: Log query template only, or log arg count/types with redaction.

6) Go toolchain is pinned to 1.25.x in `go.mod` and Dockerfile.
   - Evidence: go.mod, Dockerfile
   - Impact: If this is not a deliberate custom toolchain, it can hinder builds in standard environments.
   - Recommendation: Pin to a released Go version or document the required toolchain.

## Patch-ready diffs

### 1) Require admin token for sensitive admin endpoints
```diff
diff --git a/internal/admin/server.go b/internal/admin/server.go
--- a/internal/admin/server.go
+++ b/internal/admin/server.go
@@
-import (
+import (
 	"context"
+	"crypto/subtle"
 	"encoding/json"
 	"fmt"
 	"log/slog"
 	"net/http"
 	"strconv"
 	"strings"
 	"time"
@@
 func NewServer(repo *db.Repository, cfg Config) *Server {
@@
 	// CORS middleware wrapper
 	corsHandler := func(h http.HandlerFunc) http.HandlerFunc {
 		return func(w http.ResponseWriter, r *http.Request) {
 			w.Header().Set("Access-Control-Allow-Origin", "*")
 			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
 			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
@@
 			h(w, r)
 		}
 	}
+
+	// Auth middleware for admin endpoints
+	authHandler := func(h http.HandlerFunc) http.HandlerFunc {
+		return func(w http.ResponseWriter, r *http.Request) {
+			if !s.authorize(r) {
+				http.Error(w, "unauthorized", http.StatusUnauthorized)
+				return
+			}
+			h(w, r)
+		}
+	}
@@
-	mux.HandleFunc("/admin/activity", corsHandler(s.handleActivity))
-	mux.HandleFunc("/admin/debug/", corsHandler(s.handleDebug))
+	mux.HandleFunc("/admin/activity", corsHandler(authHandler(s.handleActivity)))
+	mux.HandleFunc("/admin/debug/", corsHandler(authHandler(s.handleDebug)))
 	mux.HandleFunc("/admin/health", corsHandler(s.handleHealth))
-	mux.HandleFunc("/admin/test", corsHandler(s.handleTest))
+	mux.HandleFunc("/admin/test", corsHandler(authHandler(s.handleTest)))
@@
 }
+
+func (s *Server) authorize(r *http.Request) bool {
+	if s.authToken == "" {
+		return false
+	}
+	token := extractAdminToken(r)
+	if token == "" {
+		return false
+	}
+	return subtle.ConstantTimeCompare([]byte(token), []byte(s.authToken)) == 1
+}
+
+func extractAdminToken(r *http.Request) string {
+	auth := strings.TrimSpace(r.Header.Get("Authorization"))
+	if auth != "" {
+		lower := strings.ToLower(auth)
+		if strings.HasPrefix(lower, "bearer ") {
+			return strings.TrimSpace(auth[len("bearer "):])
+		}
+		return auth
+	}
+	return strings.TrimSpace(r.Header.Get("X-API-Key"))
+}
```

### 2) Gate persistence of raw provider payloads behind an explicit flag
```diff
diff --git a/internal/service/chat.go b/internal/service/chat.go
--- a/internal/service/chat.go
+++ b/internal/service/chat.go
@@
-import (
+import (
 	"context"
 	"fmt"
 	"html"
 	"log/slog"
+	"os"
 	"strings"
 	"time"
@@
-	// Build debug info from captured JSON (if available)
-	var debugInfo *db.DebugInfo
-	if len(result.RequestJSON) > 0 || len(result.ResponseJSON) > 0 {
+	// Build debug info from captured JSON only when explicitly enabled
+	var debugInfo *db.DebugInfo
+	if strings.EqualFold(os.Getenv("AIRBORNE_STORE_DEBUG_JSON"), "true") &&
+		(len(result.RequestJSON) > 0 || len(result.ResponseJSON) > 0) {
 		debugInfo = &db.DebugInfo{
 			SystemPrompt:    req.Instructions,
 			RawRequestJSON:  string(result.RequestJSON),
 			RawResponseJSON: string(result.ResponseJSON),
 		}
 	}
```

### 3) Add timeouts to Gemini FileSearch HTTP calls
```diff
diff --git a/internal/provider/gemini/filestore.go b/internal/provider/gemini/filestore.go
--- a/internal/provider/gemini/filestore.go
+++ b/internal/provider/gemini/filestore.go
@@
 const (
 	fileSearchBaseURL         = "https://generativelanguage.googleapis.com/v1beta"
 	fileSearchPollingInterval = 2 * time.Second
 	fileSearchPollingTimeout  = 5 * time.Minute
+	fileSearchHTTPTimeout     = 60 * time.Second
 )
+
+var fileSearchHTTPClient = &http.Client{Timeout: fileSearchHTTPTimeout}
@@
-	resp, err := http.DefaultClient.Do(req)
+	resp, err := fileSearchHTTPClient.Do(req)
@@
-	resp, err := http.DefaultClient.Do(req)
+	resp, err := fileSearchHTTPClient.Do(req)
@@
-	resp2, err := http.DefaultClient.Do(req2)
+	resp2, err := fileSearchHTTPClient.Do(req2)
@@
-			resp, err := http.DefaultClient.Do(req)
+			resp, err := fileSearchHTTPClient.Do(req)
@@
-	resp, err := http.DefaultClient.Do(req)
+	resp, err := fileSearchHTTPClient.Do(req)
@@
-	resp, err := http.DefaultClient.Do(req)
+	resp, err := fileSearchHTTPClient.Do(req)
@@
-	resp, err := http.DefaultClient.Do(req)
+	resp, err := fileSearchHTTPClient.Do(req)
```

### 4) Reduce leakage in SQL debug logging
```diff
diff --git a/internal/db/postgres.go b/internal/db/postgres.go
--- a/internal/db/postgres.go
+++ b/internal/db/postgres.go
@@
 func (c *Client) logQuery(query string, args ...interface{}) {
 	if c.logQueries {
-		slog.Debug("executing query", "sql", query, "args", args)
+		slog.Debug("executing query", "sql", query, "arg_count", len(args))
 	}
 }
```

## Test & Verification Notes
- No tests executed (time-boxed audit).
- Consider running `go test ./...` and integration checks for admin endpoints and database persistence after applying fixes.
