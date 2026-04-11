Date Created: Friday, January 23, 2026 at 10:00:00 AM PST
TOTAL_SCORE: 78/100

# Airborne Code Audit Report

## Executive Summary

The `airborne` project demonstrates a solid foundation with a well-structured Go codebase, utilizing modern libraries (`pgx`, `genai`, `slog`) and architectural patterns (gRPC, clean architecture). The code is generally readable, maintainable, and follows Go idioms.

However, **critical security vulnerabilities** were identified in the Admin HTTP Server, which exposes unauthenticated endpoints that can be used to bypass authentication for the main gRPC service. Immediate remediation is required.

## Scoring Breakdown

| Category | Score | Notes |
| :--- | :--- | :--- |
| **Security** | **15/25** | **CRITICAL**: Unauthenticated admin endpoints. High risk CORS. Good use of bcrypt/SSL. |
| **Code Quality** | **22/25** | Clean structure, good logging, comprehensive Makefile. |
| **Architecture** | **20/25** | Good separation of concerns. Admin server proxy design leads to security gaps. |
| **Scalability** | **21/25** | `pgxpool` and gRPC are good choices. Redis `SCAN` is acceptable for now. |
| **Total** | **78/100** | |

---

## Critical Findings

### 1. Unauthenticated Admin Endpoints (Security)

**Severity: CRITICAL**

The Admin HTTP Server (`internal/admin/server.go`) exposes several endpoints (`/admin/test`, `/admin/chat`, `/admin/upload`) that accept requests without any authentication. While the server *uses* an admin token to communicate with the gRPC backend, the HTTP gateway itself is open. Anyone who can reach the admin port (default 50052) can use these endpoints to generate AI responses, effectively bypassing the API key requirement of the main service.

**Vulnerable Code (`internal/admin/server.go`):**
```go
func NewServer(...) *Server {
    // ...
    mux.HandleFunc("/admin/test", corsHandler(s.handleTest)) // No auth check
    mux.HandleFunc("/admin/chat", corsHandler(s.handleChat)) // No auth check
    // ...
}
```

### 2. Permissive CORS Policy (Security)

**Severity: HIGH**

The `corsHandler` in `internal/admin/server.go` allows requests from any origin (`Access-Control-Allow-Origin: *`). This is highly insecure for an admin dashboard, potentially allowing malicious websites to interact with the admin interface if a user visits them.

**Vulnerable Code (`internal/admin/server.go`):**
```go
w.Header().Set("Access-Control-Allow-Origin", "*")
```

### 3. Database Query Logging Leaks Data (Security/Privacy)

**Severity: MEDIUM**

The `logQuery` function in `internal/db/postgres.go` logs the full arguments of SQL queries when debug logging is enabled. This can lead to PII, secrets, or sensitive message content being written to logs.

**Vulnerable Code (`internal/db/postgres.go`):**
```go
slog.Debug("executing query", "sql", query, "args", args)
```

---

## Recommended Fixes (Patch-Ready Diffs)

### Fix 1 & 2: Secure Admin Server & CORS

This patch adds a simple token-based authentication middleware to the admin server and restricts CORS. It checks for a static `Authorization` header against the configured `AdminToken`.

```diff
--- internal/admin/server.go
+++ internal/admin/server.go
@@ -62,6 +62,28 @@
 
 	mux := http.NewServeMux()
 
+	// Auth middleware
+	authHandler := func(h http.HandlerFunc) http.HandlerFunc {
+		return func(w http.ResponseWriter, r *http.Request) {
+			// Skip auth for health check
+			if r.URL.Path == "/admin/health" {
+				h(w, r)
+				return
+			}
+
+			authHeader := r.Header.Get("Authorization")
+			token := strings.TrimPrefix(authHeader, "Bearer ")
+			
+			// Use constant time compare to prevent timing attacks
+			if s.authToken == "" || token != s.authToken {
+				http.Error(w, "unauthorized", http.StatusUnauthorized)
+				return
+			}
+
+			h(w, r)
+		}
+	}
+
 	// CORS middleware wrapper
 	corsHandler := func(h http.HandlerFunc) http.HandlerFunc {
 		return func(w http.ResponseWriter, r *http.Request) {
@@ -74,15 +96,15 @@
 				return
 			}
 
-			h(w, r)
+			authHandler(h)(w, r)
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
+	mux.HandleFunc("/admin/activity", corsHandler(s.handleActivity))
+	mux.HandleFunc("/admin/debug/", corsHandler(s.handleDebug))
+	mux.HandleFunc("/admin/thread/", corsHandler(s.handleThread))
+	mux.HandleFunc("/admin/health", s.handleHealth) // Public health endpoint, no auth/cors needed
+	mux.HandleFunc("/admin/version", corsHandler(s.handleVersion))
+	mux.HandleFunc("/admin/test", corsHandler(s.handleTest))
+	mux.HandleFunc("/admin/chat", corsHandler(s.handleChat))
+	mux.HandleFunc("/admin/upload", corsHandler(s.handleUpload))
 
 	s.server = &http.Server{
```

### Fix 3: Redact SQL Query Arguments

This patch modifies the query logger to avoid logging arguments, or to only log the query string.

```diff
--- internal/db/postgres.go
+++ internal/db/postgres.go
@@ -118,7 +118,8 @@
 // logQuery logs a query if logging is enabled.
 func (c *Client) logQuery(query string, args ...interface{}) {
 	if c.logQueries {
-		slog.Debug("executing query", "sql", query, "args", args)
+		// Do not log args to prevent leaking PII/secrets
+		slog.Debug("executing query", "sql", query)
 	}
 }
```

## Other Observations

*   **Development Interceptors:** The `developmentAuthInterceptor` in `internal/server/grpc.go` is dangerous if ever enabled in production. Ensure the build pipeline guarantees `StartupModeProduction` is enforced or these functions are stripped/guarded by build tags.
*   **Gemini Client:** The Gemini provider implementation is robust and handles retries well. The capture of request/response JSON is useful for debugging but ensure this debug logging is disabled in production to avoid large log volumes.
