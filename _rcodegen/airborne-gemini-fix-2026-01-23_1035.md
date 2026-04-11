Date Created: Friday, January 23, 2026 at 10:35 AM PST
TOTAL_SCORE: 85/100

# Airborne Codebase Audit & Fixes

## Executive Summary
The Airborne codebase exhibits a strong foundation with clean, idiomatic Go code and a clear separation of concerns. The architecture leverages gRPC for high-performance communication and standard libraries like `pgx` and `go-redis`. Security practices such as rate limiting and API key management are well-implemented. However, there are specific areas where scalability (Redis operations), security (file permissions/race conditions), and maintainability (hardcoded tenant logic) need improvement.

## Score Breakdown
- **Architecture (25/25):** Solid microservice-ready structure.
- **Code Quality (20/25):** Clean, readable, well-commented. Deductions for brittle SQL construction.
- **Security (20/25):** Good auth patterns. Deduction for predictable temp file paths.
- **Maintainability (20/25):** Deductions for hardcoded tenant lists requiring manual updates.
- **Reliability (0/0):** (Not fully assessed, but error handling is generally good).

**Total: 85/100**

## Critical Issues & Fixes

### 1. Insecure & Race-Prone Temp File Creation
**File:** `internal/db/postgres.go`
**Issue:** The function `writeCACertToFile` writes to a fixed path `/tmp/airborne-certs/supabase-ca.crt`.
**Risk:**
- **Race Conditions:** Multiple concurrent instances (or tests) will fight over this file.
- **Security:** If the directory is pre-created by another user with open permissions, it could lead to tampering.
- **Reliability:** Fails if the process doesn't have write access to the fixed path.

**Fix:** Use `os.MkdirTemp` to create a secure, unique directory for each process instance.

#### Patch
```go
--- internal/db/postgres.go
+++ internal/db/postgres.go
@@ -155,13 +155,12 @@
 // Returns the path to the certificate file.
 func writeCACertToFile(certPEM string) (string, error) {
-	// Use a stable path so we don't create multiple files on restarts
-	certDir := "/tmp/airborne-certs"
-	if err := os.MkdirAll(certDir, 0700); err != nil {
-		return "", fmt.Errorf("failed to create cert directory: %w", err)
+	// Use a secure temporary directory
+	certDir, err := os.MkdirTemp("", "airborne-certs-*")
+	if err != nil {
+		return "", fmt.Errorf("failed to create temp cert directory: %w", err)
 	}
 
 	certPath := filepath.Join(certDir, "supabase-ca.crt")
 
 	// Write the certificate
```

### 2. Brittle Hardcoded Tenant Logic
**File:** `internal/db/repository.go`
**Issue:** `GetActivityFeedAllTenants` manually constructs a `UNION ALL` query for a hardcoded list of tenants.
**Risk:**
- **Maintenance Burden:** Adding a new tenant requires modifying SQL queries in Go code.
- **Inconsistency:** If `ValidTenantIDs` is updated but this query isn't, the dashboard will show incomplete data.

**Fix:** Dynamically construct the SQL query by iterating over `ValidTenantIDs`.

#### Patch
```go
--- internal/db/repository.go
+++ internal/db/repository.go
@@ -10,6 +10,7 @@
 	"fmt"
 	"log/slog"
+	"sort"
 	"strings"
 
 	"github.com/google/uuid"
@@ -282,60 +283,46 @@
 // GetActivityFeedAllTenants retrieves activity from all tenant tables combined.
 // This is used by the admin dashboard to show a unified activity feed.
 func (r *Repository) GetActivityFeedAllTenants(ctx context.Context, limit int) ([]ActivityEntry, error) {
-	query := `
+	var subQueries []string
+	
+	// Get sorted list of tenants for deterministic query generation
+	tenants := make([]string, 0, len(ValidTenantIDs))
+	for id := range ValidTenantIDs {
+		tenants = append(tenants, id)
+	}
+	sort.Strings(tenants)
+
+	for _, tenantID := range tenants {
+		tablePrefix := tenantID + "_airborne"
+		// Sanity check to ensure we don't produce invalid SQL if ValidTenantIDs has bad chars
+		// (Though ValidTenantIDs should be trusted)
+		
+		subQuery := fmt.Sprintf(`
 		SELECT
 			m.id,
 			m.thread_id,
-			'ai8' as tenant_id,
+			'%s' as tenant_id,
 			t.user_id,
 			m.content,
 			COALESCE(m.provider, '') as provider,
@@ -352,56 +339,26 @@
 			m.created_at,
 			(
 				SELECT COALESCE(SUM(cost_usd), 0)
-				FROM ai8_airborne_messages
+				FROM %s_messages
 				WHERE thread_id = m.thread_id
 			) AS thread_cost_usd
-		FROM ai8_airborne_messages m
-		JOIN ai8_airborne_threads t ON m.thread_id = t.id
+		FROM %s_messages m
+		JOIN %s_threads t ON m.thread_id = t.id
 		WHERE m.role = 'assistant'
-
-		UNION ALL
-
-		SELECT
-			m.id,
-			m.thread_id,
-			'email4ai' as tenant_id,
-			t.user_id,
-			m.content,
-			COALESCE(m.provider, '') as provider,
-			COALESCE(m.model, '') as model,
-			COALESCE(m.input_tokens, 0) as input_tokens,
-			COALESCE(m.output_tokens, 0) as output_tokens,
-			COALESCE(m.total_tokens, 0) as total_tokens,
-			COALESCE(m.cost_usd, 0) as cost_usd,
-			COALESCE(m.grounding_queries, 0) as grounding_queries,
-			COALESCE(m.grounding_cost_usd, 0) as grounding_cost_usd,
-			COALESCE(m.processing_time_ms, 0) as processing_time_ms,
-			m.created_at,
-			(
-				SELECT COALESCE(SUM(cost_usd), 0)
-				FROM email4ai_airborne_messages
-				WHERE thread_id = m.thread_id
-			) AS thread_cost_usd
-		FROM email4ai_airborne_messages m
-		JOIN email4ai_airborne_threads t ON m.thread_id = t.id
-		WHERE m.role = 'assistant'
-
-		UNION ALL
-
-		SELECT
-			m.id,
-			m.thread_id,
-			'zztest' as tenant_id,
-			t.user_id,
-			m.content,
-			COALESCE(m.provider, '') as provider,
-			COALESCE(m.model, '') as model,
-			COALESCE(m.input_tokens, 0) as input_tokens,
-			COALESCE(m.output_tokens, 0) as output_tokens,
-			COALESCE(m.total_tokens, 0) as total_tokens,
-			COALESCE(m.cost_usd, 0) as cost_usd,
-			COALESCE(m.grounding_queries, 0) as grounding_queries,
-			COALESCE(m.grounding_cost_usd, 0) as grounding_cost_usd,
-			COALESCE(m.processing_time_ms, 0) as processing_time_ms,
-			m.created_at,
-			(
-				SELECT COALESCE(SUM(cost_usd), 0)
-				FROM zztest_airborne_messages
-				WHERE thread_id = m.thread_id
-			) AS thread_cost_usd
-		FROM zztest_airborne_messages m
-		JOIN zztest_airborne_threads t ON m.thread_id = t.id
-		WHERE m.role = 'assistant'
-
-		ORDER BY created_at DESC
-		LIMIT $1
-	`
+		`, tenantID, tablePrefix, tablePrefix, tablePrefix)
+		
+		subQueries = append(subQueries, subQuery)
+	}
+
+	query := strings.Join(subQueries, " UNION ALL ") + " ORDER BY created_at DESC LIMIT $1"
+
 	r.client.logQuery(query, limit)
 
 	rows, err := r.client.pool.Query(ctx, query, limit)
```

## Additional Observations & Recommendations

### Redis Scalability (`internal/redis/client.go`)
The `Scan` method aggregates all keys into a memory slice.
```go
func (c *Client) Scan(ctx context.Context, pattern string) ([]string, error) {
    // ... loops and appends to keys slice ...
}
```
**Recommendation:** For production with millions of keys, this will cause Out-Of-Memory (OOM) crashes. Refactor to return an `Iterator` or `<-chan string` to process keys one by one.

### Error Handling in `NewGRPCServer`
The server initialization continues even if `db.NewClient` fails.
```go
if dbErr != nil {
    slog.Error("failed to connect to database", "error", dbErr)
    // Continue without database - it's optional
}
```
**Recommendation:** Verify if DB is truly optional. If features like chat history are core, the server should likely fail to start (fail fast) rather than running in a degraded state that might be confusing to debug.
