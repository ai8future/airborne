Date Created: 2026-01-22 19:50

# Airborne Codebase Analysis

## 1. AUDIT

### Issue: Dynamic SQL Generation with Hardcoded Tenants
**Severity:** High
**Location:** `internal/db/repository.go`

The `GetActivityFeedAllTenants` method manually constructs a massive UNION query with hardcoded tenant IDs. This violates the Single Responsibility Principle and creates a maintenance hazard where adding a tenant requires modifying SQL queries in multiple places. It also risks SQL injection if the list of tenants is ever dynamic, though currently it is hardcoded.

**Recommendation:** Dynamically build the query based on the `ValidTenantIDs` map or a configuration source.

#### Patch-Ready Diff
```go
<<<<
// GetActivityFeedAllTenants retrieves activity from all tenant tables combined.
// This is used by the admin dashboard to show a unified activity feed.
func (r *Repository) GetActivityFeedAllTenants(ctx context.Context, limit int) ([]ActivityEntry, error) {
	query := `
		SELECT
			m.id,
			m.thread_id,
			'ai8' as tenant_id,
			t.user_id,
			m.content,
			COALESCE(m.provider, '') as provider,
			COALESCE(m.model, '') as model,
			COALESCE(m.input_tokens, 0) as input_tokens,
			COALESCE(m.output_tokens, 0) as output_tokens,
			COALESCE(m.total_tokens, 0) as total_tokens,
			COALESCE(m.cost_usd, 0) as cost_usd,
			COALESCE(m.grounding_queries, 0) as grounding_queries,
			COALESCE(m.grounding_cost_usd, 0) as grounding_cost_usd,
			COALESCE(m.processing_time_ms, 0) as processing_time_ms,
			m.created_at,
			(
				SELECT COALESCE(SUM(cost_usd), 0)
				FROM ai8_airborne_messages
				WHERE thread_id = m.thread_id
			) AS thread_cost_usd
		FROM ai8_airborne_messages m
		JOIN ai8_airborne_threads t ON m.thread_id = t.id
		WHERE m.role = 'assistant'

		UNION ALL

		SELECT
			m.id,
			m.thread_id,
			'email4ai' as tenant_id,
			t.user_id,
			m.content,
			COALESCE(m.provider, '') as provider,
			COALESCE(m.model, '') as model,
			COALESCE(m.input_tokens, 0) as input_tokens,
			COALESCE(m.output_tokens, 0) as output_tokens,
			COALESCE(m.total_tokens, 0) as total_tokens,
			COALESCE(m.cost_usd, 0) as cost_usd,
			COALESCE(m.grounding_queries, 0) as grounding_queries,
			COALESCE(m.grounding_cost_usd, 0) as grounding_cost_usd,
			COALESCE(m.processing_time_ms, 0) as processing_time_ms,
			m.created_at,
			(
				SELECT COALESCE(SUM(cost_usd), 0)
				FROM email4ai_airborne_messages
				WHERE thread_id = m.thread_id
			) AS thread_cost_usd
		FROM email4ai_airborne_messages m
		JOIN email4ai_airborne_threads t ON m.thread_id = t.id
		WHERE m.role = 'assistant'

		UNION ALL

		SELECT
			m.id,
			m.thread_id,
			'zztest' as tenant_id,
			t.user_id,
			m.content,
			COALESCE(m.provider, '') as provider,
			COALESCE(m.model, '') as model,
			COALESCE(m.input_tokens, 0) as input_tokens,
			COALESCE(m.output_tokens, 0) as output_tokens,
			COALESCE(m.total_tokens, 0) as total_tokens,
			COALESCE(m.cost_usd, 0) as cost_usd,
			COALESCE(m.grounding_queries, 0) as grounding_queries,
			COALESCE(m.grounding_cost_usd, 0) as grounding_cost_usd,
			COALESCE(m.processing_time_ms, 0) as processing_time_ms,
			m.created_at,
			(
				SELECT COALESCE(SUM(cost_usd), 0)
				FROM zztest_airborne_messages
				WHERE thread_id = m.thread_id
			) AS thread_cost_usd
		FROM zztest_airborne_messages m
		JOIN zztest_airborne_threads t ON m.thread_id = t.id
		WHERE m.role = 'assistant'

		ORDER BY created_at DESC
		LIMIT $1
	`
	r.client.logQuery(query, limit)

	rows, err := r.client.pool.Query(ctx, query, limit)
====
// GetActivityFeedAllTenants retrieves activity from all tenant tables combined.
// This is used by the admin dashboard to show a unified activity feed.
func (r *Repository) GetActivityFeedAllTenants(ctx context.Context, limit int) ([]ActivityEntry, error) {
	var queries []string
	for tenantID := range ValidTenantIDs {
		// Safe because ValidTenantIDs keys are hardcoded and trusted
		tablePrefix := tenantID + "_airborne"
		q := fmt.Sprintf(`
		SELECT
			m.id, m.thread_id, '%[1]s' as tenant_id, t.user_id, m.content,
			COALESCE(m.provider, '') as provider, COALESCE(m.model, '') as model,
			COALESCE(m.input_tokens, 0) as input_tokens, COALESCE(m.output_tokens, 0) as output_tokens,
			COALESCE(m.total_tokens, 0) as total_tokens, COALESCE(m.cost_usd, 0) as cost_usd,
			COALESCE(m.grounding_queries, 0) as grounding_queries,
			COALESCE(m.grounding_cost_usd, 0) as grounding_cost_usd,
			COALESCE(m.processing_time_ms, 0) as processing_time_ms,
			m.created_at,
			(SELECT COALESCE(SUM(cost_usd), 0) FROM %[2]s_messages WHERE thread_id = m.thread_id) AS thread_cost_usd
		FROM %[2]s_messages m
		JOIN %[2]s_threads t ON m.thread_id = t.id
		WHERE m.role = 'assistant'`, tenantID, tablePrefix)
		queries = append(queries, q)
	}

	finalQuery := strings.Join(queries, " UNION ALL ") + " ORDER BY created_at DESC LIMIT $1"
	r.client.logQuery(finalQuery, limit)

	rows, err := r.client.pool.Query(ctx, finalQuery, limit)
>>>>
```

## 2. TESTS

### Issue: Missing Unit Tests for Repository Helper Methods
**Severity:** Medium
**Location:** `internal/db/repository_test.go` (New File)

The `internal/db/repository.go` file contains critical logic for determining table names based on tenant IDs. There are no tests to verify that these table names are generated correctly, especially for the "legacy" empty prefix case.

#### Patch-Ready Diff
```go
<<<<
(New File)
====
package db

import (
	"testing"
)

func TestRepository_TableNames(t *testing.T) {
	tests := []struct {
		name        string
		tenantID    string
		wantPrefix  string
		expectError bool
	}{
		{"ValidTenant_Ai8", "ai8", "ai8_airborne", false},
		{"ValidTenant_Email4Ai", "email4ai", "email4ai_airborne", false},
		{"ValidTenant_Test", "zztest", "zztest_airborne", false},
		{"InvalidTenant", "invalid", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo, err := NewTenantRepository(nil, tt.tenantID)
			if tt.expectError {
				if err == nil {
					t.Errorf("NewTenantRepository() expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("NewTenantRepository() error = %v", err)
			}

			if got := repo.threadsTable(); got != tt.wantPrefix+"_threads" {
				t.Errorf("threadsTable() = %v, want %v", got, tt.wantPrefix+"_threads")
			}
			if got := repo.messagesTable(); got != tt.wantPrefix+"_messages" {
				t.Errorf("messagesTable() = %v, want %v", got, tt.wantPrefix+"_messages")
			}
		})
	}
}

func TestRepository_LegacyTables(t *testing.T) {
	// Directly create struct to simulate legacy state which constructor doesn't allow anymore
	repo := &Repository{tablePrefix: ""}

	if got := repo.threadsTable(); got != "airborne_threads" {
		t.Errorf("threadsTable() legacy = %v, want airborne_threads", got)
	}
}
>>>>
```

## 3. FIXES

### Issue: Argument Explosion in `PersistConversationTurnWithDebug`
**Severity:** Medium
**Location:** `internal/db/repository.go`

The function `PersistConversationTurnWithDebug` takes 16 arguments. This makes the code hard to read, prone to argument swapping errors (e.g., swapping `groundingQueries` and `processingTimeMs` if types matched), and difficult to extend.

**Proposed Fix:** Introduce a `ConversationTurnParams` struct.

#### Patch-Ready Diff
```go
<<<<
// PersistConversationTurn saves both user and assistant messages in a transaction.
// This is the main entry point for chat service persistence.
// Note: tenantID parameter is no longer needed - the repository is already scoped to a tenant.
func (r *Repository) PersistConversationTurn(ctx context.Context, threadID uuid.UUID, userID string, userContent, assistantContent, provider, model, responseID string, inputTokens, outputTokens, processingTimeMs int, costUSD float64) error {
	return r.PersistConversationTurnWithDebug(ctx, threadID, userID, userContent, assistantContent, provider, model, responseID, inputTokens, outputTokens, processingTimeMs, costUSD, 0, 0, nil, nil)
}

// PersistConversationTurnWithDebug saves both user and assistant messages with optional debug data and citations.
func (r *Repository) PersistConversationTurnWithDebug(ctx context.Context, threadID uuid.UUID, userID string, userContent, assistantContent, provider, model, responseID string, inputTokens, outputTokens, processingTimeMs int, costUSD float64, groundingQueries int, groundingCostUSD float64, debug *DebugInfo, citations []Citation) error {
	tx, err := r.client.pool.Begin(ctx)
====
// ConversationTurnParams holds all parameters for persisting a conversation turn.
type ConversationTurnParams struct {
	ThreadID         uuid.UUID
	UserID           string
	UserContent      string
	AssistantContent string
	Provider         string
	Model            string
	ResponseID       string
	InputTokens      int
	OutputTokens     int
	ProcessingTimeMs int
	CostUSD          float64
	GroundingQueries int
	GroundingCostUSD float64
	Debug            *DebugInfo
	Citations        []Citation
}

// PersistConversationTurn saves both user and assistant messages in a transaction.
// This is the main entry point for chat service persistence.
func (r *Repository) PersistConversationTurn(ctx context.Context, params ConversationTurnParams) error {
	tx, err := r.client.pool.Begin(ctx)
	// ... (rest of implementation using params.Field instead of arguments)
>>>>
```
*(Note: Caller sites in `internal/service/chat.go` would need corresponding updates)*

## 4. REFACTOR

### 1. `internal/service/chat.go`: `prepareRequest` Complexity
The `prepareRequest` method in `ChatService` is doing too much:
- Permission checking (admin for base_url)
- Request validation
- Slash command parsing
- Provider selection
- RAG retrieval
- Parameter conversion

**Refactor:** Break this down into smaller, focused methods:
- `validateRequest(req)`
- `handleSlashCommands(req)`
- `resolveProvider(req)`
- `enrichWithRAG(req)`

### 2. `internal/db/repository.go`: Tenant ID Validation
The `ValidTenantIDs` map and `ErrInvalidTenant` error are defined in the `db` package. This couples the database layer with specific business configuration.
**Refactor:** Move tenant configuration to a `config` or `tenant` package and have the repository accept a `TenantConfig` interface or similar, decoupling the DB layer from specific tenant names.
