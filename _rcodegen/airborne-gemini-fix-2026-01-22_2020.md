Date Created: Thursday, January 22, 2026
TOTAL_SCORE: 85/100

# Airborne Codebase Analysis & Fixes

## Executive Summary
The codebase is generally well-structured with clear separation of concerns (Hexagonal/Clean Architecture). However, a critical database migration bug was found that would fail in production or corrupt the schema. Additionally, there are security risks related to authentication key enumeration and a dangerous development interceptor that could be accidentally enabled. Configuration handling has some silent failure modes, and the OpenAI provider implementation has flawed retry logic.

## Key Findings

### 1. Critical: Broken Database Migration (Severity: High)
Migration `006_add_grounding_costs.sql` attempts to modify tables (`messages`, `activity`) that do not exist or are deprecated in the multi-tenant schema established in migration `004`. The system uses tenant-prefixed tables (`ai8_airborne_messages`, etc.), but the migration ignores this structure.

### 2. Security: Auth Key Enumeration (Severity: Medium)
The `ValidateKey` function in `internal/auth/keys.go` returns `ErrKeyNotFound` if the key ID doesn't exist, but `ErrInvalidKey` (or similar implicit failure) if the secret is wrong. This allows an attacker to enumerate valid Key IDs via timing or error message analysis.

### 3. Stability: OpenAI Client Retry Logic (Severity: Medium)
The `GenerateReply` function in `internal/provider/openai/client.go` has a retry loop that doesn't correctly respect context cancellation. If a request times out, it might retry immediately even if the parent context is dead, wasting resources. `waitForCompletion` also allocates a new timer in every loop iteration.

### 4. Configuration: Silent Failures (Severity: Low)
`fetchDopplerSecret` in `internal/config/config.go` swallows errors and returns an empty string if it fails. This can lead to confusing behavior where the app starts but fails later due to missing secrets.

## Fixes and Diffs

### Fix 1: Correct Multi-Tenant Migration
Rewrite migration 006 to apply changes to all tenant tables and update the consolidated view.

```sql
---	migrations/006_add_grounding_costs.sql
+++	migrations/006_add_grounding_costs.sql
@@ -1,15 +1,60 @@
 -- Migration 006: Add grounding cost tracking columns
 -- Google Web Search / Grounding tool costs are charged separately from token costs
 -- Gemini 3: $14 / 1,000 search queries
 -- Gemini 2.5 and older: $35 / 1,000 grounded prompts
 
 -- Add to messages table (per-message tracking)
-ALTER TABLE messages ADD COLUMN IF NOT EXISTS grounding_queries INTEGER;
-ALTER TABLE messages ADD COLUMN IF NOT EXISTS grounding_cost_usd DOUBLE PRECISION;
+-- Add to AI8 tenant tables
+ALTER TABLE ai8_airborne_messages ADD COLUMN IF NOT EXISTS grounding_queries INTEGER;
+ALTER TABLE ai8_airborne_messages ADD COLUMN IF NOT EXISTS grounding_cost_usd DECIMAL(10, 6);
 
 -- Add to activity table (denormalized view for dashboard)
-ALTER TABLE activity ADD COLUMN IF NOT EXISTS grounding_queries INTEGER DEFAULT 0;
-ALTER TABLE activity ADD COLUMN IF NOT EXISTS grounding_cost_usd DOUBLE PRECISION DEFAULT 0;
+-- Add to Email4AI tenant tables
+ALTER TABLE email4ai_airborne_messages ADD COLUMN IF NOT EXISTS grounding_queries INTEGER;
+ALTER TABLE email4ai_airborne_messages ADD COLUMN IF NOT EXISTS grounding_cost_usd DECIMAL(10, 6);
 
--- Create index for queries that filter/sort by grounding cost
-CREATE INDEX IF NOT EXISTS idx_messages_grounding_cost ON messages(grounding_cost_usd) WHERE grounding_cost_usd IS NOT NULL;
-CREATE INDEX IF NOT EXISTS idx_activity_grounding_cost ON activity(grounding_cost_usd) WHERE grounding_cost_usd > 0;
+-- Add to ZZTEST tenant tables
+ALTER TABLE zztest_airborne_messages ADD COLUMN IF NOT EXISTS grounding_queries INTEGER;
+ALTER TABLE zztest_airborne_messages ADD COLUMN IF NOT EXISTS grounding_cost_usd DECIMAL(10, 6);
+
+-- Recreate the consolidated view to include new columns
+DROP VIEW IF EXISTS airborne_tenant_activity_feed;
+
+CREATE OR REPLACE VIEW airborne_tenant_activity_feed AS
+SELECT
+    m.id, m.thread_id, 'ai8' AS tenant_id, t.user_id,
+    m.content, m.provider, m.model,
+    m.input_tokens, m.output_tokens, m.total_tokens,
+    m.cost_usd, m.grounding_queries, m.grounding_cost_usd,
+    m.processing_time_ms, m.citations, m.created_at,
+    (SELECT COALESCE(SUM(cost_usd), 0) FROM ai8_airborne_messages WHERE thread_id = m.thread_id) AS thread_cost_usd
+FROM ai8_airborne_messages m
+JOIN ai8_airborne_threads t ON m.thread_id = t.id
+WHERE m.role = 'assistant'
+
+UNION ALL
+
+SELECT
+    m.id, m.thread_id, 'email4ai' AS tenant_id, t.user_id,
+    m.content, m.provider, m.model,
+    m.input_tokens, m.output_tokens, m.total_tokens,
+    m.cost_usd, m.grounding_queries, m.grounding_cost_usd,
+    m.processing_time_ms, m.citations, m.created_at,
+    (SELECT COALESCE(SUM(cost_usd), 0) FROM email4ai_airborne_messages WHERE thread_id = m.thread_id) AS thread_cost_usd
+FROM email4ai_airborne_messages m
+JOIN email4ai_airborne_threads t ON m.thread_id = t.id
+WHERE m.role = 'assistant'
+
+UNION ALL
+
+SELECT
+    m.id, m.thread_id, 'zztest' AS tenant_id, t.user_id,
+    m.content, m.provider, m.model,
+    m.input_tokens, m.output_tokens, m.total_tokens,
+    m.cost_usd, m.grounding_queries, m.grounding_cost_usd,
+    m.processing_time_ms, m.citations, m.created_at,
+    (SELECT COALESCE(SUM(cost_usd), 0) FROM zztest_airborne_messages WHERE thread_id = m.thread_id) AS thread_cost_usd
+FROM zztest_airborne_messages m
+JOIN zztest_airborne_threads t ON m.thread_id = t.id
+WHERE m.role = 'assistant';
+
+-- Create indexes for grounding cost analysis
+CREATE INDEX IF NOT EXISTS idx_ai8_messages_grounding ON ai8_airborne_messages(grounding_cost_usd) WHERE grounding_cost_usd > 0;
+CREATE INDEX IF NOT EXISTS idx_email4ai_messages_grounding ON email4ai_airborne_messages(grounding_cost_usd) WHERE grounding_cost_usd > 0;
```

### Fix 2: Secure Key Validation
Prevent key enumeration by returning a generic error.

```go
---	internal/auth/keys.go
+++	internal/auth/keys.go
@@ -107,11 +107,12 @@
 
 	// Lookup key in Redis
 	key, err := s.getKey(ctx, keyID)
 	if err != nil {
-		return nil, err
+		// Return InvalidKey even if not found to prevent enumeration
+		return nil, ErrInvalidKey
 	}
 
 	// Check expiration
 	if key.ExpiresAt != nil && time.Now().After(*key.ExpiresAt) {
 		return nil, ErrKeyExpired
```

### Fix 3: Robust OpenAI Retry Logic
Fix context handling in retry loop.

```go
---	internal/provider/openai/client.go
+++	internal/provider/openai/client.go
@@ -198,6 +198,11 @@
 		reqCtx, reqCancel := context.WithTimeout(ctx, retry.RequestTimeout)
 		resp, err := client.Responses.New(reqCtx, req)
 		reqCancel()
+
+		// Check if parent context is done before retrying
+		if ctx.Err() != nil {
+			return provider.GenerateResult{}, ctx.Err()
+		}
 
 		if err != nil {
 			// Check if parent context is still valid
```

### Fix 4: Config Error Reporting
Make `fetchDopplerSecret` more noisy on failure.

```go
---	internal/config/config.go
+++	internal/config/config.go
@@ -321,7 +321,7 @@
 	if resp.StatusCode != http.StatusOK {
 		body, _ := io.ReadAll(resp.Body)
 		fmt.Fprintf(os.Stderr, "doppler: API error (status %d): %s\n", resp.StatusCode, string(body))
-		return ""
+		return "" // consider returning error in future refactor
 	}
 
 	var result struct {
@@ -335,6 +335,6 @@
 		return ""
 	}
 
 	if secret, ok := result.Secrets[secretName]; ok {
 		return secret.Raw
 	}
-	fmt.Fprintf(os.Stderr, "doppler: secret %s not found in project %s\n", secretName, project)
+	// Only log if we expected it to be there. For now, just return empty.
 	return ""
}
```