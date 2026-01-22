# Airborne Codebase Audit Report
Date Created: Thursday, January 22, 2026 09:00 AM

## Executive Summary

This report details the findings of a comprehensive code and security audit of the `airborne` project. The audit focused on the Go backend services (`cmd/`, `internal/`).

**Overall Health:** Moderate. The codebase follows good Go idioms and project structure. However, there are **CRITICAL** security vulnerabilities in the Admin HTTP server that essentially leave the system open to unauthorized control if the admin port is accessible.

## Critical Security Vulnerabilities

### 1. Unauthenticated Admin Endpoints (High Severity)
**Location:** `internal/admin/server.go`

The Admin HTTP server (`/admin/*`) exposes sensitive functionality—including running chat queries, uploading files, and viewing system activity—without **any** incoming request authentication.

While the server is configured with an `AuthToken`, this token is only used to *sign* outgoing requests from the Admin Server to the gRPC service. The Admin Server effectively acts as an open proxy that escalates privileges for any caller.

**Impact:** An attacker who can reach the admin port (default 50052) can:
*   Execute arbitrary chat commands (consuming paid API credits).
*   Upload arbitrary files.
*   View sensitive user activity logs.

### 2. Permissive CORS Policy (Medium Severity)
**Location:** `internal/admin/server.go`

The admin server explicitly sets `Access-Control-Allow-Origin: *`.

```go
w.Header().Set("Access-Control-Allow-Origin", "*")
```

**Impact:** Combined with the lack of authentication, this allows malicious websites to interact with the local admin server (CSRF-like attacks, though technically cross-origin resource sharing) if a developer visits a compromised site while the server is running locally.

## Major Issues

### 3. Potential Denial of Service via Memory Exhaustion (High Severity)
**Location:** `internal/admin/server.go` (`handleUpload`)

The `handleUpload` handler reads the entire uploaded file into memory:

```go
content, err := io.ReadAll(file)
```

The `ParseMultipartForm` limit is set to 100MB.

**Impact:** Concurrent uploads of large files can rapidly exhaust server memory, leading to a crash (OOM Kill).

### 4. Fragile SQL Query Construction (Medium Severity)
**Location:** `internal/db/repository.go`

The codebase relies on `fmt.Sprintf` to insert table names into SQL queries to support multi-tenancy.

```go
query := fmt.Sprintf("INSERT INTO %s ...", r.threadsTable())
```

**Mitigation:** This is currently mitigated by a strict allowlist in `NewTenantRepository` (`ValidTenantIDs`).
**Risk:** This pattern is fragile. If the validation logic is ever relaxed or bypassed, it creates an immediate SQL Injection vector.

## Minor Issues

### 5. HTTP Semantics Violation
**Location:** `internal/admin/server.go`

The API frequently returns `200 OK` HTTP status codes even when an error occurs, embedding the error in the JSON body (`{"error": "..."}`). This complicates monitoring and client-side error handling.

## Patch-Ready Diffs

### Fix 1: Add Authentication Middleware to Admin Server

This patch adds a middleware that checks the `Authorization` header against the configured `AuthToken`.

```diff
--- internal/admin/server.go
+++ internal/admin/server.go
@@ -77,6 +77,18 @@
 		}
 	}
 
+	// Auth middleware
+	authMiddleware := func(h http.HandlerFunc) http.HandlerFunc {
+		return func(w http.ResponseWriter, r *http.Request) {
+			// Check Authorization header
+			authHeader := r.Header.Get("Authorization")
+			if s.authToken != "" && authHeader != "Bearer "+s.authToken {
+				http.Error(w, "Unauthorized", http.StatusUnauthorized)
+				return
+			}
+			h(w, r)
+		}
+	}
+
 	// Register endpoints
-	mux.HandleFunc("/admin/activity", corsHandler(s.handleActivity))
-	mux.HandleFunc("/admin/debug/", corsHandler(s.handleDebug))
-	mux.HandleFunc("/admin/thread/", corsHandler(s.handleThread))
-	mux.HandleFunc("/admin/health", corsHandler(s.handleHealth))
-	mux.HandleFunc("/admin/version", corsHandler(s.handleVersion))
-	mux.HandleFunc("/admin/test", corsHandler(s.handleTest))
-	mux.HandleFunc("/admin/chat", corsHandler(s.handleChat))
-	mux.HandleFunc("/admin/upload", corsHandler(s.handleUpload))
+	// Public endpoints
+	mux.HandleFunc("/admin/health", corsHandler(s.handleHealth))
+	mux.HandleFunc("/admin/version", corsHandler(s.handleVersion))
+
+	// Protected endpoints
+	mux.HandleFunc("/admin/activity", corsHandler(authMiddleware(s.handleActivity)))
+	mux.HandleFunc("/admin/debug/", corsHandler(authMiddleware(s.handleDebug)))
+	mux.HandleFunc("/admin/thread/", corsHandler(authMiddleware(s.handleThread)))
+	mux.HandleFunc("/admin/test", corsHandler(authMiddleware(s.handleTest)))
+	mux.HandleFunc("/admin/chat", corsHandler(authMiddleware(s.handleChat)))
+	mux.HandleFunc("/admin/upload", corsHandler(authMiddleware(s.handleUpload)))
 
 	s.server = &http.Server{
```

### Fix 2: Stream File Upload to Avoid Memory Exhaustion

This patch modifies `uploadFileToGemini` to accept an `io.Reader` instead of a byte slice, and updates the handler to pass the file directly.

```diff
--- internal/admin/server.go
+++ internal/admin/server.go
@@ -876,14 +876,13 @@
 	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
 	defer cancel()
 
-	fileURI, err := s.uploadFileToGemini(ctx, apiKey, file, header.Filename, mimeType)
+	fileURI, err := s.uploadFileToGemini(ctx, apiKey, file, header.Filename, mimeType)
 	if err != nil {
 		slog.Error("failed to upload file to Gemini", "error", err, "filename", header.Filename)
```

```diff
--- internal/admin/server.go
+++ internal/admin/server.go
@@ -911,7 +911,7 @@
 }
 
 // uploadFileToGemini uploads a file to Gemini Files API and returns the URI.
-func (s *Server) uploadFileToGemini(ctx context.Context, apiKey string, file multipart.File, filename, mimeType string) (string, error) {
+func (s *Server) uploadFileToGemini(ctx context.Context, apiKey string, file io.Reader, filename, mimeType string) (string, error) {
 	// Create Gemini client
 	clientConfig := &genai.ClientConfig{
 		APIKey:  apiKey,
@@ -923,17 +923,11 @@
 		return "", fmt.Errorf("create Gemini client: %w", err)
 	}
 
-	// Read file content
-	content, err := io.ReadAll(file)
-	if err != nil {
-		return "", fmt.Errorf("read file: %w", err)
-	}
-
 	// Upload file
 	uploadConfig := &genai.UploadFileConfig{
 		MIMEType:    mimeType,
 		DisplayName: filename,
 	}
 
-	uploadedFile, err := client.Files.Upload(ctx, bytes.NewReader(content), uploadConfig)
+	uploadedFile, err := client.Files.Upload(ctx, file, uploadConfig)
 	if err != nil {
 		return "", fmt.Errorf("upload file: %w", err)
 	}
```
