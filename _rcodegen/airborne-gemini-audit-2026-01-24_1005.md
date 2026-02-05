Date Created: Saturday, January 24, 2026 10:05 AM
TOTAL_SCORE: 87/100

# Airborne Codebase Audit

## Executive Summary
The Airborne codebase demonstrates a strong adherence to Go best practices, with a clean "Clean Architecture" structure (`internal`, `cmd`, `pkg`). Security has clearly been a priority, evidenced by the use of a non-root user in Docker, careful context management, and input sanitization.

However, a few areas require attention to harden the system further, specifically regarding potential misconfiguration risks with development tools and the brittleness of the multi-tenancy implementation.

## Detailed Scoring

### Security (35/40)
*   **Strengths:**
    *   **Container Security:** `Dockerfile` correctly uses a non-root `airborne` user.
    *   **SSRF Protection:** `ChatService` restricts custom `base_url` usage to admins and validates URLs.
    *   **Input Validation:** Strong validation logic in `internal/validation` and `ChatService`.
    *   **Sanitization:** Errors are sanitized before reaching the client to prevent information leakage.
*   **Weaknesses:**
    *   **Dev Auth Bypass:** `internal/server/grpc.go` contains `developmentAuthInterceptor` which completely bypasses auth. While not wired in production `NewGRPCServer` by default, its presence is a "loaded gun" if accidental wiring occurs.
    *   **Hardcoded Tenants:** Tenant validation relies on a hardcoded list in `internal/db/repository.go`.

### Code Quality (25/30)
*   **Strengths:**
    *   **Structure:** Excellent separation of concerns.
    *   **Context Usage:** `context.Context` is used effectively for passing request-scoped data (Auth, Tenant).
*   **Weaknesses:**
    *   **Magic Strings:** Tenant IDs ("ai8", "email4ai") are hardcoded in multiple files (`db`, `server` logs).
    *   **Server Logic:** `NewGRPCServer` is becoming a "god function," handling auth, RAG, database, and image gen initialization.

### Observability (18/20)
*   **Strengths:**
    *   **Logging:** Consistent use of `log/slog`.
    *   **Interceptors:** Logging interceptors provide visibility into gRPC traffic.

### Documentation & Config (9/10)
*   **Strengths:**
    *   Clear `Dockerfile` and likely `airborne.yaml` structure.

## Critical Issues & Fixes

### 1. Hardening Development Interceptors
**Severity:** Medium (Potential High if misconfigured)
**File:** `internal/server/grpc.go`

The `developmentAuthInterceptor` functions bypass authentication entirely. To prevent accidental misuse, they should be renamed to explicitly indicate their unsafe nature, and ideally, they should panic or log a critical warning if used.

#### Patch-Ready Diff

```go
--- internal/server/grpc.go
+++ internal/server/grpc.go
@@ -216,14 +216,14 @@
 	}
 }
 
-// developmentAuthInterceptor injects a dev client in non-production mode when Redis is unavailable.
+// unsafeDevelopmentAuthInterceptor injects a dev client in non-production mode when Redis is unavailable.
 //
 // WARNING: This function bypasses authentication entirely. It is intended ONLY for
 // local development and testing. NEVER wire this into NewGRPCServer for production builds.
 // If you need to use this, ensure it's behind a build tag or explicit development mode check.
-func developmentAuthInterceptor() grpc.UnaryServerInterceptor {
-	slog.Warn("SECURITY: Development authentication interceptor is active - do not use in production")
+func unsafeDevelopmentAuthInterceptor() grpc.UnaryServerInterceptor {
+	slog.Error("SECURITY: UNSAFE_DEVELOPMENT_AUTH_INTERCEPTOR IS ACTIVE. THIS MUST NOT BE USED IN PRODUCTION.")
 	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
 		client := &auth.ClientKey{
 			ClientID:   "dev",
@@ -240,12 +240,12 @@
 	}
 }
 
-// developmentAuthStreamInterceptor injects a dev client for streams in non-production mode.
+// unsafeDevelopmentAuthStreamInterceptor injects a dev client for streams in non-production mode.
 //
 // WARNING: This function bypasses authentication entirely. It is intended ONLY for
 // local development and testing. NEVER wire this into NewGRPCServer for production builds.
 // If you need to use this, ensure it's behind a build tag or explicit development mode check.
-func developmentAuthStreamInterceptor() grpc.StreamServerInterceptor {
-	slog.Warn("SECURITY: Development stream authentication interceptor is active - do not use in production")
+func unsafeDevelopmentAuthStreamInterceptor() grpc.StreamServerInterceptor {
+	slog.Error("SECURITY: UNSAFE_DEVELOPMENT_STREAM_AUTH_INTERCEPTOR IS ACTIVE. THIS MUST NOT BE USED IN PRODUCTION.")
 	return func(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
 		client := &auth.ClientKey{
 			ClientID:   "dev",
```

### 2. Centralizing Tenant Validation
**Severity:** Low (Maintenance)
**File:** `internal/db/repository.go`

The valid tenant IDs are hardcoded in the `db` package. This makes adding tenants a code change rather than a config change. While a full refactor to config-based tenants is recommended, a quick win is to at least centralize this. (No patch provided as it requires broader refactoring).