# Airborne Codebase Analysis Report

**Date Created:** 2026-01-21 21:45 UTC
**Project:** github.com/ai8future/airborne
**Type:** Go backend service (LLM Provider & RAG Service)
**Analyzed By:** Claude:Opus 4.5

---

## 1. AUDIT - Security and Code Quality Issues

### 1.1 HIGH: IO Read Error Silently Ignored (16 locations)

**Impact:** Error response bodies from APIs are discarded, making debugging failures difficult.

**Locations:**
- `internal/tenant/doppler.go:146`
- `internal/config/config.go:471`
- `internal/provider/gemini/filestore.go:154,180`
- `internal/rag/embedder/ollama.go:106`
- `internal/rag/extractor/docbox.go:175`
- `internal/rag/vectorstore/qdrant.go:76,222`

**File: internal/tenant/doppler.go:145-148**

```diff
 	if resp.StatusCode != http.StatusOK {
-		body, _ := io.ReadAll(resp.Body)
+		body, readErr := io.ReadAll(resp.Body)
+		if readErr != nil {
+			return nil, resp.StatusCode, fmt.Errorf("doppler API error (status %d): failed to read body: %w", resp.StatusCode, readErr)
+		}
 		return nil, resp.StatusCode, fmt.Errorf("doppler API error (status %d): %s", resp.StatusCode, string(body))
 	}
```

**File: internal/config/config.go:470-474**

```diff
 	if resp.StatusCode != http.StatusOK {
-		body, _ := io.ReadAll(resp.Body)
+		body, readErr := io.ReadAll(resp.Body)
+		if readErr != nil {
+			fmt.Fprintf(os.Stderr, "doppler: API error (status %d): failed to read response body\n", resp.StatusCode)
+			return ""
+		}
 		fmt.Fprintf(os.Stderr, "doppler: API error (status %d): %s\n", resp.StatusCode, string(body))
 		return ""
 	}
```

**File: internal/provider/gemini/filestore.go:153-156**

```diff
 	if initResp.StatusCode != http.StatusOK {
-		body, _ := io.ReadAll(initResp.Body)
+		body, readErr := io.ReadAll(initResp.Body)
+		if readErr != nil {
+			return "", fmt.Errorf("init upload failed: %s (body read error: %v)", initResp.Status, readErr)
+		}
 		return "", fmt.Errorf("init upload failed: %s - %s", initResp.Status, string(body))
 	}
```

---

### 1.2 MEDIUM: JSON Marshal Error Silently Ignored

**Impact:** Debug JSON capture may fail silently, reducing observability.

**File: internal/provider/gemini/client.go:578**

```diff
 				// Handle function calls
 				if part.FunctionCall != nil {
-					argsJSON, _ := json.Marshal(part.FunctionCall.Args)
+					argsJSON, err := json.Marshal(part.FunctionCall.Args)
+					if err != nil {
+						slog.Warn("failed to marshal function call args", "error", err, "function", part.FunctionCall.Name)
+						argsJSON = []byte("{}")
+					}
 					toolCall := provider.ToolCall{
 						ID:        part.FunctionCall.ID,
 						Name:      part.FunctionCall.Name,
```

**File: internal/provider/gemini/client.go:637**

```diff
 		// Build synthetic response JSON for debugging
 		var respJSON []byte
 		if lastUsage != nil {
 			syntheticResp := map[string]any{
 				"text":            totalText.String(),
 				"model":           model,
 				"input_tokens":    lastUsage.InputTokens,
 				"output_tokens":   lastUsage.OutputTokens,
 				"total_tokens":    lastUsage.TotalTokens,
 				"tool_calls":      len(toolCalls),
 				"code_executions": len(codeExecutions),
 			}
-			respJSON, _ = json.Marshal(syntheticResp)
+			respJSON, err = json.Marshal(syntheticResp)
+			if err != nil {
+				slog.Warn("failed to marshal synthetic response", "error", err)
+			}
 		}
```

---

### 1.3 MEDIUM: Background Goroutine Uses context.Background()

**Impact:** Loses request tracing context, making distributed tracing difficult.

**File: internal/service/chat.go:1101-1104**

```diff
 	// Run persistence in background goroutine
 	go func() {
-		// Create a new context with timeout for the background operation
-		persistCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
+		// Create a new context with timeout for the background operation.
+		// Note: We use Background() because the parent request context may be cancelled
+		// before persistence completes. Consider adding trace ID propagation here.
+		persistCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
 		defer cancel()
```

**Recommendation:** Create a utility function that propagates trace IDs:

```go
// detachedContextWithTrace creates a new context with trace ID from parent but no cancellation
func detachedContextWithTrace(parent context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
    ctx, cancel := context.WithTimeout(context.Background(), timeout)
    // Propagate trace ID if using OpenTelemetry
    if span := trace.SpanFromContext(parent); span.SpanContext().IsValid() {
        ctx = trace.ContextWithSpan(ctx, span)
    }
    return ctx, cancel
}
```

---

### 1.4 LOW: Magic Number for Limit Validation

**File: internal/admin/server.go:138**

```diff
+const maxActivityLimit = 200
+
 // In handleActivity function:
 	limitStr := r.URL.Query().Get("limit")
 	limit := 50 // default
 	if limitStr != "" {
-		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 && l <= 200 {
+		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 && l <= maxActivityLimit {
 			limit = l
 		}
 	}
```

---

### 1.5 Security Strengths (No Action Needed)

- **SSRF Protection:** `internal/validation/url.go` has comprehensive protection against SSRF attacks
- **SQL Injection:** All queries use parameterized `$1, $2, $3` format with tenant ID whitelist
- **Secrets Management:** Uses Doppler with proper caching and retry logic
- **Rate Limiting:** Implemented with atomic Lua scripts in Redis

---

## 2. TESTS - Proposed Unit Tests for Untested Code

### 2.1 CRITICAL: Database Repository Tests (No Test File Exists)

**File: internal/db/repository_test.go (NEW FILE)**

```diff
+package db
+
+import (
+	"context"
+	"testing"
+	"time"
+
+	"github.com/google/uuid"
+	"github.com/stretchr/testify/assert"
+	"github.com/stretchr/testify/require"
+)
+
+func TestNewTenantRepository(t *testing.T) {
+	tests := []struct {
+		name      string
+		tenantID  string
+		wantErr   bool
+		wantTable string
+	}{
+		{
+			name:      "valid tenant ai8",
+			tenantID:  "ai8",
+			wantErr:   false,
+			wantTable: "ai8_airborne_threads",
+		},
+		{
+			name:      "valid tenant email4ai",
+			tenantID:  "email4ai",
+			wantErr:   false,
+			wantTable: "email4ai_airborne_threads",
+		},
+		{
+			name:      "valid tenant zztest",
+			tenantID:  "zztest",
+			wantErr:   false,
+			wantTable: "zztest_airborne_threads",
+		},
+		{
+			name:     "invalid tenant",
+			tenantID: "hacker",
+			wantErr:  true,
+		},
+		{
+			name:     "empty tenant",
+			tenantID: "",
+			wantErr:  true,
+		},
+		{
+			name:     "sql injection attempt",
+			tenantID: "'; DROP TABLE users; --",
+			wantErr:  true,
+		},
+	}
+
+	for _, tt := range tests {
+		t.Run(tt.name, func(t *testing.T) {
+			repo, err := NewTenantRepository(nil, tt.tenantID)
+			if tt.wantErr {
+				assert.Error(t, err)
+				assert.Nil(t, repo)
+				return
+			}
+			require.NoError(t, err)
+			require.NotNil(t, repo)
+			assert.Equal(t, tt.wantTable, repo.threadsTable())
+			assert.Equal(t, tt.tenantID, repo.TenantID())
+		})
+	}
+}
+
+func TestRepository_TableNames(t *testing.T) {
+	repo := &Repository{
+		tablePrefix: "ai8_airborne",
+		tenantID:    "ai8",
+	}
+
+	assert.Equal(t, "ai8_airborne_threads", repo.threadsTable())
+	assert.Equal(t, "ai8_airborne_messages", repo.messagesTable())
+	assert.Equal(t, "ai8_airborne_files", repo.filesTable())
+	assert.Equal(t, "ai8_airborne_file_provider_uploads", repo.fileUploadsTable())
+	assert.Equal(t, "ai8_airborne_thread_vector_stores", repo.vectorStoresTable())
+}
+
+func TestRepository_LegacyTableNames(t *testing.T) {
+	repo := &Repository{
+		tablePrefix: "",
+		tenantID:    "",
+	}
+
+	assert.Equal(t, "airborne_threads", repo.threadsTable())
+	assert.Equal(t, "airborne_messages", repo.messagesTable())
+	assert.Equal(t, "airborne_files", repo.filesTable())
+}
```

---

### 2.2 HIGH: Pricing Calculation Tests (No Test File Exists)

**File: internal/pricing/pricing_test.go (NEW FILE)**

```diff
+package pricing
+
+import (
+	"os"
+	"path/filepath"
+	"testing"
+
+	"github.com/stretchr/testify/assert"
+	"github.com/stretchr/testify/require"
+)
+
+func TestNewPricer(t *testing.T) {
+	// Create temp config dir
+	tempDir := t.TempDir()
+
+	// Create test pricing file
+	pricingJSON := `{
+		"provider": "test",
+		"models": {
+			"test-model": {
+				"input_per_million": 1.0,
+				"output_per_million": 2.0
+			},
+			"gpt-4": {
+				"input_per_million": 30.0,
+				"output_per_million": 60.0
+			}
+		}
+	}`
+	err := os.WriteFile(filepath.Join(tempDir, "test_pricing.json"), []byte(pricingJSON), 0644)
+	require.NoError(t, err)
+
+	pricer, err := NewPricer(tempDir)
+	require.NoError(t, err)
+	assert.NotNil(t, pricer)
+	assert.Equal(t, 2, pricer.ModelCount())
+}
+
+func TestPricer_Calculate(t *testing.T) {
+	pricer := &Pricer{
+		models: map[string]ModelPricing{
+			"gpt-4": {
+				InputPerMillion:  30.0,
+				OutputPerMillion: 60.0,
+			},
+		},
+	}
+
+	tests := []struct {
+		name         string
+		model        string
+		inputTokens  int64
+		outputTokens int64
+		wantInput    float64
+		wantOutput   float64
+		wantTotal    float64
+		wantUnknown  bool
+	}{
+		{
+			name:         "known model",
+			model:        "gpt-4",
+			inputTokens:  1000,
+			outputTokens: 500,
+			wantInput:    0.00003,  // 1000 * 30 / 1_000_000
+			wantOutput:   0.00003,  // 500 * 60 / 1_000_000
+			wantTotal:    0.00006,
+			wantUnknown:  false,
+		},
+		{
+			name:         "unknown model",
+			model:        "unknown-model",
+			inputTokens:  1000,
+			outputTokens: 500,
+			wantUnknown:  true,
+		},
+		{
+			name:         "zero tokens",
+			model:        "gpt-4",
+			inputTokens:  0,
+			outputTokens: 0,
+			wantInput:    0,
+			wantOutput:   0,
+			wantTotal:    0,
+			wantUnknown:  false,
+		},
+		{
+			name:         "large token count",
+			model:        "gpt-4",
+			inputTokens:  1_000_000,
+			outputTokens: 1_000_000,
+			wantInput:    30.0,
+			wantOutput:   60.0,
+			wantTotal:    90.0,
+			wantUnknown:  false,
+		},
+	}
+
+	for _, tt := range tests {
+		t.Run(tt.name, func(t *testing.T) {
+			cost := pricer.Calculate(tt.model, tt.inputTokens, tt.outputTokens)
+			assert.Equal(t, tt.wantUnknown, cost.Unknown)
+			if !tt.wantUnknown {
+				assert.InDelta(t, tt.wantInput, cost.InputCost, 0.0000001)
+				assert.InDelta(t, tt.wantOutput, cost.OutputCost, 0.0000001)
+				assert.InDelta(t, tt.wantTotal, cost.TotalCost, 0.0000001)
+			}
+		})
+	}
+}
+
+func TestPricer_PrefixMatch(t *testing.T) {
+	pricer := &Pricer{
+		models: map[string]ModelPricing{
+			"claude-3-opus": {
+				InputPerMillion:  15.0,
+				OutputPerMillion: 75.0,
+			},
+		},
+	}
+
+	// Should match "claude-3-opus-20240229" via prefix
+	cost := pricer.Calculate("claude-3-opus-20240229", 1000, 1000)
+	assert.False(t, cost.Unknown)
+	assert.InDelta(t, 0.000015, cost.InputCost, 0.0000001)
+}
```

---

### 2.3 HIGH: OpenAI FileStore Tests (No Test File Exists)

**File: internal/provider/openai/filestore_test.go (NEW FILE)**

```diff
+package openai
+
+import (
+	"testing"
+
+	"github.com/stretchr/testify/assert"
+)
+
+func TestFileStoreConfig_Validation(t *testing.T) {
+	tests := []struct {
+		name    string
+		cfg     FileStoreConfig
+		wantErr bool
+		errMsg  string
+	}{
+		{
+			name:    "empty API key",
+			cfg:     FileStoreConfig{APIKey: ""},
+			wantErr: true,
+			errMsg:  "API key is required",
+		},
+		{
+			name:    "whitespace only API key",
+			cfg:     FileStoreConfig{APIKey: "   "},
+			wantErr: true,
+			errMsg:  "API key is required",
+		},
+		{
+			name:    "valid API key",
+			cfg:     FileStoreConfig{APIKey: "sk-test-key"},
+			wantErr: false,
+		},
+		{
+			name: "invalid base URL - private IP",
+			cfg: FileStoreConfig{
+				APIKey:  "sk-test-key",
+				BaseURL: "http://192.168.1.1/api",
+			},
+			wantErr: true,
+			errMsg:  "invalid base URL",
+		},
+		{
+			name: "invalid base URL - HTTP non-localhost",
+			cfg: FileStoreConfig{
+				APIKey:  "sk-test-key",
+				BaseURL: "http://example.com/api",
+			},
+			wantErr: true,
+			errMsg:  "invalid base URL",
+		},
+	}
+
+	for _, tt := range tests {
+		t.Run(tt.name, func(t *testing.T) {
+			// Test via CreateVectorStore which validates config
+			_, err := CreateVectorStore(nil, tt.cfg, "test-store")
+			if tt.wantErr {
+				assert.Error(t, err)
+				if tt.errMsg != "" {
+					assert.Contains(t, err.Error(), tt.errMsg)
+				}
+			}
+		})
+	}
+}
+
+func TestUploadFileToVectorStore_Validation(t *testing.T) {
+	tests := []struct {
+		name    string
+		cfg     FileStoreConfig
+		storeID string
+		wantErr bool
+		errMsg  string
+	}{
+		{
+			name:    "empty store ID",
+			cfg:     FileStoreConfig{APIKey: "sk-test"},
+			storeID: "",
+			wantErr: true,
+			errMsg:  "store ID is required",
+		},
+		{
+			name:    "whitespace store ID",
+			cfg:     FileStoreConfig{APIKey: "sk-test"},
+			storeID: "   ",
+			wantErr: true,
+			errMsg:  "store ID is required",
+		},
+	}
+
+	for _, tt := range tests {
+		t.Run(tt.name, func(t *testing.T) {
+			_, err := UploadFileToVectorStore(nil, tt.cfg, tt.storeID, "test.txt", nil)
+			if tt.wantErr {
+				assert.Error(t, err)
+				if tt.errMsg != "" {
+					assert.Contains(t, err.Error(), tt.errMsg)
+				}
+			}
+		})
+	}
+}
```

---

### 2.4 MEDIUM: Gemini FileStore Office File Detection Tests

**File: internal/provider/gemini/filestore_test.go (NEW FILE)**

```diff
+package gemini
+
+import (
+	"testing"
+
+	"github.com/stretchr/testify/assert"
+)
+
+func TestIsOfficeFile(t *testing.T) {
+	tests := []struct {
+		mimeType string
+		expected bool
+	}{
+		// Modern Office formats
+		{"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", true},         // .xlsx
+		{"application/vnd.openxmlformats-officedocument.wordprocessingml.document", true},   // .docx
+		{"application/vnd.openxmlformats-officedocument.presentationml.presentation", true}, // .pptx
+		// Legacy Office formats
+		{"application/vnd.ms-excel", true},      // .xls
+		{"application/msword", true},            // .doc
+		{"application/vnd.ms-powerpoint", true}, // .ppt
+		// CSV
+		{"text/csv", true},
+		// Non-office files
+		{"application/pdf", false},
+		{"text/plain", false},
+		{"image/png", false},
+		{"application/json", false},
+		{"", false},
+	}
+
+	for _, tt := range tests {
+		t.Run(tt.mimeType, func(t *testing.T) {
+			result := isOfficeFile(tt.mimeType)
+			assert.Equal(t, tt.expected, result, "MIME type: %s", tt.mimeType)
+		})
+	}
+}
+
+func TestFileStoreConfig_GetBaseURL(t *testing.T) {
+	tests := []struct {
+		name     string
+		cfg      FileStoreConfig
+		expected string
+	}{
+		{
+			name:     "default URL when empty",
+			cfg:      FileStoreConfig{},
+			expected: "https://generativelanguage.googleapis.com/v1beta",
+		},
+		{
+			name: "custom URL when set",
+			cfg: FileStoreConfig{
+				BaseURL: "https://custom.example.com/api",
+			},
+			expected: "https://custom.example.com/api",
+		},
+	}
+
+	for _, tt := range tests {
+		t.Run(tt.name, func(t *testing.T) {
+			result := tt.cfg.getBaseURL()
+			assert.Equal(t, tt.expected, result)
+		})
+	}
+}
```

---

## 3. FIXES - Bugs, Issues, and Code Smells

### 3.1 HIGH: Doppler Retry Logic Returns Wrong Error on Max Retries

**Impact:** When max retries are exceeded, the error message doesn't include the actual last error details clearly.

**File: internal/tenant/doppler.go:124**

```diff
-	return nil, fmt.Errorf("after %d attempts: %w", maxRetries, lastErr)
+	return nil, fmt.Errorf("doppler fetch failed after %d attempts, last error: %w", maxRetries, lastErr)
```

---

### 3.2 MEDIUM: Provider Options Building Duplicated

**Impact:** Code duplication across 6+ locations increases maintenance burden and risk of inconsistency.

**Recommendation:** Extract to helper function in new file `internal/provider/options.go`:

```diff
+package provider
+
+import (
+	"fmt"
+
+	"github.com/openai/openai-go/option"
+	"github.com/ai8future/airborne/internal/validation"
+)
+
+// BuildOpenAIOptions constructs request options for OpenAI client.
+// Validates base URL if provided.
+func BuildOpenAIOptions(apiKey, baseURL string) ([]option.RequestOption, error) {
+	opts := []option.RequestOption{
+		option.WithAPIKey(apiKey),
+	}
+	if baseURL != "" {
+		if err := validation.ValidateProviderURL(baseURL); err != nil {
+			return nil, fmt.Errorf("invalid base URL: %w", err)
+		}
+		opts = append(opts, option.WithBaseURL(baseURL))
+	}
+	return opts, nil
+}
```

Then update all locations (example for `internal/provider/openai/filestore.go:56-64`):

```diff
 func CreateVectorStore(ctx context.Context, cfg FileStoreConfig, name string) (*FileStoreResult, error) {
 	if strings.TrimSpace(cfg.APIKey) == "" {
 		return nil, fmt.Errorf("API key is required")
 	}

-	opts := []option.RequestOption{
-		option.WithAPIKey(cfg.APIKey),
-	}
-	if cfg.BaseURL != "" {
-		if err := validation.ValidateProviderURL(cfg.BaseURL); err != nil {
-			return nil, fmt.Errorf("invalid base URL: %w", err)
-		}
-		opts = append(opts, option.WithBaseURL(cfg.BaseURL))
-	}
+	opts, err := provider.BuildOpenAIOptions(cfg.APIKey, cfg.BaseURL)
+	if err != nil {
+		return nil, err
+	}

 	client := openai.NewClient(opts...)
```

---

### 3.3 LOW: Polling Uses Fixed Interval Instead of Exponential Backoff

**Impact:** Inefficient API usage during file processing waits.

**File: internal/provider/openai/filestore.go:177-213**

```diff
+const (
+	vectorStoreInitialInterval = 1 * time.Second
+	vectorStoreMaxInterval     = 10 * time.Second
+)
+
 // waitForFileProcessing polls until the file is processed or timeout.
 func waitForFileProcessing(ctx context.Context, client openai.Client, storeID, vsFileID string) (string, error) {
 	timeoutCtx, cancel := context.WithTimeout(ctx, vectorStorePollingTimeout)
 	defer cancel()

-	ticker := time.NewTicker(vectorStorePollingInterval)
-	defer ticker.Stop()
+	interval := vectorStoreInitialInterval

 	for {
 		select {
 		case <-timeoutCtx.Done():
 			return "in_progress", fmt.Errorf("file processing timeout")
-		case <-ticker.C:
+		default:
+			time.Sleep(interval)
+			// Exponential backoff with cap
+			if interval < vectorStoreMaxInterval {
+				interval = time.Duration(float64(interval) * 1.5)
+				if interval > vectorStoreMaxInterval {
+					interval = vectorStoreMaxInterval
+				}
+			}
+
 			vsFile, err := client.VectorStores.Files.Get(timeoutCtx, storeID, vsFileID)
 			if err != nil {
 				return "unknown", fmt.Errorf("get file status: %w", err)
 			}
```

---

### 3.4 LOW: Inconsistent Receiver Names

**Impact:** Reduces code readability.

**Locations:**
- `internal/db/repository.go` uses `r` for Repository
- `internal/service/chat.go` uses `s` for Service
- `internal/pricing/pricing.go` uses `p` for Pricer

**Recommendation:** Use more descriptive 3-4 character receivers:

```diff
-func (r *Repository) CreateThread(ctx context.Context, thread *Thread) error {
+func (repo *Repository) CreateThread(ctx context.Context, thread *Thread) error {

-func (s *Service) Chat(ctx context.Context, req *ChatRequest) error {
+func (svc *Service) Chat(ctx context.Context, req *ChatRequest) error {
```

---

## 4. REFACTOR - Code Quality Improvement Opportunities

### 4.1 HIGH PRIORITY

#### 4.1.1 Extract Error Response Logging Helper

**Problem:** 16 locations have `body, _ := io.ReadAll(resp.Body)` pattern.

**Recommendation:** Create `internal/httputil/response.go`:
```go
package httputil

func ReadErrorBody(resp *http.Response) string {
    body, err := io.ReadAll(resp.Body)
    if err != nil {
        slog.Warn("failed to read error response", "error", err)
        return "<unreadable>"
    }
    return string(body)
}
```

#### 4.1.2 Implement Null Object Pattern for Optional Services

**Problem:** Many nil checks for optional services (RAG, ImageGen, DB).

**Recommendation:** Create null implementations:
```go
type NullRAGService struct{}
func (n *NullRAGService) Retrieve(ctx context.Context, ...) ([]Result, error) {
    return []Result{}, nil
}
```

#### 4.1.3 Extract Provider-Specific File Upload Logic

**Problem:** `UploadFileToFileSearchStore` has complex conditional logic for different providers.

**Recommendation:** Use strategy pattern:
```go
type FileUploader interface {
    Upload(ctx context.Context, storeID string, file io.Reader) (*UploadResult, error)
}

type OpenAIUploader struct { cfg FileStoreConfig }
type GeminiUploader struct { cfg FileStoreConfig }
```

### 4.2 MEDIUM PRIORITY

#### 4.2.1 Reduce Database Query Duplication

**Location:** `internal/db/repository.go:325-370` - `GetActivityFeedAllTenants()`

**Problem:** Three nearly identical UNION queries for each tenant.

**Recommendation:** Generate queries dynamically from `ValidTenantIDs` map.

#### 4.2.2 Consolidate Context Timeout Creation

**Problem:** Multiple places create `context.WithTimeout(context.Background(), ...)`.

**Recommendation:** Create helper:
```go
func BackgroundTimeout(d time.Duration) (context.Context, context.CancelFunc) {
    return context.WithTimeout(context.Background(), d)
}
```

#### 4.2.3 Standardize Error Wrapping

**Problem:** Inconsistent use of `fmt.Errorf` with `%w` vs without.

**Recommendation:** Always use `%w` for error wrapping to preserve error chain.

### 4.3 LOW PRIORITY

#### 4.3.1 Consistent Naming for File Stores

**Problem:** Mixed terminology: `fileStore`, `vectorStore`, `FileSearchStore`.

**Recommendation:** Document the distinction or consolidate naming.

#### 4.3.2 Extract SQL Query Constants

**Problem:** SQL queries built with `fmt.Sprintf()` at runtime.

**Recommendation:** Use query templates or builder pattern for complex queries.

#### 4.3.3 Add Structured Logging Context

**Problem:** Some log calls lack sufficient context for debugging.

**Recommendation:** Add `slog.With()` at function entry for consistent context.

---

## Summary

| Category | Critical | High | Medium | Low |
|----------|----------|------|--------|-----|
| Audit    | 0        | 1    | 2      | 2   |
| Tests    | 2        | 2    | 1      | 0   |
| Fixes    | 0        | 1    | 1      | 2   |
| Refactor | -        | 3    | 3      | 3   |

**Top 3 Priorities:**
1. Add database repository tests (`internal/db/repository_test.go`)
2. Fix IO read error ignoring pattern (16 locations)
3. Add pricing calculation tests (`internal/pricing/pricing_test.go`)

**Security Posture:** Good - SSRF protection, parameterized queries, secrets management all properly implemented.

**Test Coverage Gap:** Database layer and file operations are completely untested, representing significant risk.
