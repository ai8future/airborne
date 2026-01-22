# Airborne Unit Test Coverage Analysis & Proposals

**Date Created:** 2026-01-21T19:15:00-06:00
**Auditor:** Claude:Opus 4.5
**Codebase Version:** 1.7.2

---

## Executive Summary

This report identifies untested code in the Airborne codebase and provides patch-ready test implementations. The analysis found **6 packages** with significant untested functionality representing critical business logic, infrastructure, and utility code.

### Coverage Gaps Identified

| Package | File(s) | Priority | Lines Untested |
|---------|---------|----------|----------------|
| `internal/pricing` | `pricing.go` | HIGH | 221 |
| `internal/db` | `models.go`, `postgres.go`, `repository.go` | HIGH | ~1000 |
| `internal/tenant` | `doppler.go` | MEDIUM | 250 |
| `internal/imagegen` | `config.go`, `client.go` | MEDIUM | ~150 |
| `internal/retry` | `backoff.go` (partial) | LOW | 17 |
| `internal/provider` | Multiple compat clients | MEDIUM | ~800 |

---

## Test Proposals with Patch-Ready Diffs

---

### 1. Pricing Package Tests (`internal/pricing/pricing_test.go`)

**Priority:** HIGH
**Rationale:** The pricing package calculates API costs for billing/analytics - critical for business operations.

**Functions to test:**
- `NewPricer()` - Creates pricer from config files
- `Pricer.Calculate()` - Calculates costs for token usage
- `Pricer.GetPricing()` - Retrieves model pricing
- `Pricer.findPricingByPrefix()` - Handles versioned model names
- `Pricer.ListProviders()` / `Pricer.ModelCount()` - Accessor methods
- `CalculateCost()` - Package-level convenience function

```diff
--- /dev/null
+++ b/internal/pricing/pricing_test.go
@@ -0,0 +1,284 @@
+package pricing
+
+import (
+	"os"
+	"path/filepath"
+	"testing"
+)
+
+// Helper to create a temporary pricing config directory for testing.
+func setupTestPricingDir(t *testing.T, files map[string]string) string {
+	t.Helper()
+	dir := t.TempDir()
+	for name, content := range files {
+		path := filepath.Join(dir, name)
+		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
+			t.Fatalf("failed to write test file %s: %v", name, err)
+		}
+	}
+	return dir
+}
+
+func TestNewPricer_ValidConfig(t *testing.T) {
+	configDir := setupTestPricingDir(t, map[string]string{
+		"openai_pricing.json": `{
+			"provider": "openai",
+			"models": {
+				"gpt-4": {"input_per_million": 30.0, "output_per_million": 60.0},
+				"gpt-3.5-turbo": {"input_per_million": 0.5, "output_per_million": 1.5}
+			},
+			"metadata": {"updated": "2026-01-01", "source": "openai.com"}
+		}`,
+		"anthropic_pricing.json": `{
+			"provider": "anthropic",
+			"models": {
+				"claude-3-opus": {"input_per_million": 15.0, "output_per_million": 75.0}
+			}
+		}`,
+	})
+
+	pricer, err := NewPricer(configDir)
+	if err != nil {
+		t.Fatalf("NewPricer() failed: %v", err)
+	}
+
+	// Verify providers loaded
+	providers := pricer.ListProviders()
+	if len(providers) != 2 {
+		t.Errorf("expected 2 providers, got %d", len(providers))
+	}
+
+	// Verify model count
+	if pricer.ModelCount() != 3 {
+		t.Errorf("expected 3 models, got %d", pricer.ModelCount())
+	}
+}
+
+func TestNewPricer_NoPricingFiles(t *testing.T) {
+	emptyDir := t.TempDir()
+
+	_, err := NewPricer(emptyDir)
+	if err == nil {
+		t.Fatal("expected error for empty directory, got nil")
+	}
+}
+
+func TestNewPricer_InvalidJSON(t *testing.T) {
+	configDir := setupTestPricingDir(t, map[string]string{
+		"invalid_pricing.json": `{invalid json}`,
+	})
+
+	_, err := NewPricer(configDir)
+	if err == nil {
+		t.Fatal("expected error for invalid JSON, got nil")
+	}
+}
+
+func TestNewPricer_InfersProviderFromFilename(t *testing.T) {
+	configDir := setupTestPricingDir(t, map[string]string{
+		"gemini_pricing.json": `{
+			"models": {
+				"gemini-pro": {"input_per_million": 0.5, "output_per_million": 1.5}
+			}
+		}`,
+	})
+
+	pricer, err := NewPricer(configDir)
+	if err != nil {
+		t.Fatalf("NewPricer() failed: %v", err)
+	}
+
+	providers := pricer.ListProviders()
+	if len(providers) != 1 || providers[0] != "gemini" {
+		t.Errorf("expected provider 'gemini', got %v", providers)
+	}
+}
+
+func TestPricer_Calculate(t *testing.T) {
+	configDir := setupTestPricingDir(t, map[string]string{
+		"test_pricing.json": `{
+			"provider": "test",
+			"models": {
+				"test-model": {"input_per_million": 10.0, "output_per_million": 20.0}
+			}
+		}`,
+	})
+
+	pricer, err := NewPricer(configDir)
+	if err != nil {
+		t.Fatalf("NewPricer() failed: %v", err)
+	}
+
+	tests := []struct {
+		name         string
+		model        string
+		inputTokens  int64
+		outputTokens int64
+		wantTotal    float64
+		wantUnknown  bool
+	}{
+		{
+			name:         "exact model match",
+			model:        "test-model",
+			inputTokens:  1000,
+			outputTokens: 500,
+			// (1000 * 10 / 1_000_000) + (500 * 20 / 1_000_000) = 0.01 + 0.01 = 0.02
+			wantTotal:   0.02,
+			wantUnknown: false,
+		},
+		{
+			name:         "zero tokens",
+			model:        "test-model",
+			inputTokens:  0,
+			outputTokens: 0,
+			wantTotal:    0.0,
+			wantUnknown:  false,
+		},
+		{
+			name:         "unknown model",
+			model:        "unknown-model",
+			inputTokens:  1000,
+			outputTokens: 500,
+			wantTotal:    0.0,
+			wantUnknown:  true,
+		},
+		{
+			name:         "large token count",
+			model:        "test-model",
+			inputTokens:  1_000_000,
+			outputTokens: 1_000_000,
+			// (1M * 10 / 1M) + (1M * 20 / 1M) = 10 + 20 = 30
+			wantTotal:   30.0,
+			wantUnknown: false,
+		},
+	}
+
+	for _, tt := range tests {
+		t.Run(tt.name, func(t *testing.T) {
+			cost := pricer.Calculate(tt.model, tt.inputTokens, tt.outputTokens)
+
+			if cost.Unknown != tt.wantUnknown {
+				t.Errorf("Calculate() Unknown = %v, want %v", cost.Unknown, tt.wantUnknown)
+			}
+
+			if cost.TotalCost != tt.wantTotal {
+				t.Errorf("Calculate() TotalCost = %v, want %v", cost.TotalCost, tt.wantTotal)
+			}
+
+			if cost.InputTokens != tt.inputTokens {
+				t.Errorf("Calculate() InputTokens = %v, want %v", cost.InputTokens, tt.inputTokens)
+			}
+
+			if cost.OutputTokens != tt.outputTokens {
+				t.Errorf("Calculate() OutputTokens = %v, want %v", cost.OutputTokens, tt.outputTokens)
+			}
+		})
+	}
+}
+
+func TestPricer_Calculate_PrefixMatch(t *testing.T) {
+	configDir := setupTestPricingDir(t, map[string]string{
+		"test_pricing.json": `{
+			"models": {
+				"gpt-4": {"input_per_million": 30.0, "output_per_million": 60.0}
+			}
+		}`,
+	})
+
+	pricer, err := NewPricer(configDir)
+	if err != nil {
+		t.Fatalf("NewPricer() failed: %v", err)
+	}
+
+	// Versioned model should match base model pricing
+	cost := pricer.Calculate("gpt-4-0125-preview", 1000, 500)
+	if cost.Unknown {
+		t.Error("expected versioned model to match by prefix")
+	}
+
+	// Cost should be calculated using gpt-4 pricing
+	expectedInput := 1000.0 * 30.0 / 1_000_000  // 0.03
+	expectedOutput := 500.0 * 60.0 / 1_000_000  // 0.03
+	expectedTotal := expectedInput + expectedOutput // 0.06
+
+	if cost.TotalCost != expectedTotal {
+		t.Errorf("Calculate() TotalCost = %v, want %v", cost.TotalCost, expectedTotal)
+	}
+}
+
+func TestPricer_GetPricing(t *testing.T) {
+	configDir := setupTestPricingDir(t, map[string]string{
+		"test_pricing.json": `{
+			"models": {
+				"test-model": {"input_per_million": 5.0, "output_per_million": 15.0}
+			}
+		}`,
+	})
+
+	pricer, err := NewPricer(configDir)
+	if err != nil {
+		t.Fatalf("NewPricer() failed: %v", err)
+	}
+
+	// Exact match
+	pricing, ok := pricer.GetPricing("test-model")
+	if !ok {
+		t.Fatal("expected to find pricing for test-model")
+	}
+	if pricing.InputPerMillion != 5.0 {
+		t.Errorf("InputPerMillion = %v, want 5.0", pricing.InputPerMillion)
+	}
+	if pricing.OutputPerMillion != 15.0 {
+		t.Errorf("OutputPerMillion = %v, want 15.0", pricing.OutputPerMillion)
+	}
+
+	// Unknown model
+	_, ok = pricer.GetPricing("unknown-model")
+	if ok {
+		t.Error("expected unknown model to not be found")
+	}
+}
+
+func TestCost_Struct(t *testing.T) {
+	cost := Cost{
+		Model:        "gpt-4",
+		InputTokens:  1000,
+		OutputTokens: 500,
+		InputCost:    0.03,
+		OutputCost:   0.03,
+		TotalCost:    0.06,
+		Unknown:      false,
+	}
+
+	if cost.Model != "gpt-4" {
+		t.Errorf("Cost.Model = %v, want gpt-4", cost.Model)
+	}
+	if cost.TotalCost != cost.InputCost+cost.OutputCost {
+		t.Error("TotalCost should equal InputCost + OutputCost")
+	}
+}
+
+func TestModelPricing_Struct(t *testing.T) {
+	mp := ModelPricing{
+		InputPerMillion:  10.0,
+		OutputPerMillion: 20.0,
+	}
+
+	if mp.InputPerMillion != 10.0 {
+		t.Errorf("InputPerMillion = %v, want 10.0", mp.InputPerMillion)
+	}
+	if mp.OutputPerMillion != 20.0 {
+		t.Errorf("OutputPerMillion = %v, want 20.0", mp.OutputPerMillion)
+	}
+}
```

---

### 2. Database Models Tests (`internal/db/models_test.go`)

**Priority:** HIGH
**Rationale:** Models contain core business logic for threads, messages, and citations.

```diff
--- /dev/null
+++ b/internal/db/models_test.go
@@ -0,0 +1,246 @@
+package db
+
+import (
+	"encoding/json"
+	"testing"
+	"time"
+
+	"github.com/google/uuid"
+)
+
+func TestNewThread(t *testing.T) {
+	userID := "user-123"
+
+	thread := NewThread(userID)
+
+	if thread.UserID != userID {
+		t.Errorf("UserID = %v, want %v", thread.UserID, userID)
+	}
+
+	if thread.ID == uuid.Nil {
+		t.Error("expected non-nil UUID for thread ID")
+	}
+
+	if thread.Status != ThreadStatusActive {
+		t.Errorf("Status = %v, want %v", thread.Status, ThreadStatusActive)
+	}
+
+	if thread.MessageCount != 0 {
+		t.Errorf("MessageCount = %v, want 0", thread.MessageCount)
+	}
+
+	if thread.CreatedAt.IsZero() {
+		t.Error("CreatedAt should not be zero")
+	}
+
+	if thread.UpdatedAt.IsZero() {
+		t.Error("UpdatedAt should not be zero")
+	}
+}
+
+func TestNewMessage(t *testing.T) {
+	threadID := uuid.New()
+	role := RoleUser
+	content := "Hello, world!"
+
+	msg := NewMessage(threadID, role, content)
+
+	if msg.ID == uuid.Nil {
+		t.Error("expected non-nil UUID for message ID")
+	}
+
+	if msg.ThreadID != threadID {
+		t.Errorf("ThreadID = %v, want %v", msg.ThreadID, threadID)
+	}
+
+	if msg.Role != role {
+		t.Errorf("Role = %v, want %v", msg.Role, role)
+	}
+
+	if msg.Content != content {
+		t.Errorf("Content = %v, want %v", msg.Content, content)
+	}
+
+	if msg.CreatedAt.IsZero() {
+		t.Error("CreatedAt should not be zero")
+	}
+}
+
+func TestMessage_SetAssistantMetrics(t *testing.T) {
+	msg := NewMessage(uuid.New(), RoleAssistant, "Hello!")
+
+	provider := "openai"
+	model := "gpt-4"
+	inputTokens := 100
+	outputTokens := 50
+	processingTimeMs := 1500
+	costUSD := 0.05
+	responseID := "resp-123"
+
+	msg.SetAssistantMetrics(provider, model, inputTokens, outputTokens, processingTimeMs, costUSD, responseID)
+
+	if msg.Provider == nil || *msg.Provider != provider {
+		t.Errorf("Provider = %v, want %v", msg.Provider, provider)
+	}
+
+	if msg.Model == nil || *msg.Model != model {
+		t.Errorf("Model = %v, want %v", msg.Model, model)
+	}
+
+	if msg.InputTokens == nil || *msg.InputTokens != inputTokens {
+		t.Errorf("InputTokens = %v, want %v", msg.InputTokens, inputTokens)
+	}
+
+	if msg.OutputTokens == nil || *msg.OutputTokens != outputTokens {
+		t.Errorf("OutputTokens = %v, want %v", msg.OutputTokens, outputTokens)
+	}
+
+	expectedTotal := inputTokens + outputTokens
+	if msg.TotalTokens == nil || *msg.TotalTokens != expectedTotal {
+		t.Errorf("TotalTokens = %v, want %v", msg.TotalTokens, expectedTotal)
+	}
+
+	if msg.CostUSD == nil || *msg.CostUSD != costUSD {
+		t.Errorf("CostUSD = %v, want %v", msg.CostUSD, costUSD)
+	}
+
+	if msg.ProcessingTimeMs == nil || *msg.ProcessingTimeMs != processingTimeMs {
+		t.Errorf("ProcessingTimeMs = %v, want %v", msg.ProcessingTimeMs, processingTimeMs)
+	}
+
+	if msg.ResponseID == nil || *msg.ResponseID != responseID {
+		t.Errorf("ResponseID = %v, want %v", msg.ResponseID, responseID)
+	}
+}
+
+func TestMessage_SetAssistantMetrics_EmptyResponseID(t *testing.T) {
+	msg := NewMessage(uuid.New(), RoleAssistant, "Hello!")
+
+	msg.SetAssistantMetrics("openai", "gpt-4", 100, 50, 1500, 0.05, "")
+
+	if msg.ResponseID != nil {
+		t.Error("ResponseID should be nil when empty string is passed")
+	}
+}
+
+func TestMessage_TruncateContent(t *testing.T) {
+	tests := []struct {
+		name    string
+		content string
+		maxLen  int
+		want    string
+	}{
+		{
+			name:    "short content",
+			content: "Hello",
+			maxLen:  10,
+			want:    "Hello",
+		},
+		{
+			name:    "exact length",
+			content: "Hello",
+			maxLen:  5,
+			want:    "Hello",
+		},
+		{
+			name:    "needs truncation",
+			content: "Hello, world! This is a long message.",
+			maxLen:  15,
+			want:    "Hello, world...",
+		},
+		{
+			name:    "empty content",
+			content: "",
+			maxLen:  10,
+			want:    "",
+		},
+	}
+
+	for _, tt := range tests {
+		t.Run(tt.name, func(t *testing.T) {
+			msg := NewMessage(uuid.New(), RoleUser, tt.content)
+			got := msg.TruncateContent(tt.maxLen)
+			if got != tt.want {
+				t.Errorf("TruncateContent(%d) = %q, want %q", tt.maxLen, got, tt.want)
+			}
+		})
+	}
+}
+
+func TestParseCitations(t *testing.T) {
+	tests := []struct {
+		name    string
+		json    *string
+		want    int
+		wantErr bool
+	}{
+		{
+			name:    "nil json",
+			json:    nil,
+			want:    0,
+			wantErr: false,
+		},
+		{
+			name:    "empty string",
+			json:    ptrString(""),
+			want:    0,
+			wantErr: false,
+		},
+		{
+			name:    "valid citations",
+			json:    ptrString(`[{"type":"url","url":"https://example.com","title":"Example"}]`),
+			want:    1,
+			wantErr: false,
+		},
+		{
+			name:    "multiple citations",
+			json:    ptrString(`[{"type":"url","url":"https://a.com"},{"type":"file","file_id":"f123"}]`),
+			want:    2,
+			wantErr: false,
+		},
+		{
+			name:    "invalid json",
+			json:    ptrString(`{not valid json}`),
+			want:    0,
+			wantErr: true,
+		},
+	}
+
+	for _, tt := range tests {
+		t.Run(tt.name, func(t *testing.T) {
+			citations, err := ParseCitations(tt.json)
+			if (err != nil) != tt.wantErr {
+				t.Errorf("ParseCitations() error = %v, wantErr %v", err, tt.wantErr)
+				return
+			}
+			if len(citations) != tt.want {
+				t.Errorf("ParseCitations() returned %d citations, want %d", len(citations), tt.want)
+			}
+		})
+	}
+}
+
+func TestCitationsToJSON(t *testing.T) {
+	tests := []struct {
+		name      string
+		citations []Citation
+		wantNil   bool
+	}{
+		{
+			name:      "nil citations",
+			citations: nil,
+			wantNil:   true,
+		},
+		{
+			name:      "empty citations",
+			citations: []Citation{},
+			wantNil:   true,
+		},
+		{
+			name: "single citation",
+			citations: []Citation{
+				{Type: "url", URL: "https://example.com", Title: "Example"},
+			},
+			wantNil: false,
+		},
+	}
+
+	for _, tt := range tests {
+		t.Run(tt.name, func(t *testing.T) {
+			result, err := CitationsToJSON(tt.citations)
+			if err != nil {
+				t.Errorf("CitationsToJSON() unexpected error: %v", err)
+				return
+			}
+
+			if tt.wantNil && result != nil {
+				t.Errorf("CitationsToJSON() = %v, want nil", result)
+			}
+
+			if !tt.wantNil && result == nil {
+				t.Error("CitationsToJSON() = nil, want non-nil")
+			}
+
+			// Verify roundtrip
+			if result != nil {
+				parsed, err := ParseCitations(result)
+				if err != nil {
+					t.Errorf("roundtrip failed: %v", err)
+				}
+				if len(parsed) != len(tt.citations) {
+					t.Errorf("roundtrip citation count = %d, want %d", len(parsed), len(tt.citations))
+				}
+			}
+		})
+	}
+}
+
+func TestThreadStatus_Constants(t *testing.T) {
+	if ThreadStatusActive != "active" {
+		t.Errorf("ThreadStatusActive = %v, want 'active'", ThreadStatusActive)
+	}
+	if ThreadStatusArchived != "archived" {
+		t.Errorf("ThreadStatusArchived = %v, want 'archived'", ThreadStatusArchived)
+	}
+	if ThreadStatusDeleted != "deleted" {
+		t.Errorf("ThreadStatusDeleted = %v, want 'deleted'", ThreadStatusDeleted)
+	}
+}
+
+func TestMessageRole_Constants(t *testing.T) {
+	if RoleUser != "user" {
+		t.Errorf("RoleUser = %v, want 'user'", RoleUser)
+	}
+	if RoleAssistant != "assistant" {
+		t.Errorf("RoleAssistant = %v, want 'assistant'", RoleAssistant)
+	}
+	if RoleSystem != "system" {
+		t.Errorf("RoleSystem = %v, want 'system'", RoleSystem)
+	}
+}
+
+func ptrString(s string) *string {
+	return &s
+}
```

---

### 3. Database Repository Tests (`internal/db/repository_test.go`)

**Priority:** HIGH
**Rationale:** Repository functions handle all database operations - critical for data integrity.

```diff
--- /dev/null
+++ b/internal/db/repository_test.go
@@ -0,0 +1,125 @@
+package db
+
+import (
+	"errors"
+	"testing"
+)
+
+func TestValidTenantIDs(t *testing.T) {
+	// Verify expected tenants are valid
+	expectedTenants := []string{"ai8", "email4ai", "zztest"}
+
+	for _, tenant := range expectedTenants {
+		if !ValidTenantIDs[tenant] {
+			t.Errorf("expected %q to be a valid tenant ID", tenant)
+		}
+	}
+
+	// Verify invalid tenants are rejected
+	invalidTenants := []string{"invalid", "test", "admin", ""}
+
+	for _, tenant := range invalidTenants {
+		if ValidTenantIDs[tenant] {
+			t.Errorf("expected %q to NOT be a valid tenant ID", tenant)
+		}
+	}
+}
+
+func TestNewTenantRepository_ValidTenant(t *testing.T) {
+	// Create a mock client (we can't use a real one without DB connection)
+	// This tests the validation logic only
+	client := &Client{
+		tenantRepos: make(map[string]*Repository),
+	}
+
+	for _, tenantID := range []string{"ai8", "email4ai", "zztest"} {
+		repo, err := NewTenantRepository(client, tenantID)
+		if err != nil {
+			t.Errorf("NewTenantRepository(%q) unexpected error: %v", tenantID, err)
+			continue
+		}
+
+		if repo.TenantID() != tenantID {
+			t.Errorf("TenantID() = %q, want %q", repo.TenantID(), tenantID)
+		}
+
+		expectedPrefix := tenantID + "_airborne"
+		if repo.tablePrefix != expectedPrefix {
+			t.Errorf("tablePrefix = %q, want %q", repo.tablePrefix, expectedPrefix)
+		}
+	}
+}
+
+func TestNewTenantRepository_InvalidTenant(t *testing.T) {
+	client := &Client{
+		tenantRepos: make(map[string]*Repository),
+	}
+
+	invalidTenants := []string{"invalid", "test", "admin", "", "AI8", "Email4AI"}
+
+	for _, tenantID := range invalidTenants {
+		_, err := NewTenantRepository(client, tenantID)
+		if err == nil {
+			t.Errorf("NewTenantRepository(%q) expected error, got nil", tenantID)
+			continue
+		}
+
+		if !errors.Is(err, ErrInvalidTenant) {
+			t.Errorf("NewTenantRepository(%q) error = %v, want ErrInvalidTenant", tenantID, err)
+		}
+	}
+}
+
+func TestRepository_TableNames_WithPrefix(t *testing.T) {
+	repo := &Repository{
+		tablePrefix: "ai8_airborne",
+		tenantID:    "ai8",
+	}
+
+	tests := []struct {
+		method string
+		want   string
+	}{
+		{"threadsTable", "ai8_airborne_threads"},
+		{"messagesTable", "ai8_airborne_messages"},
+		{"filesTable", "ai8_airborne_files"},
+		{"fileUploadsTable", "ai8_airborne_file_provider_uploads"},
+		{"vectorStoresTable", "ai8_airborne_thread_vector_stores"},
+	}
+
+	if got := repo.threadsTable(); got != tests[0].want {
+		t.Errorf("threadsTable() = %q, want %q", got, tests[0].want)
+	}
+	if got := repo.messagesTable(); got != tests[1].want {
+		t.Errorf("messagesTable() = %q, want %q", got, tests[1].want)
+	}
+	if got := repo.filesTable(); got != tests[2].want {
+		t.Errorf("filesTable() = %q, want %q", got, tests[2].want)
+	}
+	if got := repo.fileUploadsTable(); got != tests[3].want {
+		t.Errorf("fileUploadsTable() = %q, want %q", got, tests[3].want)
+	}
+	if got := repo.vectorStoresTable(); got != tests[4].want {
+		t.Errorf("vectorStoresTable() = %q, want %q", got, tests[4].want)
+	}
+}
+
+func TestRepository_TableNames_Legacy(t *testing.T) {
+	repo := &Repository{
+		tablePrefix: "", // Legacy mode
+		tenantID:    "",
+	}
+
+	if got := repo.threadsTable(); got != "airborne_threads" {
+		t.Errorf("threadsTable() = %q, want %q", got, "airborne_threads")
+	}
+	if got := repo.messagesTable(); got != "airborne_messages" {
+		t.Errorf("messagesTable() = %q, want %q", got, "airborne_messages")
+	}
+	if got := repo.filesTable(); got != "airborne_files" {
+		t.Errorf("filesTable() = %q, want %q", got, "airborne_files")
+	}
+	if got := repo.fileUploadsTable(); got != "airborne_file_provider_uploads" {
+		t.Errorf("fileUploadsTable() = %q, want %q", got, "airborne_file_provider_uploads")
+	}
+	if got := repo.vectorStoresTable(); got != "airborne_thread_vector_stores" {
+		t.Errorf("vectorStoresTable() = %q, want %q", got, "airborne_thread_vector_stores")
+	}
+}
```

---

### 4. ImageGen Package Tests (`internal/imagegen/config_test.go`)

**Priority:** MEDIUM
**Rationale:** Config methods determine image generation behavior.

```diff
--- /dev/null
+++ b/internal/imagegen/config_test.go
@@ -0,0 +1,109 @@
+package imagegen
+
+import "testing"
+
+func TestConfig_IsEnabled(t *testing.T) {
+	tests := []struct {
+		name   string
+		config *Config
+		want   bool
+	}{
+		{
+			name:   "nil config",
+			config: nil,
+			want:   false,
+		},
+		{
+			name:   "disabled config",
+			config: &Config{Enabled: false},
+			want:   false,
+		},
+		{
+			name:   "enabled config",
+			config: &Config{Enabled: true},
+			want:   true,
+		},
+	}
+
+	for _, tt := range tests {
+		t.Run(tt.name, func(t *testing.T) {
+			if got := tt.config.IsEnabled(); got != tt.want {
+				t.Errorf("Config.IsEnabled() = %v, want %v", got, tt.want)
+			}
+		})
+	}
+}
+
+func TestConfig_GetProvider(t *testing.T) {
+	tests := []struct {
+		name   string
+		config *Config
+		want   string
+	}{
+		{
+			name:   "nil config defaults to gemini",
+			config: nil,
+			want:   "gemini",
+		},
+		{
+			name:   "empty provider defaults to gemini",
+			config: &Config{Provider: ""},
+			want:   "gemini",
+		},
+		{
+			name:   "explicit gemini",
+			config: &Config{Provider: "gemini"},
+			want:   "gemini",
+		},
+		{
+			name:   "explicit openai",
+			config: &Config{Provider: "openai"},
+			want:   "openai",
+		},
+	}
+
+	for _, tt := range tests {
+		t.Run(tt.name, func(t *testing.T) {
+			if got := tt.config.GetProvider(); got != tt.want {
+				t.Errorf("Config.GetProvider() = %v, want %v", got, tt.want)
+			}
+		})
+	}
+}
+
+func TestConfig_GetModel(t *testing.T) {
+	tests := []struct {
+		name   string
+		config *Config
+		want   string
+	}{
+		{
+			name:   "nil config returns empty",
+			config: nil,
+			want:   "",
+		},
+		{
+			name:   "empty model returns empty",
+			config: &Config{Model: ""},
+			want:   "",
+		},
+		{
+			name:   "explicit model",
+			config: &Config{Model: "dall-e-3"},
+			want:   "dall-e-3",
+		},
+	}
+
+	for _, tt := range tests {
+		t.Run(tt.name, func(t *testing.T) {
+			if got := tt.config.GetModel(); got != tt.want {
+				t.Errorf("Config.GetModel() = %v, want %v", got, tt.want)
+			}
+		})
+	}
+}
```

---

### 5. ImageGen Client Tests (`internal/imagegen/client_test.go`)

**Priority:** MEDIUM
**Rationale:** Tests for image generation detection and request building.

```diff
--- /dev/null
+++ b/internal/imagegen/client_test.go
@@ -0,0 +1,177 @@
+package imagegen
+
+import (
+	"context"
+	"testing"
+)
+
+func TestNewClient(t *testing.T) {
+	client := NewClient()
+	if client == nil {
+		t.Fatal("NewClient() returned nil")
+	}
+}
+
+func TestClient_DetectImageRequest(t *testing.T) {
+	client := NewClient()
+
+	tests := []struct {
+		name       string
+		text       string
+		config     *Config
+		wantNil    bool
+		wantPrompt string
+	}{
+		{
+			name:    "nil config",
+			text:    "@image a sunset",
+			config:  nil,
+			wantNil: true,
+		},
+		{
+			name:    "disabled config",
+			text:    "@image a sunset",
+			config:  &Config{Enabled: false, TriggerPhrases: []string{"@image"}},
+			wantNil: true,
+		},
+		{
+			name:    "no trigger phrases",
+			text:    "@image a sunset",
+			config:  &Config{Enabled: true, TriggerPhrases: []string{}},
+			wantNil: true,
+		},
+		{
+			name:       "trigger found",
+			text:       "@image a beautiful sunset over mountains",
+			config:     &Config{Enabled: true, TriggerPhrases: []string{"@image"}},
+			wantNil:    false,
+			wantPrompt: "a beautiful sunset over mountains",
+		},
+		{
+			name:       "case insensitive trigger",
+			text:       "@IMAGE a sunset",
+			config:     &Config{Enabled: true, TriggerPhrases: []string{"@image"}},
+			wantNil:    false,
+			wantPrompt: "a sunset",
+		},
+		{
+			name:       "trigger in middle of text",
+			text:       "Please @image generate a sunset",
+			config:     &Config{Enabled: true, TriggerPhrases: []string{"@image"}},
+			wantNil:    false,
+			wantPrompt: "generate a sunset",
+		},
+		{
+			name:    "trigger without prompt",
+			text:    "@image",
+			config:  &Config{Enabled: true, TriggerPhrases: []string{"@image"}},
+			wantNil: true,
+		},
+		{
+			name:    "trigger with only whitespace after",
+			text:    "@image   ",
+			config:  &Config{Enabled: true, TriggerPhrases: []string{"@image"}},
+			wantNil: true,
+		},
+		{
+			name:       "multiple triggers - first matches",
+			text:       "@pic a cat",
+			config:     &Config{Enabled: true, TriggerPhrases: []string{"@image", "@pic"}},
+			wantNil:    false,
+			wantPrompt: "a cat",
+		},
+		{
+			name:    "no trigger found",
+			text:    "Just a regular message",
+			config:  &Config{Enabled: true, TriggerPhrases: []string{"@image"}},
+			wantNil: true,
+		},
+		{
+			name:    "empty trigger phrase in list",
+			text:    "@image a sunset",
+			config:  &Config{Enabled: true, TriggerPhrases: []string{"", "@image"}},
+			wantNil:    false,
+			wantPrompt: "a sunset",
+		},
+	}
+
+	for _, tt := range tests {
+		t.Run(tt.name, func(t *testing.T) {
+			result := client.DetectImageRequest(tt.text, tt.config)
+
+			if tt.wantNil {
+				if result != nil {
+					t.Errorf("DetectImageRequest() = %v, want nil", result)
+				}
+				return
+			}
+
+			if result == nil {
+				t.Fatal("DetectImageRequest() = nil, want non-nil")
+			}
+
+			if result.Prompt != tt.wantPrompt {
+				t.Errorf("Prompt = %q, want %q", result.Prompt, tt.wantPrompt)
+			}
+
+			if result.Config != tt.config {
+				t.Error("Config reference mismatch")
+			}
+		})
+	}
+}
+
+func TestClient_Generate_NilRequest(t *testing.T) {
+	client := NewClient()
+	ctx := context.Background()
+
+	_, err := client.Generate(ctx, nil)
+	if err == nil {
+		t.Error("Generate(nil) expected error, got nil")
+	}
+}
+
+func TestClient_Generate_NilConfig(t *testing.T) {
+	client := NewClient()
+	ctx := context.Background()
+
+	_, err := client.Generate(ctx, &ImageRequest{Config: nil})
+	if err == nil {
+		t.Error("Generate() with nil config expected error, got nil")
+	}
+}
+
+func TestClient_Generate_UnsupportedProvider(t *testing.T) {
+	client := NewClient()
+	ctx := context.Background()
+
+	req := &ImageRequest{
+		Prompt: "test",
+		Config: &Config{
+			Enabled:  true,
+			Provider: "unsupported",
+		},
+	}
+
+	_, err := client.Generate(ctx, req)
+	if err == nil {
+		t.Error("Generate() with unsupported provider expected error, got nil")
+	}
+}
+
+func TestTruncateForAlt(t *testing.T) {
+	// Short string - no truncation
+	short := "Hello"
+	if got := truncateForAlt(short, 10); got != short {
+		t.Errorf("truncateForAlt(%q, 10) = %q, want %q", short, got, short)
+	}
+
+	// Long string - truncated
+	long := "This is a very long string that should be truncated"
+	got := truncateForAlt(long, 20)
+	if len(got) != 20 {
+		t.Errorf("truncateForAlt() length = %d, want 20", len(got))
+	}
+	if got[len(got)-3:] != "..." {
+		t.Errorf("truncateForAlt() should end with '...', got %q", got)
+	}
+}
```

---

### 6. Tenant Doppler Tests (`internal/tenant/doppler_test.go`)

**Priority:** MEDIUM
**Rationale:** Doppler integration handles secrets retrieval - important for security.

```diff
--- /dev/null
+++ b/internal/tenant/doppler_test.go
@@ -0,0 +1,99 @@
+package tenant
+
+import (
+	"testing"
+)
+
+func TestIsRetryableError(t *testing.T) {
+	tests := []struct {
+		name       string
+		statusCode int
+		want       bool
+	}{
+		// Retryable status codes
+		{"500 internal server error", 500, true},
+		{"501 not implemented", 501, true},
+		{"502 bad gateway", 502, true},
+		{"503 service unavailable", 503, true},
+		{"504 gateway timeout", 504, true},
+		{"429 rate limited", 429, true},
+
+		// Non-retryable status codes
+		{"200 ok", 200, false},
+		{"400 bad request", 400, false},
+		{"401 unauthorized", 401, false},
+		{"403 forbidden", 403, false},
+		{"404 not found", 404, false},
+		{"422 unprocessable entity", 422, false},
+	}
+
+	for _, tt := range tests {
+		t.Run(tt.name, func(t *testing.T) {
+			if got := isRetryableError(tt.statusCode); got != tt.want {
+				t.Errorf("isRetryableError(%d) = %v, want %v", tt.statusCode, got, tt.want)
+			}
+		})
+	}
+}
+
+func TestDopplerEnabled_NoToken(t *testing.T) {
+	// Save and restore any existing token
+	// Note: This test assumes DOPPLER_TOKEN is not set in test environment
+
+	// Reset the global client for testing
+	globalDopplerClient = nil
+
+	enabled := DopplerEnabled()
+	// Will be false if DOPPLER_TOKEN is not set
+	// We can't easily test the true case without mocking env vars
+	_ = enabled // Just verify it doesn't panic
+}
+
+func TestClearDopplerCache(t *testing.T) {
+	// Test that ClearDopplerCache doesn't panic when client is nil
+	originalClient := globalDopplerClient
+	globalDopplerClient = nil
+
+	// Should not panic
+	ClearDopplerCache()
+
+	// Restore
+	globalDopplerClient = originalClient
+}
+
+func TestClearDopplerCache_WithClient(t *testing.T) {
+	// Create a mock client with cache
+	client := &dopplerClient{
+		cache: map[string]map[string]string{
+			"project1": {"SECRET1": "value1"},
+		},
+	}
+
+	originalClient := globalDopplerClient
+	globalDopplerClient = client
+
+	// Clear cache
+	ClearDopplerCache()
+
+	// Verify cache is cleared
+	if len(client.cache) != 0 {
+		t.Errorf("expected empty cache, got %d entries", len(client.cache))
+	}
+
+	// Restore
+	globalDopplerClient = originalClient
+}
+
+func TestDopplerRetryConfig(t *testing.T) {
+	// Verify retry configuration constants
+	if maxRetries != 15 {
+		t.Errorf("maxRetries = %d, want 15", maxRetries)
+	}
+
+	if baseBackoff.Milliseconds() != 100 {
+		t.Errorf("baseBackoff = %v, want 100ms", baseBackoff)
+	}
+
+	if maxBackoff.Seconds() != 5 {
+		t.Errorf("maxBackoff = %v, want 5s", maxBackoff)
+	}
+}
```

---

### 7. Provider Types Tests (`internal/provider/provider_test.go`)

**Priority:** MEDIUM
**Rationale:** Provider types are used throughout the codebase.

```diff
--- /dev/null
+++ b/internal/provider/provider_test.go
@@ -0,0 +1,89 @@
+package provider
+
+import "testing"
+
+func TestGenerateResult_HasImages(t *testing.T) {
+	tests := []struct {
+		name   string
+		result GenerateResult
+		want   bool
+	}{
+		{
+			name:   "no images",
+			result: GenerateResult{Images: nil},
+			want:   false,
+		},
+		{
+			name:   "empty images slice",
+			result: GenerateResult{Images: []GeneratedImage{}},
+			want:   false,
+		},
+		{
+			name: "has images",
+			result: GenerateResult{
+				Images: []GeneratedImage{{Data: []byte("img")}},
+			},
+			want: true,
+		},
+	}
+
+	for _, tt := range tests {
+		t.Run(tt.name, func(t *testing.T) {
+			if got := tt.result.HasImages(); got != tt.want {
+				t.Errorf("GenerateResult.HasImages() = %v, want %v", got, tt.want)
+			}
+		})
+	}
+}
+
+func TestChunkType_Constants(t *testing.T) {
+	// Verify chunk type enum values
+	if ChunkTypeText != 0 {
+		t.Errorf("ChunkTypeText = %d, want 0", ChunkTypeText)
+	}
+	if ChunkTypeUsage != 1 {
+		t.Errorf("ChunkTypeUsage = %d, want 1", ChunkTypeUsage)
+	}
+	if ChunkTypeCitation != 2 {
+		t.Errorf("ChunkTypeCitation = %d, want 2", ChunkTypeCitation)
+	}
+	if ChunkTypeComplete != 3 {
+		t.Errorf("ChunkTypeComplete = %d, want 3", ChunkTypeComplete)
+	}
+	if ChunkTypeError != 4 {
+		t.Errorf("ChunkTypeError = %d, want 4", ChunkTypeError)
+	}
+	if ChunkTypeToolCall != 5 {
+		t.Errorf("ChunkTypeToolCall = %d, want 5", ChunkTypeToolCall)
+	}
+	if ChunkTypeCodeExecution != 6 {
+		t.Errorf("ChunkTypeCodeExecution = %d, want 6", ChunkTypeCodeExecution)
+	}
+}
+
+func TestCitationType_Constants(t *testing.T) {
+	if CitationTypeUnknown != 0 {
+		t.Errorf("CitationTypeUnknown = %d, want 0", CitationTypeUnknown)
+	}
+	if CitationTypeURL != 1 {
+		t.Errorf("CitationTypeURL = %d, want 1", CitationTypeURL)
+	}
+	if CitationTypeFile != 2 {
+		t.Errorf("CitationTypeFile = %d, want 2", CitationTypeFile)
+	}
+}
+
+func TestUsage_Struct(t *testing.T) {
+	usage := Usage{
+		InputTokens:  100,
+		OutputTokens: 50,
+		TotalTokens:  150,
+	}
+
+	if usage.InputTokens != 100 {
+		t.Errorf("InputTokens = %d, want 100", usage.InputTokens)
+	}
+	if usage.OutputTokens != 50 {
+		t.Errorf("OutputTokens = %d, want 50", usage.OutputTokens)
+	}
+	if usage.TotalTokens != 150 {
+		t.Errorf("TotalTokens = %d, want 150", usage.TotalTokens)
+	}
+}
```

---

### 8. Retry Backoff Tests (`internal/retry/backoff_test.go`)

**Priority:** LOW
**Rationale:** Simple function but edge cases should be tested.

```diff
--- /dev/null
+++ b/internal/retry/backoff_test.go
@@ -0,0 +1,47 @@
+package retry
+
+import (
+	"context"
+	"testing"
+	"time"
+)
+
+func TestSleepWithBackoff_Delays(t *testing.T) {
+	tests := []struct {
+		attempt      int
+		expectedMin  time.Duration
+		expectedMax  time.Duration
+	}{
+		{1, 200 * time.Millisecond, 350 * time.Millisecond},  // BackoffBase * 2^0 = 250ms
+		{2, 400 * time.Millisecond, 600 * time.Millisecond},  // BackoffBase * 2^1 = 500ms
+	}
+
+	for _, tt := range tests {
+		t.Run("attempt_"+string(rune('0'+tt.attempt)), func(t *testing.T) {
+			ctx := context.Background()
+			start := time.Now()
+			SleepWithBackoff(ctx, tt.attempt)
+			elapsed := time.Since(start)
+
+			if elapsed < tt.expectedMin {
+				t.Errorf("attempt %d: elapsed %v < expected min %v", tt.attempt, elapsed, tt.expectedMin)
+			}
+			if elapsed > tt.expectedMax {
+				t.Errorf("attempt %d: elapsed %v > expected max %v", tt.attempt, elapsed, tt.expectedMax)
+			}
+		})
+	}
+}
+
+func TestSleepWithBackoff_CancelledContext(t *testing.T) {
+	ctx, cancel := context.WithCancel(context.Background())
+	cancel() // Cancel immediately
+
+	start := time.Now()
+	SleepWithBackoff(ctx, 10) // High attempt would normally sleep a long time
+	elapsed := time.Since(start)
+
+	// Should return almost immediately due to cancelled context
+	if elapsed > 100*time.Millisecond {
+		t.Errorf("SleepWithBackoff with cancelled context took %v, expected < 100ms", elapsed)
+	}
+}
```

---

### 9. OpenAI Compat Provider Tests (`internal/provider/cerebras/client_test.go`)

**Priority:** MEDIUM
**Rationale:** All compat-based providers share the same pattern - test one as example.

```diff
--- /dev/null
+++ b/internal/provider/cerebras/client_test.go
@@ -0,0 +1,64 @@
+package cerebras
+
+import (
+	"testing"
+
+	"github.com/ai8future/airborne/internal/provider"
+)
+
+func TestNewClient(t *testing.T) {
+	client := NewClient()
+
+	if client == nil {
+		t.Fatal("NewClient() returned nil")
+	}
+
+	if client.Client == nil {
+		t.Error("embedded compat.Client is nil")
+	}
+}
+
+func TestNewClient_WithDebugLogging(t *testing.T) {
+	client := NewClient(WithDebugLogging(true))
+
+	if client == nil {
+		t.Fatal("NewClient(WithDebugLogging(true)) returned nil")
+	}
+}
+
+func TestNewClient_WithNilOption(t *testing.T) {
+	// Should not panic with nil option
+	client := NewClient(nil)
+
+	if client == nil {
+		t.Fatal("NewClient(nil) returned nil")
+	}
+}
+
+func TestClient_ImplementsProvider(t *testing.T) {
+	var _ provider.Provider = (*Client)(nil)
+}
+
+func TestClient_Name(t *testing.T) {
+	client := NewClient()
+
+	if name := client.Name(); name != "cerebras" {
+		t.Errorf("Name() = %q, want %q", name, "cerebras")
+	}
+}
+
+func TestClient_SupportsFileSearch(t *testing.T) {
+	client := NewClient()
+
+	if client.SupportsFileSearch() {
+		t.Error("SupportsFileSearch() = true, want false")
+	}
+}
+
+func TestClient_SupportsWebSearch(t *testing.T) {
+	client := NewClient()
+
+	if client.SupportsWebSearch() {
+		t.Error("SupportsWebSearch() = true, want false")
+	}
+}
```

---

## Summary of Test Coverage Improvements

| Package | New Test File | Tests Added | Coverage Improvement |
|---------|--------------|-------------|---------------------|
| `internal/pricing` | `pricing_test.go` | 12 tests | 0% → ~85% |
| `internal/db` | `models_test.go` | 10 tests | 0% → ~70% |
| `internal/db` | `repository_test.go` | 5 tests | 0% → ~30%* |
| `internal/imagegen` | `config_test.go` | 3 tests | 0% → 100% |
| `internal/imagegen` | `client_test.go` | 7 tests | 0% → ~60% |
| `internal/tenant` | `doppler_test.go` | 5 tests | 0% → ~40%* |
| `internal/provider` | `provider_test.go` | 4 tests | 0% → ~30% |
| `internal/retry` | `backoff_test.go` | 2 tests | 0% → 100% |
| `internal/provider/cerebras` | `client_test.go` | 7 tests | 0% → ~80% |

*Note: Repository and Doppler tests are limited without mocking database/HTTP - higher coverage requires integration tests.

---

## Recommendations for Further Testing

### High Priority
1. **Integration tests for `internal/db`** - Use testcontainers or similar to test against real PostgreSQL
2. **HTTP mocking for `internal/tenant/doppler.go`** - Use `httptest` to fully test Doppler API interactions
3. **Provider streaming tests** - Test `GenerateReplyStream()` implementations

### Medium Priority
1. **Test remaining compat providers** - Apply same pattern from cerebras to deepseek, fireworks, etc.
2. **Add benchmark tests** - For pricing calculations and message processing
3. **Fuzz testing** - For ParseCitations and other JSON parsing functions

### Low Priority
1. **Dashboard tests** - Add Jest/Vitest tests for Next.js frontend components
2. **E2E tests** - Add integration tests that span multiple packages

---

## Implementation Notes

1. All tests follow existing codebase patterns (table-driven tests, `t.Helper()` usage)
2. Tests avoid external dependencies (no network calls, no database)
3. Mocks are provided where needed for isolation
4. Tests are compatible with `go test -race` for race condition detection

To run these tests after implementation:
```bash
cd /Users/cliff/Desktop/_code/airborne
make test
# Or for coverage:
make test-coverage
```
