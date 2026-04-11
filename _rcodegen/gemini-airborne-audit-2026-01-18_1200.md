Date Created: 2026-01-18 12:00:00 UTC
TOTAL_SCORE: 92/100

# Airborne Codebase Audit Report

## 1. Executive Summary

The Airborne project demonstrates a mature and professional Go codebase. It utilizes modern patterns (dependency injection, structural logging with `slog`, atomic Redis operations via Lua) and enforces strict typing. The architecture clearly separates transport (gRPC), business logic (Service), and data access (Repository/Provider).

**Score Breakdown:**
- **Security**: 23/25 (Strong auth/crypto, but latent risk in dead code)
- **Architecture**: 23/25 (Clean separation, slightly coupled server init)
- **Code Quality**: 24/25 (Consistent style, good error handling)
- **Maintainability**: 22/25 (Good, but needs more inline documentation/comments in complex areas)

**Total**: 92/100

## 2. Detailed Findings

### Security Audit

**CRITICAL: Latent Backdoor Risk**
The file `internal/server/grpc.go` contains two package-private functions: `developmentAuthInterceptor` and `developmentAuthStreamInterceptor`. These functions hardcode a "development" client and **bypass all authentication checks**.
-   **Risk**: High. While not currently called in `NewGRPCServer`, their existence makes it dangerously easy for a developer to accidentally wire them up (e.g., "just for testing") and forget to remove them, exposing the entire API.
-   **Fix**: Remove this dead code immediately. (Included in Patch)

**Positive Security Features**:
-   **Secret Storage**: API keys are hashed using `bcrypt` before storage (verified in `internal/auth/keys.go`).
-   **Rate Limiting**: Implemented using atomic Lua scripts in Redis (`internal/auth/ratelimit.go`), preventing race conditions.
-   **SQL Injection Prevention**: Uses `pgx` with parameterized queries.
-   **SSRF Protection**: The OpenAI provider includes validation for the `BaseURL` to prevent internal network scanning.

### Code Quality & Architecture

**Strengths**:
-   **Configuration**: The `internal/config` package handles environment variables and YAML robustly.
-   **Concurrency**: usage of `errgroup` or explicit goroutines with context cancellation is consistent.
-   **Provider Abstraction**: The `internal/provider` package (specifically `openai` implementation) uses a clean interface, allowing easy addition of new LLMs.

**Areas for Improvement**:
-   **Monolithic Initialization**: `NewGRPCServer` in `internal/server/grpc.go` is very long (~150 lines) and mixes dependency wiring with logic. Splitting this into smaller setup functions would improve testability.
-   **Comments**: While the code is readable, some complex logic (like the RAG integration wiring) lacks explanatory comments "why" certain choices were made.

## 3. Patch-Ready Diffs

The following patch removes the dangerous unused development interceptors from `internal/server/grpc.go`.

```diff
diff --git a/internal/server/grpc.go b/internal/server/grpc.go
index 61f8a2b..e4b9c1a 100644
--- a/internal/server/grpc.go
+++ b/internal/server/grpc.go
@@ -244,45 +244,3 @@ func streamLoggingInterceptor() grpc.StreamServerInterceptor {
 		return err
 	}
 }
-
-// developmentAuthInterceptor injects a dev client in non-production mode when Redis is unavailable.
-//
-// WARNING: This function bypasses authentication entirely. It is intended ONLY for
-// local development and testing. NEVER wire this into NewGRPCServer for production builds.
-// If you need to use this, ensure it's behind a build tag or explicit development mode check.
-func developmentAuthInterceptor() grpc.UnaryServerInterceptor {
-	slog.Warn("SECURITY: Development authentication interceptor is active - do not use in production")
-	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
-		client := &auth.ClientKey{
-			ClientID:   "dev",
-			ClientName: "development",
-			Permissions: []auth.Permission{
-				// NOTE: PermissionAdmin intentionally excluded for security
-				auth.PermissionChat,
-				auth.PermissionChatStream,
-				auth.PermissionFiles,
-			},
-		}
-		ctx = context.WithValue(ctx, auth.ClientContextKey, client)
-		return handler(ctx, req)
-	}
-}
-
-// developmentAuthStreamInterceptor injects a dev client for streams in non-production mode.
-//
-// WARNING: This function bypasses authentication entirely. It is intended ONLY for
-// local development and testing. NEVER wire this into NewGRPCServer for production builds.
-// If you need to use this, ensure it's behind a build tag or explicit development mode check.
-func developmentAuthStreamInterceptor() grpc.StreamServerInterceptor {
-	slog.Warn("SECURITY: Development stream authentication interceptor is active - do not use in production")
-	return func(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
-		client := &auth.ClientKey{
-			ClientID:   "dev",
-			ClientName: "development",
-			Permissions: []auth.Permission{
-				// NOTE: PermissionAdmin intentionally excluded for security
-				auth.PermissionChat,
-				auth.PermissionChatStream,
-				auth.PermissionFiles,
-			},
-		}
-		ctx := context.WithValue(ss.Context(), auth.ClientContextKey, client)
-		wrapped := &devWrappedStream{ServerStream: ss, ctx: ctx}
-		return handler(srv, wrapped)
-	}
-}
-
-type devWrappedStream struct {
-	grpc.ServerStream
-	ctx context.Context
-}
-
-func (s *devWrappedStream) Context() context.Context {
-	return s.ctx
-}
```
