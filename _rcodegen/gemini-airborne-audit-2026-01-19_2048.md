Date Created: 2026-01-19_2048
TOTAL_SCORE: 55/100

# Airborne Code Audit Report

## Executive Summary
The `airborne` project demonstrates a solid architectural foundation with clear separation of concerns (Clean Architecture), utilizing gRPC for internal communication and modern Go practices. However, **CRITICAL security vulnerabilities** exist in the administrative interface and file handling that must be addressed immediately before any production deployment.

## Grade Breakdown (55/100)
*   **Architecture & Design (25/30):** Good use of interfaces, dependency injection, and project layout.
*   **Security (0/30):** Critical failures in admin protection and file handling.
*   **Code Quality (15/20):** Generally clean code, but some blocking calls and hardcoded configurations.
*   **Reliability (15/20):** Good error handling patterns, though some observability gaps.

## Critical Issues (Priority P0)

### 1. Unauthenticated Admin Interface
**File:** `internal/admin/server.go`
The HTTP admin server (default port 50052) exposes sensitive endpoints like `/admin/activity` (user data), `/admin/debug` (full payloads), and `/admin/test` (LLM usage) with **ZERO authentication**. Anyone with network access to this port can exfiltrate data or abuse the LLM quotas.

### 2. Insecure Temporary File Handling (Symlink Race)
**File:** `internal/db/postgres.go`
The `writeCACertToFile` function writes sensitive CA certificates to a fixed path `/tmp/airborne-certs/supabase-ca.crt` with a predictable name.
*   **Risk:** A malicious local user can pre-create this path as a symlink to overwrite arbitrary files on the system or read the injected certificate.
*   **Fix:** Use `os.MkdirTemp` to create a randomized, secure directory.

## High Issues (Priority P1)

### 3. Permissive CORS Policy
**File:** `internal/admin/server.go`
The admin server sets `Access-Control-Allow-Origin: *`.
*   **Risk:** If the admin tool is accessed via a browser, it opens the door to CSRF/CORS attacks from malicious websites.

### 4. Blocking External Calls in Config Loading
**File:** `internal/config/config.go`
`fetchDopplerSecret` performs a blocking HTTP request during `Load()`. If Doppler is down or slow, the application startup will hang indefinitely or fail without a clear fallback strategy for local development.

## Medium/Low Issues

*   **Hardcoded Auth Exclusions:** `internal/auth/interceptor.go` hardcodes skipped methods. This should be configuration-driven.
*   **Logging:** `slog.Debug` usage is good, but ensure `LogQueries` in DB config is false by default in production to prevent leaking PII in SQL params.

---

## Patch-Ready Diffs

### Fix 1: Secure Admin Server with Bearer Auth

```go
diff --git a/internal/admin/server.go b/internal/admin/server.go
index 1234567..89abcdef 100644
--- a/internal/admin/server.go
+++ b/internal/admin/server.go
@@ -58,6 +58,18 @@ func NewServer(dbClient *db.Client, cfg Config) *Server {
 
 	mux := http.NewServeMux()
 
+	// Auth middleware
+	authHandler := func(h http.HandlerFunc) http.HandlerFunc {
+		return func(w http.ResponseWriter, r *http.Request) {
+			// simple bearer token check
+			authHeader := r.Header.Get("Authorization")
+			if s.authToken != "" && authHeader != "Bearer "+s.authToken {
+				http.Error(w, "unauthorized", http.StatusUnauthorized)
+				return
+			}
+			h(w, r)
+		}
+	}
+
 	// CORS middleware wrapper
 	corsHandler := func(h http.HandlerFunc) http.HandlerFunc {
 		return func(w http.ResponseWriter, r *http.Request) {
@@ -75,13 +87,13 @@ func NewServer(dbClient *db.Client, cfg Config) *Server {
 	}
 
 	// Register endpoints
-	mux.HandleFunc("/admin/activity", corsHandler(s.handleActivity))
-	mux.HandleFunc("/admin/debug/", corsHandler(s.handleDebug))
-	mux.HandleFunc("/admin/thread/", corsHandler(s.handleThread))
-	mux.HandleFunc("/admin/health", corsHandler(s.handleHealth))
-	mux.HandleFunc("/admin/version", corsHandler(s.handleVersion))
-	mux.HandleFunc("/admin/test", corsHandler(s.handleTest))
-	mux.HandleFunc("/admin/chat", corsHandler(s.handleChat))
+	mux.HandleFunc("/admin/activity", corsHandler(authHandler(s.handleActivity)))
+	mux.HandleFunc("/admin/debug/", corsHandler(authHandler(s.handleDebug)))
+	mux.HandleFunc("/admin/thread/", corsHandler(authHandler(s.handleThread)))
+	mux.HandleFunc("/admin/health", corsHandler(s.handleHealth)) // Keep health public
+	mux.HandleFunc("/admin/version", corsHandler(s.handleVersion)) // Keep version public
+	mux.HandleFunc("/admin/test", corsHandler(authHandler(s.handleTest)))
+	mux.HandleFunc("/admin/chat", corsHandler(authHandler(s.handleChat)))
 
 	s.server = &http.Server{
 		Addr:         fmt.Sprintf(":%d", cfg.Port),
```

### Fix 2: Secure Temporary File Creation

```go
diff --git a/internal/db/postgres.go b/internal/db/postgres.go
index abcdef1..2345678 100644
--- a/internal/db/postgres.go
+++ b/internal/db/postgres.go
@@ -118,9 +118,8 @@ func (c *Client) logQuery(query string, args ...interface{}) {
 // Returns the path to the certificate file.
 func writeCACertToFile(certPEM string) (string, error) {
-	// Use a stable path so we don't create multiple files on restarts
-	certDir := "/tmp/airborne-certs"
-	if err := os.MkdirAll(certDir, 0700); err != nil {
+	// Use os.MkdirTemp for secure directory creation
+	certDir, err := os.MkdirTemp("", "airborne-certs-*")
+	if err != nil {
 		return "", fmt.Errorf("failed to create cert directory: %w", err)
 	}
 
```
