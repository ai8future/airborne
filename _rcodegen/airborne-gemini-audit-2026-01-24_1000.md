Date Created: 2026-01-24 10:00:00
TOTAL_SCORE: 85/100

# Airborne Codebase Audit Report

## Executive Summary
The Airborne codebase is generally well-structured, leveraging modern Go practices, gRPC for services, and safe database patterns (pgxpool). Key security features like API key hashing (bcrypt) and secure random generation are implemented correctly.

However, a **critical security vulnerability** was identified in the Admin HTTP Server (`internal/admin/server.go`), where sensitive endpoints are exposed without authentication. This allows any network-adjacent attacker to view activity feeds, debug data (potentially containing PII), and execute chat requests.

## Score Breakdown (85/100)
*   **Architecture & Code Quality:** 28/30
    *   Clean separation of concerns (svc, db, auth).
    *   Proper use of context and error handling.
*   **Security:** 20/30
    *   (-10) **CRITICAL:** Missing authentication on Admin HTTP endpoints.
    *   Basic auth implementation is solid (bcrypt, expiries).
*   **Reliability & Performance:** 19/20
    *   Good use of connection pooling.
    *   (-1) Unbounded Redis scan for key listing could be a bottleneck.
*   **Observability:** 18/20
    *   Structured logging (slog) used effectively.
    *   (-2) Potential for sensitive data leakage in logs if `log_queries` is enabled or via error logging.

## Detailed Findings

### 1. Critical: Unauthenticated Admin Endpoints
**File:** `internal/admin/server.go`
**Severity:** Critical
**Description:** The HTTP endpoints `/admin/activity`, `/admin/debug/`, `/admin/thread/`, `/admin/test`, `/admin/chat`, and `/admin/upload` do not validate the `Authorization` header. While the handlers `handleTest` and `handleChat` access `s.authToken`, they use it to authenticate *outgoing* gRPC calls, not to validate the *incoming* HTTP request.
**Impact:** Unrestricted access to system activity, user data, and AI generation capabilities.

### 2. High: Permissive CORS Policy
**File:** `internal/admin/server.go`
**Severity:** High
**Description:** The CORS configuration allows `Access-Control-Allow-Origin: *`.
**Recommendation:** Restrict to specific trusted domains in production.

### 3. Medium: Potential PII Logging
**File:** `internal/db/postgres.go` / `internal/db/repository.go`
**Severity:** Medium
**Description:** The `logQuery` function logs full SQL queries and arguments. If `log_queries` is enabled in config, this will write message content (potentially PII/PHI) to the logs.
**Recommendation:** Ensure `log_queries` is strictly disabled in production or sanitize arguments before logging.

### 4. Low: Insecure Temp File Usage
**File:** `internal/db/postgres.go`
**Severity:** Low
**Description:** `writeCACertToFile` writes to a fixed path `/tmp/airborne-certs`.
**Recommendation:** Use `os.MkdirTemp` to avoid collisions and potential symlink attacks, although the permissions (0700) mitigate this somewhat.

## Recommended Fixes (Patch)

The following patch addresses the critical authentication vulnerability in the Admin Server by implementing a middleware that validates the `Authorization` header against the configured `admin_token`.

```diff
diff --git a/internal/admin/server.go b/internal/admin/server.go
index 1234567..89abcdef 1
--- a/internal/admin/server.go
+++ b/internal/admin/server.go
@@ -4,6 +4,7 @@ package admin
 
 import (
 	"bytes"
 	"context"
+	"crypto/subtle"
 	"encoding/json"
 	"fmt"
 	"io"
@@ -79,14 +80,18 @@ func NewServer(dbClient *db.Client, cfg Config) *Server {
 	}
 
 	// Register endpoints
-	mux.HandleFunc("/admin/activity", corsHandler(s.handleActivity))
-	mux.HandleFunc("/admin/debug/", corsHandler(s.handleDebug))
-	mux.HandleFunc("/admin/thread/", corsHandler(s.handleThread))
+	// Public endpoints
 	mux.HandleFunc("/admin/health", corsHandler(s.handleHealth))
 	mux.HandleFunc("/admin/version", corsHandler(s.handleVersion))
-	mux.HandleFunc("/admin/test", corsHandler(s.handleTest))
-	mux.HandleFunc("/admin/chat", corsHandler(s.handleChat))
-	mux.HandleFunc("/admin/upload", corsHandler(s.handleUpload))
+
+	// Protected endpoints
+	mux.HandleFunc("/admin/activity", corsHandler(s.authMiddleware(s.handleActivity)))
+	mux.HandleFunc("/admin/debug/", corsHandler(s.authMiddleware(s.handleDebug)))
+	mux.HandleFunc("/admin/thread/", corsHandler(s.authMiddleware(s.handleThread)))
+	mux.HandleFunc("/admin/test", corsHandler(s.authMiddleware(s.handleTest)))
+	mux.HandleFunc("/admin/chat", corsHandler(s.authMiddleware(s.handleChat)))
+	mux.HandleFunc("/admin/upload", corsHandler(s.authMiddleware(s.handleUpload)))
 
 	s.server = &http.Server{
 		Addr:         fmt.Sprintf(":%d", cfg.Port),
@@ -113,6 +118,34 @@ func (s *Server) Shutdown(ctx context.Context) error {
 	return s.server.Shutdown(ctx)
 }
 
+// authMiddleware ensures the request is authenticated with the admin token.
+func (s *Server) authMiddleware(h http.HandlerFunc) http.HandlerFunc {
+	return func(w http.ResponseWriter, r *http.Request) {
+		// Check Authorization header
+		authHeader := r.Header.Get("Authorization")
+		if authHeader == "" {
+			// Try x-api-key header as well
+			authHeader = r.Header.Get("x-api-key")
+		}
+
+		if authHeader == "" {
+			http.Error(w, "unauthorized", http.StatusUnauthorized)
+			return
+		}
+
+		// Handle "Bearer " prefix if present
+		token := authHeader
+		if strings.HasPrefix(strings.ToLower(token), "bearer ") {
+			token = token[7:]
+		}
+		token = strings.TrimSpace(token)
+
+		if subtle.ConstantTimeCompare([]byte(token), []byte(s.authToken)) != 1 {
+			http.Error(w, "forbidden", http.StatusForbidden)
+			return
+		}
+
+		h(w, r)
+	}
+}
+
 // handleActivity returns recent activity for the dashboard.
 // GET /admin/activity?limit=50&tenant_id=optional
 func (s *Server) handleActivity(w http.ResponseWriter, r *http.Request) {
```
