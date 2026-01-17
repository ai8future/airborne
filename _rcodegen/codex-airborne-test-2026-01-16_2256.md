Date Created: 2026-01-16 22:57:00 +0100
Date Updated: 2026-01-17
TOTAL_SCORE: 72/100 → 82/100 (after implementing compat + providers tests)

# Airborne Unit Test Coverage Review

## ✅ IMPLEMENTED (v1.1.6)
- `internal/provider/compat/openai_compat_test.go` - buildMessages, extractText, extractUsage, isRetryableError, validation tests
- `internal/provider/providers_test.go` - Table-driven capability tests for all 13 compat-based providers

---

## Score Rationale
- Solid unit coverage exists for validation, auth, config, server, RAG, and primary providers (OpenAI, Gemini, Anthropic, Mistral).
- Large coverage gaps remain in OpenAI-compat core logic, multiple provider wrappers, file store APIs, and the cmd entrypoint.

## Untested/Weakly Tested Areas (Quick Scan)
- internal/provider/compat/openai_compat.go: message assembly, retry logic, stream error propagation, validation branching.
- internal/provider/*: no tests for compat-based providers (cerebras, cohere, deepinfra, deepseek, fireworks, grok, hyperbolic, nebius, openrouter, perplexity, together, upstage).
- internal/provider/gemini/filestore.go: request composition, upload fallback, status derivation, and list parsing.
- internal/provider/openai/filestore.go: input validation and polling timeout behavior.
- cmd/airborne/main.go: configureLogger behavior not exercised; runHealthCheck has no seams for unit tests.

## Proposed Unit Tests (Details)
### internal/provider/compat/openai_compat_test.go
- buildMessages: verifies ordering, trimming, and user/assistant role mapping.
- extractText / extractUsage: nil handling and trimming correctness.
- isRetryableError: retry classification for auth, invalid request, rate limit, server, and network cases.
- GenerateReply / GenerateReplyStream: missing API key and invalid BaseURL validations.

### internal/provider/providers_test.go
- Table-driven assertions for compat wrappers covering Name(), SupportsFileSearch(), SupportsWebSearch(), SupportsStreaming(), SupportsNativeContinuity().

### internal/provider/gemini/filestore_test.go
- getBaseURL default vs override.
- CreateFileSearchStore success (request payload + ID extraction) and validation errors.
- UploadFileToFileSearchStore fallback from raw upload to metadata request, ensuring file ID extraction.
- waitForOperation empty name and timeout path.
- GetFileSearchStore status mapping (partial/processing/ready).
- ListFileSearchStores ID parsing and pageSize query parameter.

### internal/provider/openai/filestore_test.go
- Input validation for all file-store entrypoints.
- waitForFileProcessing timeout path (fast exit with short context).

### cmd/airborne/main_test.go
- configureLogger: log level gating and handler type selection (JSON vs text).
- runHealthCheck remains best as integration test unless config/dial seams are introduced.

## Patch-ready diffs

```diff
diff --git a/internal/provider/compat/openai_compat_test.go b/internal/provider/compat/openai_compat_test.go
new file mode 100644
index 0000000..b2e3c4d
--- /dev/null
+++ b/internal/provider/compat/openai_compat_test.go
@@
+package compat
+
+import (
+    "context"
+    "errors"
+    "strings"
+    "testing"
+
+    "github.com/openai/openai-go"
+
+    "github.com/ai8future/airborne/internal/provider"
+)
+
+func assertMessage(t *testing.T, msg openai.ChatCompletionMessageParamUnion, role, content string) {
+    t.Helper()
+    gotRole := msg.GetRole()
+    if gotRole == nil || *gotRole != role {
+        if gotRole == nil {
+            t.Fatalf("expected role %q, got nil", role)
+        }
+        t.Fatalf("expected role %q, got %q", role, *gotRole)
+    }
+    raw := msg.GetContent().AsAny()
+    contentPtr, ok := raw.(*string)
+    if !ok || contentPtr == nil {
+        t.Fatalf("expected content string for role %q, got %T", role, raw)
+    }
+    if *contentPtr != content {
+        t.Fatalf("expected content %q, got %q", content, *contentPtr)
+    }
+}
+
+func TestBuildMessages(t *testing.T) {
+    history := []provider.Message{
+        {Role: "user", Content: "  hello  "},
+        {Role: "assistant", Content: " ok "},
+        {Role: "user", Content: "   "},
+    }
+
+    messages := buildMessages("system", "  ask ", history)
+    if len(messages) != 4 {
+        t.Fatalf("expected 4 messages, got %d", len(messages))
+    }
+
+    assertMessage(t, messages[0], "system", "system")
+    assertMessage(t, messages[1], "user", "hello")
+    assertMessage(t, messages[2], "assistant", "ok")
+    assertMessage(t, messages[3], "user", "ask")
+}
+
+func TestExtractText(t *testing.T) {
+    if got := extractText(nil); got != "" {
+        t.Fatalf("extractText(nil) = %q, want empty", got)
+    }
+
+    if got := extractText(&openai.ChatCompletion{}); got != "" {
+        t.Fatalf("extractText(empty) = %q, want empty", got)
+    }
+
+    resp := &openai.ChatCompletion{
+        Choices: []openai.ChatCompletionChoice{
+            {Message: openai.ChatCompletionMessage{Content: "  hi  "}},
+        },
+    }
+    if got := extractText(resp); got != "hi" {
+        t.Fatalf("extractText() = %q, want %q", got, "hi")
+    }
+}
+
+func TestExtractUsage(t *testing.T) {
+    usage := extractUsage(nil)
+    if usage == nil {
+        t.Fatal("expected non-nil usage")
+    }
+    if usage.InputTokens != 0 || usage.OutputTokens != 0 || usage.TotalTokens != 0 {
+        t.Fatalf("expected zero usage, got %+v", usage)
+    }
+
+    resp := &openai.ChatCompletion{
+        Usage: openai.CompletionUsage{
+            PromptTokens:     1,
+            CompletionTokens: 2,
+            TotalTokens:      3,
+        },
+    }
+    usage = extractUsage(resp)
+    if usage.InputTokens != 1 || usage.OutputTokens != 2 || usage.TotalTokens != 3 {
+        t.Fatalf("expected usage 1/2/3, got %+v", usage)
+    }
+}
+
+func TestIsRetryableError(t *testing.T) {
+    tests := []struct {
+        name string
+        err  error
+        want bool
+    }{
+        {"nil", nil, false},
+        {"context canceled", context.Canceled, false},
+        {"deadline exceeded", context.DeadlineExceeded, false},
+        {"auth error", errors.New("401 unauthorized"), false},
+        {"invalid request", errors.New("invalid_request"), false},
+        {"rate limit", errors.New("429 rate limit"), true},
+        {"server error", errors.New("500 internal server error"), true},
+        {"network error", errors.New("connection reset by peer"), true},
+        {"timeout", errors.New("timeout waiting for response"), true},
+    }
+
+    for _, tt := range tests {
+        t.Run(tt.name, func(t *testing.T) {
+            if got := isRetryableError(tt.err); got != tt.want {
+                t.Fatalf("isRetryableError(%v) = %v, want %v", tt.err, got, tt.want)
+            }
+        })
+    }
+}
+
+func TestGenerateReply_MissingAPIKey(t *testing.T) {
+    client := NewClient(ProviderConfig{
+        Name:           "mock",
+        DefaultBaseURL: "https://example.com",
+        DefaultModel:   "mock-model",
+    })
+    _, err := client.GenerateReply(context.Background(), provider.GenerateParams{
+        Config: provider.ProviderConfig{APIKey: ""},
+    })
+    if err == nil {
+        t.Fatal("expected error for missing API key")
+    }
+    if !strings.Contains(err.Error(), "API key is required") {
+        t.Fatalf("unexpected error: %v", err)
+    }
+}
+
+func TestGenerateReply_InvalidBaseURL(t *testing.T) {
+    client := NewClient(ProviderConfig{
+        Name:           "mock",
+        DefaultBaseURL: "https://example.com",
+        DefaultModel:   "mock-model",
+    })
+    _, err := client.GenerateReply(context.Background(), provider.GenerateParams{
+        Config: provider.ProviderConfig{
+            APIKey:  "key",
+            BaseURL: "http://example.com",
+        },
+    })
+    if err == nil {
+        t.Fatal("expected error for invalid base URL")
+    }
+    if !strings.Contains(err.Error(), "invalid base URL") {
+        t.Fatalf("unexpected error: %v", err)
+    }
+}
+
+func TestGenerateReplyStream_MissingAPIKey(t *testing.T) {
+    client := NewClient(ProviderConfig{
+        Name:           "mock",
+        DefaultBaseURL: "https://example.com",
+        DefaultModel:   "mock-model",
+    })
+    ch, err := client.GenerateReplyStream(context.Background(), provider.GenerateParams{
+        Config: provider.ProviderConfig{APIKey: ""},
+    })
+    if err == nil {
+        t.Fatal("expected error for missing API key")
+    }
+    if ch != nil {
+        t.Fatal("expected nil channel on error")
+    }
+}
+
+func TestGenerateReplyStream_InvalidBaseURL(t *testing.T) {
+    client := NewClient(ProviderConfig{
+        Name:           "mock",
+        DefaultBaseURL: "https://example.com",
+        DefaultModel:   "mock-model",
+    })
+    ch, err := client.GenerateReplyStream(context.Background(), provider.GenerateParams{
+        Config: provider.ProviderConfig{
+            APIKey:  "key",
+            BaseURL: "http://example.com",
+        },
+    })
+    if err == nil {
+        t.Fatal("expected error for invalid base URL")
+    }
+    if ch != nil {
+        t.Fatal("expected nil channel on error")
+    }
+}
```

```diff
diff --git a/internal/provider/providers_test.go b/internal/provider/providers_test.go
new file mode 100644
index 0000000..f00ba42
--- /dev/null
+++ b/internal/provider/providers_test.go
@@
+package provider_test
+
+import (
+    "testing"
+
+    "github.com/ai8future/airborne/internal/provider"
+    "github.com/ai8future/airborne/internal/provider/cerebras"
+    "github.com/ai8future/airborne/internal/provider/cohere"
+    "github.com/ai8future/airborne/internal/provider/deepinfra"
+    "github.com/ai8future/airborne/internal/provider/deepseek"
+    "github.com/ai8future/airborne/internal/provider/fireworks"
+    "github.com/ai8future/airborne/internal/provider/grok"
+    "github.com/ai8future/airborne/internal/provider/hyperbolic"
+    "github.com/ai8future/airborne/internal/provider/nebius"
+    "github.com/ai8future/airborne/internal/provider/openrouter"
+    "github.com/ai8future/airborne/internal/provider/perplexity"
+    "github.com/ai8future/airborne/internal/provider/together"
+    "github.com/ai8future/airborne/internal/provider/upstage"
+)
+
+type providerCase struct {
+    name                   string
+    constructor            func() provider.Provider
+    supportsFileSearch     bool
+    supportsWebSearch      bool
+    supportsStreaming      bool
+    supportsNativeContinue bool
+}
+
+func TestCompatProviderConfigs(t *testing.T) {
+    tests := []providerCase{
+        {"cerebras", func() provider.Provider { return cerebras.NewClient() }, false, false, true, false},
+        {"cohere", func() provider.Provider { return cohere.NewClient() }, false, true, true, false},
+        {"deepinfra", func() provider.Provider { return deepinfra.NewClient() }, false, false, true, false},
+        {"deepseek", func() provider.Provider { return deepseek.NewClient() }, false, false, true, false},
+        {"fireworks", func() provider.Provider { return fireworks.NewClient() }, false, false, true, false},
+        {"grok", func() provider.Provider { return grok.NewClient() }, false, false, true, false},
+        {"hyperbolic", func() provider.Provider { return hyperbolic.NewClient() }, false, false, true, false},
+        {"nebius", func() provider.Provider { return nebius.NewClient() }, false, false, true, false},
+        {"openrouter", func() provider.Provider { return openrouter.NewClient() }, false, false, true, false},
+        {"perplexity", func() provider.Provider { return perplexity.NewClient() }, false, true, true, false},
+        {"together", func() provider.Provider { return together.NewClient() }, false, false, true, false},
+        {"upstage", func() provider.Provider { return upstage.NewClient() }, false, false, true, false},
+    }
+
+    for _, tt := range tests {
+        t.Run(tt.name, func(t *testing.T) {
+            client := tt.constructor()
+            if client.Name() != tt.name {
+                t.Fatalf("Name() = %q, want %q", client.Name(), tt.name)
+            }
+            if client.SupportsFileSearch() != tt.supportsFileSearch {
+                t.Fatalf("SupportsFileSearch() = %v, want %v", client.SupportsFileSearch(), tt.supportsFileSearch)
+            }
+            if client.SupportsWebSearch() != tt.supportsWebSearch {
+                t.Fatalf("SupportsWebSearch() = %v, want %v", client.SupportsWebSearch(), tt.supportsWebSearch)
+            }
+            if client.SupportsStreaming() != tt.supportsStreaming {
+                t.Fatalf("SupportsStreaming() = %v, want %v", client.SupportsStreaming(), tt.supportsStreaming)
+            }
+            if client.SupportsNativeContinuity() != tt.supportsNativeContinue {
+                t.Fatalf("SupportsNativeContinuity() = %v, want %v", client.SupportsNativeContinuity(), tt.supportsNativeContinue)
+            }
+        })
+    }
+}
```

```diff
diff --git a/internal/provider/gemini/filestore_test.go b/internal/provider/gemini/filestore_test.go
new file mode 100644
index 0000000..9c11a02
--- /dev/null
+++ b/internal/provider/gemini/filestore_test.go
@@
+package gemini
+
+import (
+    "context"
+    "encoding/json"
+    "io"
+    "net/http"
+    "net/http/httptest"
+    "strings"
+    "sync/atomic"
+    "testing"
+    "time"
+)
+
+func TestFileStoreConfigGetBaseURL(t *testing.T) {
+    if got := (FileStoreConfig{}).getBaseURL(); got != fileSearchBaseURL {
+        t.Fatalf("getBaseURL() = %q, want %q", got, fileSearchBaseURL)
+    }
+
+    cfg := FileStoreConfig{BaseURL: "http://127.0.0.1:1234/v1beta"}
+    if got := cfg.getBaseURL(); got != cfg.BaseURL {
+        t.Fatalf("getBaseURL() = %q, want %q", got, cfg.BaseURL)
+    }
+}
+
+func TestCreateFileSearchStore_MissingAPIKey(t *testing.T) {
+    _, err := CreateFileSearchStore(context.Background(), FileStoreConfig{}, "store")
+    if err == nil || !strings.Contains(err.Error(), "API key is required") {
+        t.Fatalf("expected API key error, got %v", err)
+    }
+}
+
+func TestCreateFileSearchStore_InvalidBaseURL(t *testing.T) {
+    _, err := CreateFileSearchStore(context.Background(), FileStoreConfig{
+        APIKey:  "key",
+        BaseURL: "http://example.com",
+    }, "store")
+    if err == nil || !strings.Contains(err.Error(), "invalid base URL") {
+        t.Fatalf("expected invalid base URL error, got %v", err)
+    }
+}
+
+func TestCreateFileSearchStore_Success(t *testing.T) {
+    server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
+        if r.Method != http.MethodPost {
+            t.Fatalf("expected POST, got %s", r.Method)
+        }
+        if r.URL.Path != "/fileSearchStores" {
+            t.Fatalf("unexpected path: %s", r.URL.Path)
+        }
+        if got := r.URL.Query().Get("key"); got != "test-key" {
+            t.Fatalf("unexpected key query: %q", got)
+        }
+
+        var payload map[string]string
+        if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
+            t.Fatalf("decode request: %v", err)
+        }
+        if payload["displayName"] != "My Store" {
+            t.Fatalf("unexpected displayName: %q", payload["displayName"])
+        }
+
+        w.Header().Set("Content-Type", "application/json")
+        io.WriteString(w, `{"name":"fileSearchStores/store-1","displayName":"My Store","createTime":"2024-01-01T00:00:00Z","updateTime":"2024-01-01T00:00:00Z","totalDocumentCount":1,"processedDocumentCount":1,"failedDocumentCount":0,"sizeBytes":"0"}`)
+    }))
+    defer server.Close()
+
+    cfg := FileStoreConfig{APIKey: "test-key", BaseURL: server.URL}
+    got, err := CreateFileSearchStore(context.Background(), cfg, "My Store")
+    if err != nil {
+        t.Fatalf("unexpected error: %v", err)
+    }
+    if got.StoreID != "store-1" {
+        t.Fatalf("StoreID = %q, want %q", got.StoreID, "store-1")
+    }
+    if got.Name != "My Store" {
+        t.Fatalf("Name = %q, want %q", got.Name, "My Store")
+    }
+    if got.Status != "ready" {
+        t.Fatalf("Status = %q, want %q", got.Status, "ready")
+    }
+    if got.DocumentCount != 1 {
+        t.Fatalf("DocumentCount = %d, want %d", got.DocumentCount, 1)
+    }
+}
+
+func TestUploadFileToFileSearchStore_FallbackToMetadata(t *testing.T) {
+    var calls int32
+    server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
+        call := atomic.AddInt32(&calls, 1)
+        switch call {
+        case 1:
+            if r.Method != http.MethodPost {
+                t.Fatalf("expected POST, got %s", r.Method)
+            }
+            if r.URL.Path != "/upload/v1beta/fileSearchStores/store-1:uploadToFileSearchStore" {
+                t.Fatalf("unexpected upload path: %s", r.URL.Path)
+            }
+            if got := r.Header.Get("X-Goog-Upload-Protocol"); got != "raw" {
+                t.Fatalf("unexpected upload protocol: %q", got)
+            }
+            w.WriteHeader(http.StatusBadRequest)
+            io.WriteString(w, "bad request")
+        case 2:
+            if r.URL.Path != "/v1beta/fileSearchStores/store-1:uploadToFileSearchStore" {
+                t.Fatalf("unexpected metadata path: %s", r.URL.Path)
+            }
+            if got := r.Header.Get("Content-Type"); got != "application/json" {
+                t.Fatalf("unexpected content type: %q", got)
+            }
+            w.Header().Set("Content-Type", "application/json")
+            io.WriteString(w, `{"name":"","done":false,"response":{"name":"fileSearchStores/store-1/files/file-1"}}`)
+        default:
+            t.Fatalf("unexpected call %d", call)
+        }
+    }))
+    defer server.Close()
+
+    cfg := FileStoreConfig{APIKey: "key", BaseURL: server.URL + "/v1beta"}
+    got, err := UploadFileToFileSearchStore(context.Background(), cfg, "store-1", "test.txt", "text/plain", strings.NewReader("hello"))
+    if err != nil {
+        t.Fatalf("unexpected error: %v", err)
+    }
+    if got.FileID != "file-1" {
+        t.Fatalf("FileID = %q, want %q", got.FileID, "file-1")
+    }
+    if got.Status != "unknown" {
+        t.Fatalf("Status = %q, want %q", got.Status, "unknown")
+    }
+    if atomic.LoadInt32(&calls) != 2 {
+        t.Fatalf("expected 2 calls, got %d", calls)
+    }
+}
+
+func TestWaitForOperation_EmptyName(t *testing.T) {
+    status, err := waitForOperation(context.Background(), FileStoreConfig{APIKey: "key"}, "")
+    if err != nil {
+        t.Fatalf("unexpected error: %v", err)
+    }
+    if status != "unknown" {
+        t.Fatalf("status = %q, want %q", status, "unknown")
+    }
+}
+
+func TestWaitForOperation_Timeout(t *testing.T) {
+    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
+    defer cancel()
+
+    status, err := waitForOperation(ctx, FileStoreConfig{APIKey: "key", BaseURL: "http://127.0.0.1"}, "operations/1")
+    if err == nil {
+        t.Fatal("expected timeout error")
+    }
+    if status != "in_progress" {
+        t.Fatalf("status = %q, want %q", status, "in_progress")
+    }
+}
+
+func TestGetFileSearchStore_Status(t *testing.T) {
+    server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
+        if r.URL.Path != "/fileSearchStores/store-1" {
+            t.Fatalf("unexpected path: %s", r.URL.Path)
+        }
+        w.Header().Set("Content-Type", "application/json")
+        io.WriteString(w, `{"name":"fileSearchStores/store-1","displayName":"Store","createTime":"2024-01-01T00:00:00Z","updateTime":"2024-01-01T00:00:00Z","totalDocumentCount":10,"processedDocumentCount":5,"failedDocumentCount":1,"sizeBytes":"0"}`)
+    }))
+    defer server.Close()
+
+    cfg := FileStoreConfig{APIKey: "key", BaseURL: server.URL}
+    got, err := GetFileSearchStore(context.Background(), cfg, "store-1")
+    if err != nil {
+        t.Fatalf("unexpected error: %v", err)
+    }
+    if got.Status != "partial" {
+        t.Fatalf("Status = %q, want %q", got.Status, "partial")
+    }
+}
+
+func TestListFileSearchStores_ParsesIDs(t *testing.T) {
+    server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
+        if got := r.URL.Query().Get("pageSize"); got != "2" {
+            t.Fatalf("unexpected pageSize: %q", got)
+        }
+        w.Header().Set("Content-Type", "application/json")
+        io.WriteString(w, `{"fileSearchStores":[{"name":"fileSearchStores/store-1","displayName":"One","createTime":"2024-01-01T00:00:00Z","updateTime":"2024-01-01T00:00:00Z","totalDocumentCount":1,"processedDocumentCount":1,"failedDocumentCount":0,"sizeBytes":"0"},{"name":"fileSearchStores/store-2","displayName":"Two","createTime":"2024-01-02T00:00:00Z","updateTime":"2024-01-02T00:00:00Z","totalDocumentCount":2,"processedDocumentCount":2,"failedDocumentCount":0,"sizeBytes":"0"}]}`)
+    }))
+    defer server.Close()
+
+    cfg := FileStoreConfig{APIKey: "key", BaseURL: server.URL}
+    got, err := ListFileSearchStores(context.Background(), cfg, 2)
+    if err != nil {
+        t.Fatalf("unexpected error: %v", err)
+    }
+    if len(got) != 2 {
+        t.Fatalf("expected 2 stores, got %d", len(got))
+    }
+    if got[0].StoreID != "store-1" || got[1].StoreID != "store-2" {
+        t.Fatalf("unexpected store IDs: %q, %q", got[0].StoreID, got[1].StoreID)
+    }
+}
```

```diff
diff --git a/internal/provider/openai/filestore_test.go b/internal/provider/openai/filestore_test.go
new file mode 100644
index 0000000..5f6b9b1
--- /dev/null
+++ b/internal/provider/openai/filestore_test.go
@@
+package openai
+
+import (
+    "context"
+    "strings"
+    "testing"
+    "time"
+
+    openaigo "github.com/openai/openai-go"
+)
+
+func TestCreateVectorStore_MissingAPIKey(t *testing.T) {
+    _, err := CreateVectorStore(context.Background(), FileStoreConfig{}, "store")
+    if err == nil || !strings.Contains(err.Error(), "API key is required") {
+        t.Fatalf("expected API key error, got %v", err)
+    }
+}
+
+func TestCreateVectorStore_InvalidBaseURL(t *testing.T) {
+    _, err := CreateVectorStore(context.Background(), FileStoreConfig{
+        APIKey:  "key",
+        BaseURL: "http://example.com",
+    }, "store")
+    if err == nil || !strings.Contains(err.Error(), "invalid base URL") {
+        t.Fatalf("expected invalid base URL error, got %v", err)
+    }
+}
+
+func TestUploadFileToVectorStore_MissingAPIKey(t *testing.T) {
+    _, err := UploadFileToVectorStore(context.Background(), FileStoreConfig{}, "store", "file.txt", strings.NewReader("data"))
+    if err == nil || !strings.Contains(err.Error(), "API key is required") {
+        t.Fatalf("expected API key error, got %v", err)
+    }
+}
+
+func TestUploadFileToVectorStore_MissingStoreID(t *testing.T) {
+    _, err := UploadFileToVectorStore(context.Background(), FileStoreConfig{APIKey: "key"}, "", "file.txt", strings.NewReader("data"))
+    if err == nil || !strings.Contains(err.Error(), "store ID is required") {
+        t.Fatalf("expected store ID error, got %v", err)
+    }
+}
+
+func TestDeleteVectorStore_MissingAPIKey(t *testing.T) {
+    err := DeleteVectorStore(context.Background(), FileStoreConfig{}, "store")
+    if err == nil || !strings.Contains(err.Error(), "API key is required") {
+        t.Fatalf("expected API key error, got %v", err)
+    }
+}
+
+func TestDeleteVectorStore_MissingStoreID(t *testing.T) {
+    err := DeleteVectorStore(context.Background(), FileStoreConfig{APIKey: "key"}, "")
+    if err == nil || !strings.Contains(err.Error(), "store ID is required") {
+        t.Fatalf("expected store ID error, got %v", err)
+    }
+}
+
+func TestGetVectorStore_MissingAPIKey(t *testing.T) {
+    _, err := GetVectorStore(context.Background(), FileStoreConfig{}, "store")
+    if err == nil || !strings.Contains(err.Error(), "API key is required") {
+        t.Fatalf("expected API key error, got %v", err)
+    }
+}
+
+func TestGetVectorStore_MissingStoreID(t *testing.T) {
+    _, err := GetVectorStore(context.Background(), FileStoreConfig{APIKey: "key"}, "")
+    if err == nil || !strings.Contains(err.Error(), "store ID is required") {
+        t.Fatalf("expected store ID error, got %v", err)
+    }
+}
+
+func TestListVectorStores_MissingAPIKey(t *testing.T) {
+    _, err := ListVectorStores(context.Background(), FileStoreConfig{}, 10)
+    if err == nil || !strings.Contains(err.Error(), "API key is required") {
+        t.Fatalf("expected API key error, got %v", err)
+    }
+}
+
+func TestListVectorStores_InvalidBaseURL(t *testing.T) {
+    _, err := ListVectorStores(context.Background(), FileStoreConfig{
+        APIKey:  "key",
+        BaseURL: "http://example.com",
+    }, 10)
+    if err == nil || !strings.Contains(err.Error(), "invalid base URL") {
+        t.Fatalf("expected invalid base URL error, got %v", err)
+    }
+}
+
+func TestWaitForFileProcessing_Timeout(t *testing.T) {
+    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
+    defer cancel()
+
+    status, err := waitForFileProcessing(ctx, openaigo.Client{}, "store", "file")
+    if err == nil {
+        t.Fatal("expected timeout error")
+    }
+    if status != "in_progress" {
+        t.Fatalf("status = %q, want %q", status, "in_progress")
+    }
+}
```

```diff
diff --git a/cmd/airborne/main_test.go b/cmd/airborne/main_test.go
new file mode 100644
index 0000000..3aabb40
--- /dev/null
+++ b/cmd/airborne/main_test.go
@@
+package main
+
+import (
+    "context"
+    "log/slog"
+    "testing"
+
+    "github.com/ai8future/airborne/internal/config"
+)
+
+func TestConfigureLogger_Level(t *testing.T) {
+    prev := slog.Default()
+    t.Cleanup(func() { slog.SetDefault(prev) })
+
+    configureLogger(config.LoggingConfig{Level: "debug", Format: "json"})
+    if !slog.Default().Enabled(context.Background(), slog.LevelDebug) {
+        t.Fatal("expected debug to be enabled")
+    }
+
+    configureLogger(config.LoggingConfig{Level: "error", Format: "json"})
+    if slog.Default().Enabled(context.Background(), slog.LevelInfo) {
+        t.Fatal("expected info to be disabled at error level")
+    }
+    if !slog.Default().Enabled(context.Background(), slog.LevelError) {
+        t.Fatal("expected error to be enabled")
+    }
+}
+
+func TestConfigureLogger_Format(t *testing.T) {
+    prev := slog.Default()
+    t.Cleanup(func() { slog.SetDefault(prev) })
+
+    configureLogger(config.LoggingConfig{Level: "info", Format: "text"})
+    if _, ok := slog.Default().Handler().(*slog.TextHandler); !ok {
+        t.Fatal("expected text handler")
+    }
+
+    configureLogger(config.LoggingConfig{Level: "info", Format: "json"})
+    if _, ok := slog.Default().Handler().(*slog.JSONHandler); !ok {
+        t.Fatal("expected JSON handler")
+    }
+}
```

## Notes / Risks
- runHealthCheck in cmd/airborne/main.go is still effectively integration-only due to config loading and gRPC dialing; adding seams (config loader, dialer) would enable unit tests.
- OpenAI file store tests above are limited to validation and timeout paths; deeper API response coverage would need JSON fixtures for openai-go required fields.
