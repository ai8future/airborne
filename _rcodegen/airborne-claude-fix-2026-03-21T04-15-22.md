Date Created: 2026-03-21T04:15:22Z
TOTAL_SCORE: 73/100

# Airborne Codebase Audit Report

**Auditor:** Claude Code (Claude:Opus 4.6)
**Codebase:** airborne v1.8.11 — Multi-provider AI gateway (Go 1.25.5)
**Scope:** Full codebase analysis across ~77 Go source files, migrations, proto definitions, Docker, and build configuration

---

## Score Breakdown

| Category | Score | Max | Notes |
|----------|-------|-----|-------|
| Architecture & Design | 17 | 20 | Clean separation of concerns, good provider abstraction |
| Error Handling | 12 | 15 | Generally good, but some swallowed errors and missing checks |
| Security | 12 | 15 | Non-root Docker, bcrypt auth, SSRF protection; some race conditions |
| Concurrency Safety | 8 | 15 | Race condition in admin gRPC client, Doppler cache TOCTOU |
| Resource Management | 10 | 15 | Background goroutine lifecycle not managed; some response body patterns |
| Configuration & Deployment | 8 | 10 | Port mismatch in Dockerfile vs actual config |
| Database & Migrations | 6 | 10 | Migration 006 references wrong tables; missing indexes |

---

## CRITICAL Issues

### 1. Dockerfile EXPOSE Port Mismatch
**File:** `Dockerfile:55`
**Severity:** CRITICAL

The Dockerfile exposes port 50051, but the application actually listens on port 50612 (per `configs/airborne.yaml:5` and `docker-compose.yml:10`). This means containerized deployments relying on the EXPOSE directive for service discovery or documentation will reference the wrong port.

**Current code:**
```dockerfile
# Line 55
EXPOSE 50051
```

**Patch-ready diff:**
```diff
--- a/Dockerfile
+++ b/Dockerfile
@@ -52,7 +52,7 @@ USER airborne

 # Expose gRPC port
-EXPOSE 50051
+EXPOSE 50612

 # Health check
```

---

### 2. Migration 006 References Non-Existent Tables
**File:** `migrations/006_add_grounding_costs.sql:7-16`
**Severity:** CRITICAL

Migration 006 alters tables named `messages` and `activity`, but migration 004 migrated all data to tenant-prefixed tables (`ai8_airborne_messages`, `email4ai_airborne_messages`, etc.). If the original tables were dropped after migration 004, migration 006 will fail. If they still exist, the grounding cost columns are added to stale, unused tables rather than the active tenant tables.

**Current code:**
```sql
-- Line 7-8
ALTER TABLE messages ADD COLUMN IF NOT EXISTS grounding_queries INTEGER;
ALTER TABLE messages ADD COLUMN IF NOT EXISTS grounding_cost_usd DOUBLE PRECISION;

-- Line 11-12
ALTER TABLE activity ADD COLUMN IF NOT EXISTS grounding_queries INTEGER DEFAULT 0;
ALTER TABLE activity ADD COLUMN IF NOT EXISTS grounding_cost_usd DOUBLE PRECISION DEFAULT 0;

-- Line 15-16
CREATE INDEX IF NOT EXISTS idx_messages_grounding_cost ON messages(grounding_cost_usd) WHERE grounding_cost_usd IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_activity_grounding_cost ON activity(grounding_cost_usd) WHERE grounding_cost_usd > 0;
```

**Patch-ready diff:**
```diff
--- a/migrations/006_add_grounding_costs.sql
+++ b/migrations/006_add_grounding_costs.sql
@@ -4,13 +4,27 @@
 -- Gemini 2.5 and older: $35 / 1,000 grounded prompts

--- Add to messages table (per-message tracking)
-ALTER TABLE messages ADD COLUMN IF NOT EXISTS grounding_queries INTEGER;
-ALTER TABLE messages ADD COLUMN IF NOT EXISTS grounding_cost_usd DOUBLE PRECISION;
+-- Add to tenant messages tables (per-message tracking)
+ALTER TABLE ai8_airborne_messages ADD COLUMN IF NOT EXISTS grounding_queries INTEGER;
+ALTER TABLE ai8_airborne_messages ADD COLUMN IF NOT EXISTS grounding_cost_usd DOUBLE PRECISION;
+ALTER TABLE email4ai_airborne_messages ADD COLUMN IF NOT EXISTS grounding_queries INTEGER;
+ALTER TABLE email4ai_airborne_messages ADD COLUMN IF NOT EXISTS grounding_cost_usd DOUBLE PRECISION;
+ALTER TABLE zztest_airborne_messages ADD COLUMN IF NOT EXISTS grounding_queries INTEGER;
+ALTER TABLE zztest_airborne_messages ADD COLUMN IF NOT EXISTS grounding_cost_usd DOUBLE PRECISION;

--- Add to activity table (denormalized view for dashboard)
-ALTER TABLE activity ADD COLUMN IF NOT EXISTS grounding_queries INTEGER DEFAULT 0;
-ALTER TABLE activity ADD COLUMN IF NOT EXISTS grounding_cost_usd DOUBLE PRECISION DEFAULT 0;
+-- Add to tenant activity tables (denormalized view for dashboard)
+ALTER TABLE ai8_airborne_activity ADD COLUMN IF NOT EXISTS grounding_queries INTEGER DEFAULT 0;
+ALTER TABLE ai8_airborne_activity ADD COLUMN IF NOT EXISTS grounding_cost_usd DOUBLE PRECISION DEFAULT 0;
+ALTER TABLE email4ai_airborne_activity ADD COLUMN IF NOT EXISTS grounding_queries INTEGER DEFAULT 0;
+ALTER TABLE email4ai_airborne_activity ADD COLUMN IF NOT EXISTS grounding_cost_usd DOUBLE PRECISION DEFAULT 0;
+ALTER TABLE zztest_airborne_activity ADD COLUMN IF NOT EXISTS grounding_queries INTEGER DEFAULT 0;
+ALTER TABLE zztest_airborne_activity ADD COLUMN IF NOT EXISTS grounding_cost_usd DOUBLE PRECISION DEFAULT 0;

 -- Create index for queries that filter/sort by grounding cost
-CREATE INDEX IF NOT EXISTS idx_messages_grounding_cost ON messages(grounding_cost_usd) WHERE grounding_cost_usd IS NOT NULL;
-CREATE INDEX IF NOT EXISTS idx_activity_grounding_cost ON activity(grounding_cost_usd) WHERE grounding_cost_usd > 0;
+CREATE INDEX IF NOT EXISTS idx_ai8_messages_grounding_cost ON ai8_airborne_messages(grounding_cost_usd) WHERE grounding_cost_usd IS NOT NULL;
+CREATE INDEX IF NOT EXISTS idx_ai8_activity_grounding_cost ON ai8_airborne_activity(grounding_cost_usd) WHERE grounding_cost_usd > 0;
+CREATE INDEX IF NOT EXISTS idx_email4ai_messages_grounding_cost ON email4ai_airborne_messages(grounding_cost_usd) WHERE grounding_cost_usd IS NOT NULL;
+CREATE INDEX IF NOT EXISTS idx_email4ai_activity_grounding_cost ON email4ai_airborne_activity(grounding_cost_usd) WHERE grounding_cost_usd > 0;
+CREATE INDEX IF NOT EXISTS idx_zztest_messages_grounding_cost ON zztest_airborne_messages(grounding_cost_usd) WHERE grounding_cost_usd IS NOT NULL;
+CREATE INDEX IF NOT EXISTS idx_zztest_activity_grounding_cost ON zztest_airborne_activity(grounding_cost_usd) WHERE grounding_cost_usd > 0;
```

> **Note:** An alternative (and possibly better) approach is to dynamically generate these ALTER statements per tenant, or to apply them via the application's tenant migration system if one exists.

---

## HIGH Severity Issues

### 3. Race Condition in Admin Server gRPC Client Initialization
**File:** `internal/admin/server.go:448-468`
**Severity:** HIGH

The `getGRPCClient()` method lazily initializes a gRPC connection without any synchronization. Multiple concurrent HTTP handlers (handleTest, handleChat) can call this method simultaneously, causing:
- Multiple gRPC connections created, with only the last one stored (leaking earlier connections)
- Data race on `s.grpcClient` and `s.grpcConn` fields

**Current code:**
```go
func (s *Server) getGRPCClient() (pb.AirborneServiceClient, error) {
    if s.grpcClient != nil {     // <-- Unsynchronized read
        return s.grpcClient, nil
    }
    // ... creates connection ...
    s.grpcConn = conn            // <-- Unsynchronized write
    s.grpcClient = pb.NewAirborneServiceClient(conn)
    return s.grpcClient, nil
}
```

**Patch-ready diff:**
```diff
--- a/internal/admin/server.go
+++ b/internal/admin/server.go
@@ -36,6 +36,7 @@ type Server struct {
 	grpcAddr   string
 	grpcConn   *grpc.ClientConn
 	grpcClient pb.AirborneServiceClient
+	grpcOnce   sync.Once
+	grpcErr    error
 	// ... other fields ...
 }

@@ -448,18 +449,16 @@ type debugResponse struct {
 // getGRPCClient lazily initializes the gRPC client.
 func (s *Server) getGRPCClient() (pb.AirborneServiceClient, error) {
-	if s.grpcClient != nil {
-		return s.grpcClient, nil
-	}
-
-	if s.grpcAddr == "" {
-		return nil, fmt.Errorf("gRPC address not configured")
-	}
-
-	conn, err := grpc.NewClient(s.grpcAddr,
-		grpc.WithTransportCredentials(insecure.NewCredentials()),
-	)
-	if err != nil {
-		return nil, fmt.Errorf("failed to connect to gRPC server: %w", err)
-	}
-
-	s.grpcConn = conn
-	s.grpcClient = pb.NewAirborneServiceClient(conn)
+	s.grpcOnce.Do(func() {
+		if s.grpcAddr == "" {
+			s.grpcErr = fmt.Errorf("gRPC address not configured")
+			return
+		}
+		conn, err := grpc.NewClient(s.grpcAddr,
+			grpc.WithTransportCredentials(insecure.NewCredentials()),
+		)
+		if err != nil {
+			s.grpcErr = fmt.Errorf("failed to connect to gRPC server: %w", err)
+			return
+		}
+		s.grpcConn = conn
+		s.grpcClient = pb.NewAirborneServiceClient(conn)
+	})
+	if s.grpcErr != nil {
+		return nil, s.grpcErr
+	}
 	return s.grpcClient, nil
 }
```

---

### 4. Background Persistence Goroutines Have No Lifecycle Management
**File:** `internal/service/chat.go:1115-1118`
**Severity:** HIGH

Background goroutines for database persistence are spawned without any `sync.WaitGroup` or shutdown coordination. On server shutdown, these goroutines may be killed mid-write, causing partial or lost data.

**Current code:**
```go
// Line 1115
go func() {
    persistCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()
    // ... persistence logic ...
}()
```

**Patch-ready diff:**
```diff
--- a/internal/service/chat.go
+++ b/internal/service/chat.go
@@ -50,6 +50,7 @@ type AirborneService struct {
 	providers    map[string]provider.Provider
 	dbClient     *db.Client
+	persistWg    sync.WaitGroup
 	// ... other fields ...
 }

@@ -1114,7 +1115,9 @@ func (s *AirborneService) persistResult(...) {

 	// Run persistence in background goroutine
+	s.persistWg.Add(1)
 	go func() {
+		defer s.persistWg.Done()
 		persistCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
 		defer cancel()
 		// ... existing persistence logic ...
 	}()
 }
+
+// WaitForPendingPersistence blocks until all background persistence goroutines complete.
+// Should be called during graceful shutdown.
+func (s *AirborneService) WaitForPendingPersistence() {
+	s.persistWg.Wait()
+}
```

---

### 5. Missing Indexes on `provider_store_id` in File Upload Tables
**File:** `migrations/005_tenant_files_tables.sql`
**Severity:** HIGH

The `file_provider_uploads` tables (for ai8, email4ai, zztest tenants) have a `provider_store_id` column used to match files across providers, but no index exists on this column. Queries filtering by provider store will do full table scans.

**Patch-ready diff:**
```diff
--- a/migrations/005_tenant_files_tables.sql
+++ b/migrations/005_tenant_files_tables.sql
@@ -55,6 +55,7 @@
 CREATE INDEX IF NOT EXISTS idx_ai8_file_uploads_file ON ai8_airborne_file_provider_uploads(file_id);
+CREATE INDEX IF NOT EXISTS idx_ai8_file_uploads_provider_store ON ai8_airborne_file_provider_uploads(provider_store_id);

@@ -121,6 +122,7 @@
 CREATE INDEX IF NOT EXISTS idx_email4ai_file_uploads_file ON email4ai_airborne_file_provider_uploads(file_id);
+CREATE INDEX IF NOT EXISTS idx_email4ai_file_uploads_provider_store ON email4ai_airborne_file_provider_uploads(provider_store_id);

@@ -187,6 +189,7 @@
 CREATE INDEX IF NOT EXISTS idx_zztest_file_uploads_file ON zztest_airborne_file_provider_uploads(file_id);
+CREATE INDEX IF NOT EXISTS idx_zztest_file_uploads_provider_store ON zztest_airborne_file_provider_uploads(provider_store_id);
```

---

## MEDIUM Severity Issues

### 6. Doppler Secret Cache Has TOCTOU Race Condition
**File:** `internal/tenant/doppler.go:77-98`
**Severity:** MEDIUM

The `fetchProjectSecrets` method uses read-lock to check cache, releases it, then acquires write-lock to update. Between the two locks, another goroutine can also miss the cache and make a redundant API call. This doesn't cause data corruption but wastes API calls.

**Current code:**
```go
c.mu.RLock()
if secrets, ok := c.cache[project]; ok {
    c.mu.RUnlock()
    return secrets, nil
}
c.mu.RUnlock()

secrets, err := c.fetchWithRetry(project)  // <-- Duplicate calls possible
// ...
c.mu.Lock()
c.cache[project] = secrets
c.mu.Unlock()
```

**Patch-ready diff:**
```diff
--- a/internal/tenant/doppler.go
+++ b/internal/tenant/doppler.go
@@ -77,17 +77,19 @@ func (c *dopplerClient) fetchProjectSecrets(project string) (map[string]string,
-	c.mu.RLock()
-	if secrets, ok := c.cache[project]; ok {
-		c.mu.RUnlock()
-		return secrets, nil
-	}
-	c.mu.RUnlock()
-
-	secrets, err := c.fetchWithRetry(project)
-	if err != nil {
-		return nil, err
+	c.mu.Lock()
+	defer c.mu.Unlock()
+
+	if secrets, ok := c.cache[project]; ok {
+		return secrets, nil
 	}

-	c.mu.Lock()
+	// Fetch while holding lock to prevent duplicate API calls.
+	// Acceptable since this only runs at startup.
+	secrets, err := c.fetchWithRetry(project)
+	if err != nil {
+		return nil, err
+	}
 	c.cache[project] = secrets
-	c.mu.Unlock()
-
 	return secrets, nil
 }
```

---

### 7. `fmt.Sscanf` Error Silently Ignored for Thinking Budget
**File:** `internal/provider/anthropic/client.go:93`
**Severity:** MEDIUM

If `thinking_budget` contains a non-numeric string, `fmt.Sscanf` fails silently and `thinkingBudget` remains 0, which is then passed to the Anthropic API. A budget of 0 may cause unexpected behavior or API errors.

**Current code:**
```go
fmt.Sscanf(budgetStr, "%d", &thinkingBudget)
```

**Patch-ready diff:**
```diff
--- a/internal/provider/anthropic/client.go
+++ b/internal/provider/anthropic/client.go
@@ -92,1 +92,4 @@
-		fmt.Sscanf(budgetStr, "%d", &thinkingBudget)
+		if _, err := fmt.Sscanf(budgetStr, "%d", &thinkingBudget); err != nil {
+			slog.Warn("invalid thinking_budget value, using default", "value", budgetStr, "error", err)
+			thinkingBudget = 1024
+		}
```

---

### 8. Anthropic Streaming: Inconsistent Indentation (Cosmetic, but Fragile)
**File:** `internal/provider/anthropic/client.go:373-375`
**Severity:** MEDIUM (Code Smell)

The error handling block inside the streaming loop has inconsistent indentation. While functionally correct (the `slog.Warn` IS inside the `if` block), the misalignment makes the code confusing to read and error-prone for future modifications.

**Current code:**
```go
		if err := message.Accumulate(event); err != nil {
		slog.Warn("failed to accumulate stream event", "error", err)
	}
```

**Patch-ready diff:**
```diff
--- a/internal/provider/anthropic/client.go
+++ b/internal/provider/anthropic/client.go
@@ -373,3 +373,3 @@
 			if err := message.Accumulate(event); err != nil {
-			slog.Warn("failed to accumulate stream event", "error", err)
-		}
+				slog.Warn("failed to accumulate stream event", "error", err)
+			}
```

---

### 9. Ignored Error in Docbox Extractor Fallback Path
**File:** `internal/rag/extractor/docbox.go:175-178`
**Severity:** MEDIUM

When JSON decode of an error response fails, the code falls through to `io.ReadAll(resp.Body)` where the error is discarded with `_`. If the body was already partially consumed by the failed JSON decode, `io.ReadAll` may return incomplete data.

**Patch-ready diff:**
```diff
--- a/internal/rag/extractor/docbox.go
+++ b/internal/rag/extractor/docbox.go
@@ -175,4 +175,7 @@
 	var errorResp struct { Detail string `json:"detail"` }
 	if err := json.NewDecoder(resp.Body).Decode(&errorResp); err == nil && errorResp.Detail != "" {
 		return nil, fmt.Errorf("pandoc error: %s", errorResp.Detail)
 	}
-	body, _ := io.ReadAll(resp.Body)
+	body, err := io.ReadAll(resp.Body)
+	if err != nil {
+		return nil, fmt.Errorf("failed to read error response body (status %d): %w", resp.StatusCode, err)
+	}
```

---

### 10. Unchecked `io.ReadAll` Errors in Qdrant Client
**File:** `internal/rag/vectorstore/qdrant.go:80, 226`
**Severity:** MEDIUM

Errors from `io.ReadAll()` are discarded with `_` when reading error response bodies. If the read fails, empty error messages are returned, masking the actual problem.

**Patch-ready diff:**
```diff
--- a/internal/rag/vectorstore/qdrant.go
+++ b/internal/rag/vectorstore/qdrant.go
@@ -80,1 +80,5 @@
-	body, _ := io.ReadAll(resp.Body)
+	body, readErr := io.ReadAll(resp.Body)
+	if readErr != nil {
+		return false, fmt.Errorf("qdrant error (status %d, body unreadable: %v)", resp.StatusCode, readErr)
+	}
```

---

## LOW Severity Issues

### 11. Job Tables Lack Retry Limits
**File:** `migrations/008_solstice_jobs_tables.sql`
**Severity:** LOW

Job tables track `retry_count INT DEFAULT 0` but have no constraint preventing infinite retries. A stuck job could be retried indefinitely. Consider adding `CHECK (retry_count <= 10)`.

### 12. `DECIMAL(10,6)` Precision May Be Insufficient for High-Volume Tenants
**File:** `migrations/001_initial_schema.sql:70`
**Severity:** LOW

`cost_usd DECIMAL(10, 6)` caps at $9,999.999999 per thread. Enterprise tenants with large AI workloads could potentially exceed this. Widening to `DECIMAL(12, 6)` would be a low-risk improvement.

### 13. Golang Build Image Not Pinned
**File:** `Dockerfile:5`
**Severity:** LOW

Uses `golang:1.25-alpine` without pinning the Alpine version, while the runtime stage pins `alpine:3.21`. For reproducible builds, use `golang:1.25.5-alpine3.21`.

### 14. Missing `buf.lock` in Version Control
**Severity:** LOW

`buf.yaml` exists but `buf.lock` is not committed. This can cause non-reproducible protobuf builds if dependencies change upstream.

### 15. `last_full_response TEXT` Column Has No Size Guard
**File:** `migrations/007_solstice_thread_extensions.sql:44`
**Severity:** LOW

Stores entire AI responses as unbounded TEXT. PostgreSQL handles this via TOAST, but exceptionally large responses could cause performance issues. Consider documenting expected max size or adding a CHECK constraint.

---

## Positive Observations

The codebase demonstrates strong engineering in several areas:

- **Security posture**: Non-root Docker user, bcrypt for API keys, SSRF protection via chassis-go-addons, proper TLS support
- **Architecture**: Clean provider abstraction with consistent interface across 20+ providers; good separation between service/server/admin layers
- **Observability**: Comprehensive OpenTelemetry integration with tracing and metrics
- **Multi-tenancy**: Well-designed tenant isolation with prefix-based table partitioning
- **Error handling**: Generally thorough with structured logging throughout
- **Auth system**: Redis-backed API key auth with rate limiting via atomic Lua scripts is well-implemented
- **RAG pipeline**: Well-factored with chunker/embedder/extractor/vectorstore abstractions

---

## Summary

| Severity | Count | Key Themes |
|----------|-------|------------|
| CRITICAL | 2 | Dockerfile port mismatch, migration targets wrong tables |
| HIGH | 3 | Race condition in gRPC init, goroutine lifecycle, missing indexes |
| MEDIUM | 5 | TOCTOU cache, ignored errors, code formatting |
| LOW | 5 | Schema constraints, Docker pinning, build reproducibility |
| **Total** | **15** | |

The codebase is a well-architected production system with strong fundamentals. The critical issues are localized and easily fixable. The high-severity race condition in the admin server's gRPC client initialization is the most impactful runtime bug. The migration issue (006) should be addressed before any fresh database deployment.
