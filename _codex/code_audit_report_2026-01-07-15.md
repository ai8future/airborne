# AIBox Code Audit Report (2026-01-07-15) - Remaining Issues

## Scope
- Reviewed Go services, auth, tenancy, providers, RAG, configs, Docker assets, and proto definitions.
- Skipped `_studies` and `_proposals` per AGENTS instructions.

## Method
- Static review only; no tests or builds executed.

## Last Updated
- 2026-01-07: Removed issues fixed in versions 0.4.5 through 0.5.3

---

## Remaining Findings

### F-03 (Medium) Tenant config load failures silently fall back to legacy mode
- Evidence: `internal/server/grpc.go`.
- Impact: In production, an invalid or missing tenant config can cause the server to start without tenancy enforcement, potentially bypassing isolation expectations.
- Recommendation: Fail fast in production when tenant config loading fails.
- Patch-ready diff:
```diff
diff --git a/internal/server/grpc.go b/internal/server/grpc.go
index 4c0eb5e..f3c6a1a 100644
--- a/internal/server/grpc.go
+++ b/internal/server/grpc.go
@@
 	tenantMgr, err := tenant.Load("")
 	if err != nil {
-		slog.Warn("tenant config not loaded - running in single-tenant legacy mode", "error", err)
-		// Create an empty manager for legacy mode
-		tenantMgr = nil
+		if cfg.StartupMode.IsProduction() {
+			return nil, fmt.Errorf("tenant config required in production: %w", err)
+		}
+		slog.Warn("tenant config not loaded - running in single-tenant legacy mode", "error", err)
+		tenantMgr = nil
 	} else {
 		slog.Info("tenant configurations loaded",
 			"tenant_count", tenantMgr.TenantCount(),
 			"tenants", tenantMgr.TenantCodes(),
 		)
 	}
```

### F-06 (Medium) OpenAI polling can hang without a deadline
- Evidence: `internal/provider/openai/client.go`.
- Impact: If OpenAI never transitions to a terminal status, requests can hang indefinitely (no timeout on poll loop).
- Recommendation: Use a bounded context for polling.
- Patch-ready diff:
```diff
diff --git a/internal/provider/openai/client.go b/internal/provider/openai/client.go
index b9b6e2e..b6f2d0b 100644
--- a/internal/provider/openai/client.go
+++ b/internal/provider/openai/client.go
@@
-		// Wait for completion
-		resp, err = waitForCompletion(ctx, client, resp)
+		// Wait for completion
+		pollCtx, pollCancel := context.WithTimeout(ctx, requestTimeout)
+		resp, err = waitForCompletion(pollCtx, client, resp)
+		pollCancel()
 		if err != nil {
 			lastErr = err
 			slog.Warn("openai wait error", "attempt", attempt, "error", err)
 			continue
 		}
```

### F-08 (Low) Go directive uses a patch version
- Evidence: `go.mod`.
- Impact: The Go toolchain expects `go 1.xx` format; patch values can break tooling and IDEs.
- Recommendation: Use `go 1.25` (or the intended major.minor) to match the toolchain and Docker image.
- Patch-ready diff:
```diff
diff --git a/go.mod b/go.mod
index bce12c8..06be784 100644
--- a/go.mod
+++ b/go.mod
@@
-go 1.25.5
+go 1.25
```

### F-10 (Low) API key generation wastes entropy
- Evidence: `internal/auth/keys.go`.
- Impact: `generateRandomString` allocates more random bytes than needed, then truncates, which is misleading and inefficient.
- Recommendation: Generate only the bytes needed for the requested hex length.
- Patch-ready diff:
```diff
diff --git a/internal/auth/keys.go b/internal/auth/keys.go
index 9b79a9f..1a4d9c4 100644
--- a/internal/auth/keys.go
+++ b/internal/auth/keys.go
@@
 func generateRandomString(length int) (string, error) {
-	bytes := make([]byte, length)
+	if length <= 0 {
+		return "", nil
+	}
+	byteLen := (length + 1) / 2
+	bytes := make([]byte, byteLen)
 	if _, err := rand.Read(bytes); err != nil {
 		return "", err
 	}
 	return hex.EncodeToString(bytes)[:length], nil
 }
```

---

## Testing gaps
- No tests cover tenant config load failure behavior in production mode.
- No tests cover OpenAI poll timeout behavior.

## Suggested validation steps
1) `go test ./...`
2) Verify tenant config loading behavior in production vs development mode.
3) Test OpenAI polling timeout scenarios.

---

## Fixed Issues (removed from this report)
The following issues have been fixed and removed from this report:
- F-01: FileService tenancy and size enforcement - Fixed in v0.4.5
- F-02: Admin endpoints permissive - Fixed in v0.4.7
- F-04: Token-per-minute defaults - Fixed in v0.4.12
- F-05: SSRF via base_url - Fixed in v0.4.14
- F-07: Docker health checks - Fixed in v0.4.13
- F-09: RAG TopK ignores config - Fixed in v0.5.0
