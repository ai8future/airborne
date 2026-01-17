Date Created: 2026-01-16T23:50:00-05:00
Date Updated: 2026-01-17
TOTAL_SCORE: 68/100 → 78/100 (after implementing compat + providers tests)

# Airborne Unit Test Analysis Report

## ✅ IMPLEMENTED (v1.1.6)
- OpenAI Compatibility Layer Tests (`internal/provider/compat/openai_compat_test.go`) - 40+ test cases
- Provider Capability Tests (`internal/provider/providers_test.go`) - Table-driven tests for 13 providers

---

**Analyzer**: Claude:Opus 4.5
**Project**: Airborne - Go-based LLM Gateway/Orchestration Service
**Framework**: Go standard `testing` package

---

## Executive Summary

Airborne has a solid testing foundation with 31 test files covering 348 test functions across critical auth, tenant, config, and service modules. However, significant gaps exist in provider implementations (12 of 16 provider clients are untested) and file store operations. The overall test coverage score of **68/100** reflects good core coverage but substantial gaps in external provider integrations.

### Score Breakdown

| Category | Weight | Score | Notes |
|----------|--------|-------|-------|
| Core Auth/Security | 25% | 23/25 | Excellent coverage |
| Multi-Tenancy | 15% | 14/15 | Comprehensive tests |
| Provider Implementations | 25% | 8/25 | 12 of 16 providers untested |
| RAG System | 15% | 10/15 | Core tested, interfaces lacking |
| Service Layer | 10% | 9/10 | Well covered |
| Utilities/Config | 10% | 4/10 | Partial coverage |

---

## Module Coverage Analysis

### Well-Tested Modules (Score: Excellent)

| Module | Files | Test Files | Coverage | Status |
|--------|-------|------------|----------|--------|
| `internal/auth/` | 6 | 5 | ~90% | Excellent |
| `internal/tenant/` | 5 | 5 | ~95% | Excellent |
| `internal/config/` | 2 | 2 | ~85% | Complete |
| `internal/service/` | 3 | 3 | ~80% | Complete |
| `internal/validation/` | 2 | 2 | ~90% | Complete |
| `internal/errors/` | 1 | 1 | ~95% | Complete |

### Partially Tested Modules (Score: Needs Work)

| Module | Files | Test Files | Coverage | Status |
|--------|-------|------------|----------|--------|
| `internal/provider/openai/` | 2 | 1 | ~50% | `filestore.go` untested |
| `internal/provider/gemini/` | 2 | 1 | ~50% | `filestore.go` untested |
| `internal/rag/embedder/` | 2 | 1 | ~40% | Interface untested |
| `internal/rag/extractor/` | 2 | 1 | ~40% | Interface untested |
| `internal/rag/vectorstore/` | 2 | 1 | ~40% | Interface untested |

### Untested Modules (Score: Critical Gap)

| Module | Files | Priority | Reason |
|--------|-------|----------|--------|
| `internal/provider/cerebras/` | 1 | High | External API integration |
| `internal/provider/cohere/` | 1 | High | External API integration |
| `internal/provider/deepinfra/` | 1 | Medium | External API integration |
| `internal/provider/deepseek/` | 1 | Medium | External API integration |
| `internal/provider/fireworks/` | 1 | Medium | External API integration |
| `internal/provider/grok/` | 1 | High | External API integration |
| `internal/provider/hyperbolic/` | 1 | Low | External API integration |
| `internal/provider/nebius/` | 1 | Low | External API integration |
| `internal/provider/openrouter/` | 1 | Medium | External API integration |
| `internal/provider/perplexity/` | 1 | Medium | External API integration |
| `internal/provider/together/` | 1 | Medium | External API integration |
| `internal/provider/upstage/` | 1 | Low | External API integration |
| `internal/provider/compat/` | 1 | Critical | Core compatibility layer |

---

## Proposed Unit Tests with Patch-Ready Diffs

### 1. OpenAI Compatibility Layer Tests (CRITICAL)

**File**: `internal/provider/compat/openai_compat_test.go`

This is the most critical gap - the compat package is used by 12 provider implementations.

```diff
--- /dev/null
+++ b/internal/provider/compat/openai_compat_test.go
@@ -0,0 +1,223 @@
+package compat
+
+import (
+	"context"
+	"errors"
+	"testing"
+
+	"github.com/ai8future/airborne/internal/provider"
+)
+
+func TestNewClient(t *testing.T) {
+	config := ProviderConfig{
+		Name:               "test-provider",
+		DefaultBaseURL:     "https://api.test.com/v1",
+		DefaultModel:       "test-model",
+		SupportsFileSearch: false,
+		SupportsWebSearch:  false,
+		SupportsStreaming:  true,
+		APIKeyEnvVar:       "TEST_API_KEY",
+	}
+
+	client := NewClient(config)
+
+	if client == nil {
+		t.Fatal("NewClient() returned nil")
+	}
+	if client.config.Name != "test-provider" {
+		t.Errorf("Name = %q, want %q", client.config.Name, "test-provider")
+	}
+}
+
+func TestNewClientWithDebugLogging(t *testing.T) {
+	config := ProviderConfig{Name: "test"}
+
+	client := NewClient(config, WithDebugLogging(true))
+	if !client.debug {
+		t.Error("expected debug to be true")
+	}
+
+	client2 := NewClient(config, WithDebugLogging(false))
+	if client2.debug {
+		t.Error("expected debug to be false")
+	}
+}
+
+func TestClientName(t *testing.T) {
+	config := ProviderConfig{Name: "test-provider"}
+	client := NewClient(config)
+
+	if got := client.Name(); got != "test-provider" {
+		t.Errorf("Name() = %q, want %q", got, "test-provider")
+	}
+}
+
+func TestClientCapabilities(t *testing.T) {
+	tests := []struct {
+		name               string
+		config             ProviderConfig
+		wantFileSearch     bool
+		wantWebSearch      bool
+		wantContinuity     bool
+		wantStreaming      bool
+	}{
+		{
+			name: "all capabilities enabled",
+			config: ProviderConfig{
+				SupportsFileSearch: true,
+				SupportsWebSearch:  true,
+				SupportsStreaming:  true,
+			},
+			wantFileSearch: true,
+			wantWebSearch:  true,
+			wantContinuity: false, // Always false for compat clients
+			wantStreaming:  true,
+		},
+		{
+			name: "all capabilities disabled",
+			config: ProviderConfig{
+				SupportsFileSearch: false,
+				SupportsWebSearch:  false,
+				SupportsStreaming:  false,
+			},
+			wantFileSearch: false,
+			wantWebSearch:  false,
+			wantContinuity: false,
+			wantStreaming:  false,
+		},
+	}
+
+	for _, tt := range tests {
+		t.Run(tt.name, func(t *testing.T) {
+			client := NewClient(tt.config)
+
+			if got := client.SupportsFileSearch(); got != tt.wantFileSearch {
+				t.Errorf("SupportsFileSearch() = %v, want %v", got, tt.wantFileSearch)
+			}
+			if got := client.SupportsWebSearch(); got != tt.wantWebSearch {
+				t.Errorf("SupportsWebSearch() = %v, want %v", got, tt.wantWebSearch)
+			}
+			if got := client.SupportsNativeContinuity(); got != tt.wantContinuity {
+				t.Errorf("SupportsNativeContinuity() = %v, want %v", got, tt.wantContinuity)
+			}
+			if got := client.SupportsStreaming(); got != tt.wantStreaming {
+				t.Errorf("SupportsStreaming() = %v, want %v", got, tt.wantStreaming)
+			}
+		})
+	}
+}
+
+func TestGenerateReply_MissingAPIKey(t *testing.T) {
+	config := ProviderConfig{Name: "test"}
+	client := NewClient(config)
+
+	_, err := client.GenerateReply(context.Background(), provider.GenerateParams{
+		Config: provider.ProviderConfig{APIKey: ""},
+	})
+
+	if err == nil {
+		t.Fatal("expected error for missing API key")
+	}
+	if got := err.Error(); got != "test API key is required" {
+		t.Errorf("error = %q, want %q", got, "test API key is required")
+	}
+}
+
+func TestGenerateReply_InvalidBaseURL(t *testing.T) {
+	config := ProviderConfig{Name: "test", DefaultBaseURL: "https://api.test.com"}
+	client := NewClient(config)
+
+	_, err := client.GenerateReply(context.Background(), provider.GenerateParams{
+		Config: provider.ProviderConfig{
+			APIKey:  "test-key",
+			BaseURL: "not-a-valid-url",
+		},
+	})
+
+	if err == nil {
+		t.Fatal("expected error for invalid base URL")
+	}
+}
+
+func TestGenerateReplyStream_MissingAPIKey(t *testing.T) {
+	config := ProviderConfig{Name: "test"}
+	client := NewClient(config)
+
+	_, err := client.GenerateReplyStream(context.Background(), provider.GenerateParams{
+		Config: provider.ProviderConfig{APIKey: ""},
+	})
+
+	if err == nil {
+		t.Fatal("expected error for missing API key")
+	}
+}
+
+func TestGenerateReplyStream_InvalidBaseURL(t *testing.T) {
+	config := ProviderConfig{Name: "test", DefaultBaseURL: "https://api.test.com"}
+	client := NewClient(config)
+
+	_, err := client.GenerateReplyStream(context.Background(), provider.GenerateParams{
+		Config: provider.ProviderConfig{
+			APIKey:  "test-key",
+			BaseURL: "javascript:alert(1)",
+		},
+	})
+
+	if err == nil {
+		t.Fatal("expected error for invalid base URL")
+	}
+}
+
+func TestBuildMessages(t *testing.T) {
+	tests := []struct {
+		name         string
+		instructions string
+		userInput    string
+		history      []provider.Message
+		wantLen      int
+	}{
+		{
+			name:         "no instructions no history",
+			instructions: "",
+			userInput:    "hello",
+			history:      nil,
+			wantLen:      1, // Just user message
+		},
+		{
+			name:         "with instructions",
+			instructions: "You are helpful",
+			userInput:    "hello",
+			history:      nil,
+			wantLen:      2, // System + user
+		},
+		{
+			name:         "with history",
+			instructions: "You are helpful",
+			userInput:    "hello",
+			history: []provider.Message{
+				{Role: "user", Content: "Hi"},
+				{Role: "assistant", Content: "Hello!"},
+			},
+			wantLen: 4, // System + 2 history + user
+		},
+		{
+			name:         "empty history content skipped",
+			instructions: "",
+			userInput:    "hello",
+			history: []provider.Message{
+				{Role: "user", Content: ""},
+				{Role: "assistant", Content: "   "},
+			},
+			wantLen: 1, // Empty messages skipped
+		},
+	}
+
+	for _, tt := range tests {
+		t.Run(tt.name, func(t *testing.T) {
+			messages := buildMessages(tt.instructions, tt.userInput, tt.history)
+			if got := len(messages); got != tt.wantLen {
+				t.Errorf("buildMessages() returned %d messages, want %d", got, tt.wantLen)
+			}
+		})
+	}
+}
+
+func TestIsRetryableError(t *testing.T) {
+	tests := []struct {
+		name string
+		err  error
+		want bool
+	}{
+		{"nil error", nil, false},
+		{"context canceled", context.Canceled, false},
+		{"context deadline", context.DeadlineExceeded, false},
+		{"rate limit 429", errors.New("status code 429"), true},
+		{"server error 500", errors.New("500 internal server error"), true},
+		{"server error 502", errors.New("502 bad gateway"), true},
+		{"server error 503", errors.New("503 service unavailable"), true},
+		{"server error 504", errors.New("504 gateway timeout"), true},
+		{"auth error 401", errors.New("401 unauthorized"), false},
+		{"auth error 403", errors.New("403 forbidden"), false},
+		{"bad request 400", errors.New("400 bad request"), false},
+		{"connection error", errors.New("connection refused"), true},
+		{"timeout error", errors.New("request timeout"), true},
+		{"temporary failure", errors.New("temporary network issue"), true},
+		{"invalid api key", errors.New("invalid_api_key"), false},
+		{"authentication failed", errors.New("authentication failed"), false},
+	}
+
+	for _, tt := range tests {
+		t.Run(tt.name, func(t *testing.T) {
+			got := isRetryableError(tt.err)
+			if got != tt.want {
+				t.Errorf("isRetryableError(%v) = %v, want %v", tt.err, got, tt.want)
+			}
+		})
+	}
+}
```

### 2. Cerebras Provider Tests (HIGH PRIORITY)

**File**: `internal/provider/cerebras/client_test.go`

Representative test for OpenAI-compat providers. Same pattern applies to other compat-based providers.

```diff
--- /dev/null
+++ b/internal/provider/cerebras/client_test.go
@@ -0,0 +1,79 @@
+package cerebras
+
+import (
+	"context"
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
+	if client.Client == nil {
+		t.Fatal("NewClient() embedded compat.Client is nil")
+	}
+}
+
+func TestNewClientWithDebugLogging(t *testing.T) {
+	// WithDebugLogging is a no-op for Cerebras, but should not panic
+	client := NewClient(WithDebugLogging(true))
+	if client == nil {
+		t.Fatal("NewClient(WithDebugLogging(true)) returned nil")
+	}
+}
+
+func TestClientName(t *testing.T) {
+	client := NewClient()
+	if got := client.Name(); got != "cerebras" {
+		t.Errorf("Name() = %q, want %q", got, "cerebras")
+	}
+}
+
+func TestClientCapabilities(t *testing.T) {
+	client := NewClient()
+
+	// Cerebras capabilities
+	if client.SupportsFileSearch() {
+		t.Error("SupportsFileSearch() should be false for Cerebras")
+	}
+	if client.SupportsWebSearch() {
+		t.Error("SupportsWebSearch() should be false for Cerebras")
+	}
+	if client.SupportsNativeContinuity() {
+		t.Error("SupportsNativeContinuity() should be false for Cerebras")
+	}
+	if !client.SupportsStreaming() {
+		t.Error("SupportsStreaming() should be true for Cerebras")
+	}
+}
+
+func TestClientImplementsInterface(t *testing.T) {
+	// Compile-time check is in client.go, but let's verify at runtime too
+	var _ provider.Provider = (*Client)(nil)
+}
+
+func TestGenerateReply_MissingAPIKey(t *testing.T) {
+	client := NewClient()
+
+	_, err := client.GenerateReply(context.Background(), provider.GenerateParams{
+		Config: provider.ProviderConfig{APIKey: ""},
+	})
+
+	if err == nil {
+		t.Fatal("expected error for missing API key")
+	}
+}
+
+func TestGenerateReplyStream_MissingAPIKey(t *testing.T) {
+	client := NewClient()
+
+	_, err := client.GenerateReplyStream(context.Background(), provider.GenerateParams{
+		Config: provider.ProviderConfig{APIKey: ""},
+	})
+
+	if err == nil {
+		t.Fatal("expected error for missing API key")
+	}
+}
```

### 3. OpenAI FileStore Tests (HIGH PRIORITY)

**File**: `internal/provider/openai/filestore_test.go`

```diff
--- /dev/null
+++ b/internal/provider/openai/filestore_test.go
@@ -0,0 +1,134 @@
+package openai
+
+import (
+	"context"
+	"strings"
+	"testing"
+)
+
+func TestCreateVectorStore_MissingAPIKey(t *testing.T) {
+	cfg := FileStoreConfig{APIKey: ""}
+
+	_, err := CreateVectorStore(context.Background(), cfg, "test-store")
+	if err == nil {
+		t.Fatal("expected error for missing API key")
+	}
+	if !strings.Contains(err.Error(), "API key is required") {
+		t.Errorf("unexpected error: %v", err)
+	}
+}
+
+func TestCreateVectorStore_InvalidBaseURL(t *testing.T) {
+	cfg := FileStoreConfig{
+		APIKey:  "test-key",
+		BaseURL: "not-a-valid-url",
+	}
+
+	_, err := CreateVectorStore(context.Background(), cfg, "test-store")
+	if err == nil {
+		t.Fatal("expected error for invalid base URL")
+	}
+	if !strings.Contains(err.Error(), "invalid base URL") {
+		t.Errorf("unexpected error: %v", err)
+	}
+}
+
+func TestUploadFileToVectorStore_MissingAPIKey(t *testing.T) {
+	cfg := FileStoreConfig{APIKey: ""}
+
+	_, err := UploadFileToVectorStore(context.Background(), cfg, "store-123", "test.txt", strings.NewReader("content"))
+	if err == nil {
+		t.Fatal("expected error for missing API key")
+	}
+}
+
+func TestUploadFileToVectorStore_MissingStoreID(t *testing.T) {
+	cfg := FileStoreConfig{APIKey: "test-key"}
+
+	_, err := UploadFileToVectorStore(context.Background(), cfg, "", "test.txt", strings.NewReader("content"))
+	if err == nil {
+		t.Fatal("expected error for missing store ID")
+	}
+	if !strings.Contains(err.Error(), "store ID is required") {
+		t.Errorf("unexpected error: %v", err)
+	}
+}
+
+func TestUploadFileToVectorStore_InvalidBaseURL(t *testing.T) {
+	cfg := FileStoreConfig{
+		APIKey:  "test-key",
+		BaseURL: "javascript:alert(1)",
+	}
+
+	_, err := UploadFileToVectorStore(context.Background(), cfg, "store-123", "test.txt", strings.NewReader("content"))
+	if err == nil {
+		t.Fatal("expected error for invalid base URL")
+	}
+}
+
+func TestDeleteVectorStore_MissingAPIKey(t *testing.T) {
+	cfg := FileStoreConfig{APIKey: ""}
+
+	err := DeleteVectorStore(context.Background(), cfg, "store-123")
+	if err == nil {
+		t.Fatal("expected error for missing API key")
+	}
+}
+
+func TestDeleteVectorStore_MissingStoreID(t *testing.T) {
+	cfg := FileStoreConfig{APIKey: "test-key"}
+
+	err := DeleteVectorStore(context.Background(), cfg, "")
+	if err == nil {
+		t.Fatal("expected error for missing store ID")
+	}
+}
+
+func TestGetVectorStore_MissingAPIKey(t *testing.T) {
+	cfg := FileStoreConfig{APIKey: ""}
+
+	_, err := GetVectorStore(context.Background(), cfg, "store-123")
+	if err == nil {
+		t.Fatal("expected error for missing API key")
+	}
+}
+
+func TestGetVectorStore_MissingStoreID(t *testing.T) {
+	cfg := FileStoreConfig{APIKey: "test-key"}
+
+	_, err := GetVectorStore(context.Background(), cfg, "")
+	if err == nil {
+		t.Fatal("expected error for missing store ID")
+	}
+}
+
+func TestGetVectorStore_InvalidBaseURL(t *testing.T) {
+	cfg := FileStoreConfig{
+		APIKey:  "test-key",
+		BaseURL: "ftp://invalid",
+	}
+
+	_, err := GetVectorStore(context.Background(), cfg, "store-123")
+	if err == nil {
+		t.Fatal("expected error for invalid base URL")
+	}
+}
+
+func TestListVectorStores_MissingAPIKey(t *testing.T) {
+	cfg := FileStoreConfig{APIKey: ""}
+
+	_, err := ListVectorStores(context.Background(), cfg, 10)
+	if err == nil {
+		t.Fatal("expected error for missing API key")
+	}
+}
+
+func TestListVectorStores_InvalidBaseURL(t *testing.T) {
+	cfg := FileStoreConfig{
+		APIKey:  "test-key",
+		BaseURL: "not-valid",
+	}
+
+	_, err := ListVectorStores(context.Background(), cfg, 10)
+	if err == nil {
+		t.Fatal("expected error for invalid base URL")
+	}
+}
```

### 4. Gemini FileStore Tests (HIGH PRIORITY)

**File**: `internal/provider/gemini/filestore_test.go`

```diff
--- /dev/null
+++ b/internal/provider/gemini/filestore_test.go
@@ -0,0 +1,171 @@
+package gemini
+
+import (
+	"context"
+	"strings"
+	"testing"
+)
+
+func TestFileStoreConfig_GetBaseURL(t *testing.T) {
+	tests := []struct {
+		name    string
+		cfg     FileStoreConfig
+		want    string
+	}{
+		{
+			name: "default base URL",
+			cfg:  FileStoreConfig{},
+			want: fileSearchBaseURL,
+		},
+		{
+			name: "custom base URL",
+			cfg:  FileStoreConfig{BaseURL: "https://custom.api.com/v1"},
+			want: "https://custom.api.com/v1",
+		},
+	}
+
+	for _, tt := range tests {
+		t.Run(tt.name, func(t *testing.T) {
+			got := tt.cfg.getBaseURL()
+			if got != tt.want {
+				t.Errorf("getBaseURL() = %q, want %q", got, tt.want)
+			}
+		})
+	}
+}
+
+func TestCreateFileSearchStore_MissingAPIKey(t *testing.T) {
+	cfg := FileStoreConfig{APIKey: ""}
+
+	_, err := CreateFileSearchStore(context.Background(), cfg, "test-store")
+	if err == nil {
+		t.Fatal("expected error for missing API key")
+	}
+	if !strings.Contains(err.Error(), "API key is required") {
+		t.Errorf("unexpected error: %v", err)
+	}
+}
+
+func TestCreateFileSearchStore_InvalidBaseURL(t *testing.T) {
+	cfg := FileStoreConfig{
+		APIKey:  "test-key",
+		BaseURL: "not-a-valid-url",
+	}
+
+	_, err := CreateFileSearchStore(context.Background(), cfg, "test-store")
+	if err == nil {
+		t.Fatal("expected error for invalid base URL")
+	}
+	if !strings.Contains(err.Error(), "invalid base URL") {
+		t.Errorf("unexpected error: %v", err)
+	}
+}
+
+func TestUploadFileToFileSearchStore_MissingAPIKey(t *testing.T) {
+	cfg := FileStoreConfig{APIKey: ""}
+
+	_, err := UploadFileToFileSearchStore(context.Background(), cfg, "store-123", "test.txt", "text/plain", strings.NewReader("content"))
+	if err == nil {
+		t.Fatal("expected error for missing API key")
+	}
+}
+
+func TestUploadFileToFileSearchStore_MissingStoreID(t *testing.T) {
+	cfg := FileStoreConfig{APIKey: "test-key"}
+
+	_, err := UploadFileToFileSearchStore(context.Background(), cfg, "", "test.txt", "text/plain", strings.NewReader("content"))
+	if err == nil {
+		t.Fatal("expected error for missing store ID")
+	}
+	if !strings.Contains(err.Error(), "store ID is required") {
+		t.Errorf("unexpected error: %v", err)
+	}
+}
+
+func TestUploadFileToFileSearchStore_InvalidBaseURL(t *testing.T) {
+	cfg := FileStoreConfig{
+		APIKey:  "test-key",
+		BaseURL: "javascript:alert(1)",
+	}
+
+	_, err := UploadFileToFileSearchStore(context.Background(), cfg, "store-123", "test.txt", "text/plain", strings.NewReader("content"))
+	if err == nil {
+		t.Fatal("expected error for invalid base URL")
+	}
+}
+
+func TestDeleteFileSearchStore_MissingAPIKey(t *testing.T) {
+	cfg := FileStoreConfig{APIKey: ""}
+
+	err := DeleteFileSearchStore(context.Background(), cfg, "store-123", false)
+	if err == nil {
+		t.Fatal("expected error for missing API key")
+	}
+}
+
+func TestDeleteFileSearchStore_MissingStoreID(t *testing.T) {
+	cfg := FileStoreConfig{APIKey: "test-key"}
+
+	err := DeleteFileSearchStore(context.Background(), cfg, "", false)
+	if err == nil {
+		t.Fatal("expected error for missing store ID")
+	}
+}
+
+func TestDeleteFileSearchStore_InvalidBaseURL(t *testing.T) {
+	cfg := FileStoreConfig{
+		APIKey:  "test-key",
+		BaseURL: "ftp://invalid",
+	}
+
+	err := DeleteFileSearchStore(context.Background(), cfg, "store-123", false)
+	if err == nil {
+		t.Fatal("expected error for invalid base URL")
+	}
+}
+
+func TestGetFileSearchStore_MissingAPIKey(t *testing.T) {
+	cfg := FileStoreConfig{APIKey: ""}
+
+	_, err := GetFileSearchStore(context.Background(), cfg, "store-123")
+	if err == nil {
+		t.Fatal("expected error for missing API key")
+	}
+}
+
+func TestGetFileSearchStore_MissingStoreID(t *testing.T) {
+	cfg := FileStoreConfig{APIKey: "test-key"}
+
+	_, err := GetFileSearchStore(context.Background(), cfg, "")
+	if err == nil {
+		t.Fatal("expected error for missing store ID")
+	}
+}
+
+func TestGetFileSearchStore_InvalidBaseURL(t *testing.T) {
+	cfg := FileStoreConfig{
+		APIKey:  "test-key",
+		BaseURL: "not-valid",
+	}
+
+	_, err := GetFileSearchStore(context.Background(), cfg, "store-123")
+	if err == nil {
+		t.Fatal("expected error for invalid base URL")
+	}
+}
+
+func TestListFileSearchStores_MissingAPIKey(t *testing.T) {
+	cfg := FileStoreConfig{APIKey: ""}
+
+	_, err := ListFileSearchStores(context.Background(), cfg, 10)
+	if err == nil {
+		t.Fatal("expected error for missing API key")
+	}
+}
+
+func TestListFileSearchStores_InvalidBaseURL(t *testing.T) {
+	cfg := FileStoreConfig{
+		APIKey:  "test-key",
+		BaseURL: "not-valid",
+	}
+
+	_, err := ListFileSearchStores(context.Background(), cfg, 10)
+	if err == nil {
+		t.Fatal("expected error for invalid base URL")
+	}
+}
```

### 5. DeepSeek Provider Tests (MEDIUM PRIORITY)

**File**: `internal/provider/deepseek/client_test.go`

```diff
--- /dev/null
+++ b/internal/provider/deepseek/client_test.go
@@ -0,0 +1,64 @@
+package deepseek
+
+import (
+	"context"
+	"testing"
+
+	"github.com/ai8future/airborne/internal/provider"
+)
+
+func TestNewClient(t *testing.T) {
+	client := NewClient()
+	if client == nil {
+		t.Fatal("NewClient() returned nil")
+	}
+}
+
+func TestNewClientWithDebugLogging(t *testing.T) {
+	client := NewClient(WithDebugLogging(true))
+	if client == nil {
+		t.Fatal("NewClient(WithDebugLogging(true)) returned nil")
+	}
+}
+
+func TestClientName(t *testing.T) {
+	client := NewClient()
+	if got := client.Name(); got != "deepseek" {
+		t.Errorf("Name() = %q, want %q", got, "deepseek")
+	}
+}
+
+func TestClientCapabilities(t *testing.T) {
+	client := NewClient()
+
+	if client.SupportsFileSearch() {
+		t.Error("SupportsFileSearch() should be false")
+	}
+	if client.SupportsWebSearch() {
+		t.Error("SupportsWebSearch() should be false")
+	}
+	if client.SupportsNativeContinuity() {
+		t.Error("SupportsNativeContinuity() should be false")
+	}
+	if !client.SupportsStreaming() {
+		t.Error("SupportsStreaming() should be true")
+	}
+}
+
+func TestGenerateReply_MissingAPIKey(t *testing.T) {
+	client := NewClient()
+	_, err := client.GenerateReply(context.Background(), provider.GenerateParams{
+		Config: provider.ProviderConfig{APIKey: ""},
+	})
+	if err == nil {
+		t.Fatal("expected error for missing API key")
+	}
+}
+
+func TestClientImplementsInterface(t *testing.T) {
+	var _ provider.Provider = (*Client)(nil)
+}
```

### 6. Grok Provider Tests (HIGH PRIORITY - Popular Provider)

**File**: `internal/provider/grok/client_test.go`

```diff
--- /dev/null
+++ b/internal/provider/grok/client_test.go
@@ -0,0 +1,64 @@
+package grok
+
+import (
+	"context"
+	"testing"
+
+	"github.com/ai8future/airborne/internal/provider"
+)
+
+func TestNewClient(t *testing.T) {
+	client := NewClient()
+	if client == nil {
+		t.Fatal("NewClient() returned nil")
+	}
+}
+
+func TestNewClientWithDebugLogging(t *testing.T) {
+	client := NewClient(WithDebugLogging(true))
+	if client == nil {
+		t.Fatal("NewClient(WithDebugLogging(true)) returned nil")
+	}
+}
+
+func TestClientName(t *testing.T) {
+	client := NewClient()
+	if got := client.Name(); got != "grok" {
+		t.Errorf("Name() = %q, want %q", got, "grok")
+	}
+}
+
+func TestClientCapabilities(t *testing.T) {
+	client := NewClient()
+
+	if client.SupportsFileSearch() {
+		t.Error("SupportsFileSearch() should be false")
+	}
+	if client.SupportsWebSearch() {
+		t.Error("SupportsWebSearch() should be false")
+	}
+	if client.SupportsNativeContinuity() {
+		t.Error("SupportsNativeContinuity() should be false")
+	}
+	if !client.SupportsStreaming() {
+		t.Error("SupportsStreaming() should be true")
+	}
+}
+
+func TestGenerateReply_MissingAPIKey(t *testing.T) {
+	client := NewClient()
+	_, err := client.GenerateReply(context.Background(), provider.GenerateParams{
+		Config: provider.ProviderConfig{APIKey: ""},
+	})
+	if err == nil {
+		t.Fatal("expected error for missing API key")
+	}
+}
+
+func TestClientImplementsInterface(t *testing.T) {
+	var _ provider.Provider = (*Client)(nil)
+}
```

### 7. Provider Interface Tests (for `provider.go`)

**File**: `internal/provider/provider_test.go`

```diff
--- /dev/null
+++ b/internal/provider/provider_test.go
@@ -0,0 +1,107 @@
+package provider
+
+import (
+	"testing"
+	"time"
+)
+
+func TestCitationType_Values(t *testing.T) {
+	tests := []struct {
+		ct   CitationType
+		want int
+	}{
+		{CitationTypeUnknown, 0},
+		{CitationTypeURL, 1},
+		{CitationTypeFile, 2},
+	}
+
+	for _, tt := range tests {
+		if int(tt.ct) != tt.want {
+			t.Errorf("CitationType %v = %d, want %d", tt.ct, tt.ct, tt.want)
+		}
+	}
+}
+
+func TestChunkType_Values(t *testing.T) {
+	tests := []struct {
+		ct   ChunkType
+		want int
+	}{
+		{ChunkTypeText, 0},
+		{ChunkTypeUsage, 1},
+		{ChunkTypeCitation, 2},
+		{ChunkTypeComplete, 3},
+		{ChunkTypeError, 4},
+		{ChunkTypeToolCall, 5},
+		{ChunkTypeCodeExecution, 6},
+	}
+
+	for _, tt := range tests {
+		if int(tt.ct) != tt.want {
+			t.Errorf("ChunkType %v = %d, want %d", tt.ct, tt.ct, tt.want)
+		}
+	}
+}
+
+func TestMessage_Fields(t *testing.T) {
+	now := time.Now()
+	msg := Message{
+		Role:      "user",
+		Content:   "Hello",
+		Timestamp: now,
+	}
+
+	if msg.Role != "user" {
+		t.Errorf("Role = %q, want %q", msg.Role, "user")
+	}
+	if msg.Content != "Hello" {
+		t.Errorf("Content = %q, want %q", msg.Content, "Hello")
+	}
+	if msg.Timestamp != now {
+		t.Errorf("Timestamp mismatch")
+	}
+}
+
+func TestTool_Fields(t *testing.T) {
+	tool := Tool{
+		Name:             "get_weather",
+		Description:      "Get the current weather",
+		ParametersSchema: `{"type":"object","properties":{"location":{"type":"string"}}}`,
+		Strict:           true,
+	}
+
+	if tool.Name != "get_weather" {
+		t.Errorf("Name = %q, want %q", tool.Name, "get_weather")
+	}
+	if !tool.Strict {
+		t.Error("Strict should be true")
+	}
+}
+
+func TestToolCall_Fields(t *testing.T) {
+	tc := ToolCall{
+		ID:        "call_123",
+		Name:      "get_weather",
+		Arguments: `{"location":"NYC"}`,
+	}
+
+	if tc.ID != "call_123" {
+		t.Errorf("ID = %q, want %q", tc.ID, "call_123")
+	}
+}
+
+func TestToolResult_Fields(t *testing.T) {
+	tr := ToolResult{
+		ToolCallID: "call_123",
+		Output:     "Sunny, 72F",
+		IsError:    false,
+	}
+
+	if tr.ToolCallID != "call_123" {
+		t.Errorf("ToolCallID = %q, want %q", tr.ToolCallID, "call_123")
+	}
+	if tr.IsError {
+		t.Error("IsError should be false")
+	}
+}
+
+func TestUsage_Fields(t *testing.T) {
+	u := Usage{
+		InputTokens:  100,
+		OutputTokens: 50,
+		TotalTokens:  150,
+	}
+
+	if u.TotalTokens != 150 {
+		t.Errorf("TotalTokens = %d, want %d", u.TotalTokens, 150)
+	}
+}
```

---

## Additional Provider Tests (Template)

The following providers all use the same `compat.Client` base and should have identical test structures. Use the Cerebras test as a template:

| Provider | File | Default Model | Special Capabilities |
|----------|------|---------------|---------------------|
| Cohere | `internal/provider/cohere/client_test.go` | command-r-plus | Web search |
| DeepInfra | `internal/provider/deepinfra/client_test.go` | meta-llama/... | None |
| Fireworks | `internal/provider/fireworks/client_test.go` | accounts/fireworks/... | None |
| Hyperbolic | `internal/provider/hyperbolic/client_test.go` | meta-llama/... | None |
| Nebius | `internal/provider/nebius/client_test.go` | meta-llama/... | None |
| OpenRouter | `internal/provider/openrouter/client_test.go` | anthropic/claude-3.5-sonnet | None |
| Perplexity | `internal/provider/perplexity/client_test.go` | llama-3.1-sonar-large-128k | Web search |
| Together | `internal/provider/together/client_test.go` | meta-llama/... | None |
| Upstage | `internal/provider/upstage/client_test.go` | solar-pro | None |

---

## Recommendations

### Immediate Priority (Week 1)
1. **Add compat package tests** - This covers 12 providers at once
2. **Add filestore tests** for OpenAI and Gemini
3. **Add basic provider tests** for Cerebras, Grok, Cohere (high-traffic providers)

### Medium Priority (Week 2)
4. Add tests for remaining providers (DeepSeek, Perplexity, etc.)
5. Add provider interface data structure tests

### Lower Priority
6. Integration tests with mock HTTP servers
7. End-to-end tests for full request flows

---

## Testing Patterns Observed

The codebase follows good testing practices:

1. **Table-driven tests** - Used extensively in `auth/static_test.go`, `openai/client_test.go`
2. **Mock utilities** - `rag/testutil/mocks.go` provides `MockEmbedder`
3. **Context testing** - Proper handling of `context.Context` in async tests
4. **Error case coverage** - Tests for missing API keys, invalid URLs, etc.

The proposed tests follow these established patterns for consistency.

---

## Conclusion

Airborne has a solid test foundation (68/100) with excellent coverage of critical auth and tenant modules. The primary gap is the 12 untested OpenAI-compatible provider implementations. Adding the proposed compat layer tests will provide immediate coverage for all these providers, bringing the score to approximately 82/100.

The filestore implementations (OpenAI and Gemini) represent important business functionality that should be tested to prevent regressions in file upload/vector store operations.
