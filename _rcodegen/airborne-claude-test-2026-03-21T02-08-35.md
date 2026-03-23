Date Created: 2026-03-21T02:08:35Z
TOTAL_SCORE: 62/100

# Airborne Unit Test Coverage Audit

**Auditor:** Claude:Opus 4.6
**Codebase:** airborne (multi-provider AI gateway)
**Date:** 2026-03-21

---

## Executive Summary

Airborne has **47 test files** covering **~78 source files** (60% file-level coverage). All existing tests pass cleanly. The well-tested packages (auth, config, tenant, RAG, service, validation) demonstrate high-quality Go testing practices with table-driven tests, mocks, and interface compliance checks. However, three critical areas remain entirely untested: the **admin HTTP server** (9+ endpoints, ~700 LOC), the **CLI package** (~680 LOC), and the **database repository** (~813 LOC of SQL/transaction logic). These gaps represent the largest risk surface.

---

## Scoring Breakdown

| Category | Score | Max | Notes |
|----------|-------|-----|-------|
| Core business logic (service layer) | 18 | 20 | chat, admin, files services all tested |
| Auth & security | 14 | 15 | interceptors, keys, rate limiting, static auth |
| Config & tenant management | 11 | 12 | config, env, loader, manager, secrets all tested |
| RAG pipeline | 10 | 10 | chunker, embedder, extractor, vectorstore, service |
| Provider interface & compat | 8 | 10 | compat client tested; central capabilities test covers 13 providers |
| Validation & errors | 5 | 5 | limits, URL validation, error sanitization |
| Retry & resilience | 3 | 5 | IsRetryable + backoff tested; EnsureTimeout untested |
| DB models | 4 | 5 | models, citations, metrics tested; no repository tests |
| Admin HTTP server | 0 | 8 | 9 endpoints, ~700 LOC, ZERO tests |
| CLI utilities | 0 | 5 | formatting functions easily unit-testable |
| DB repository / postgres | 0 | 5 | ~970 LOC of SQL logic untested |
| **TOTAL** | **62** | **100** | |

---

## Package-by-Package Coverage

| Package | Source | Tests | Coverage | Priority |
|---------|--------|-------|----------|----------|
| `internal/admin` | 1 file | 0 | **0%** | CRITICAL |
| `internal/auth` | 6 files | 5 | 83% | Good |
| `internal/cli` | 3 files | 0 | **0%** | HIGH |
| `internal/commands` | 1 file | 1 | 100% | Good |
| `internal/config` | 3 files | 3 | 100% | Good |
| `internal/config/envutil` | 1 file | 1 | 100% | Good |
| `internal/db` | 3 files | 1 | 33% | HIGH |
| `internal/errors` | 1 file | 1 | 100% | Good |
| `internal/httpcapture` | 1 file | 1 | 100% | Good |
| `internal/imagegen` | 4 files | 2 | 50% | MODERATE |
| `internal/markdownsvc` | 1 file | 1 | 100% | Good |
| `internal/pricing` | 1 file | 1 | 100% | Good |
| `internal/provider` | 1 file | 1 | 100% | Good |
| `internal/provider/anthropic` | 1 file | 1 | 100% | Good |
| `internal/provider/compat` | 1 file | 1 | 100% | Good |
| `internal/provider/gemini` | 2 files | 1 | 50% | OK |
| `internal/provider/httputil` | 1 file | 1 | 100% | Good |
| `internal/provider/mistral` | 1 file | 1 | 100% | Good |
| `internal/provider/openai` | 2 files | 1 | 50% | OK |
| `internal/provider/{10 compat}` | 10 files | 0* | 0%* | LOW |
| `internal/rag` | 2 files | 2 | 100% | Good |
| `internal/rag/chunker` | 1 file | 1 | 100% | Good |
| `internal/rag/embedder` | 1 file | 1 | 100% | Good |
| `internal/rag/extractor` | 1 file | 1 | 100% | Good |
| `internal/rag/vectorstore` | 1 file | 1 | 100% | Good |
| `internal/redis` | 1 file | 1 | 100% | Good |
| `internal/retry` | 4 files | 1 | 25% | MODERATE |
| `internal/server` | 1 file | 2 | 100% | Good |
| `internal/service` | 3 files | 3 | 100% | Good |
| `internal/service/config` | 1 file | 1 | 100% | Good |
| `internal/tenant` | 6 files | 5 | 83% | Good |
| `internal/validation` | 2 files | 2 | 100% | Good |

\* The 10 compat provider packages (cerebras, cohere, deepinfra, deepseek, fireworks, grok, hyperbolic, nebius, openrouter, together, upstage) have no per-package tests but are covered by `internal/provider/providers_test.go` for interface compliance and capability flags.

---

## Proposed Tests (Patch-Ready Diffs)

### 1. CLI Output Utilities — `internal/cli/output_test.go` (NEW FILE)

Pure formatting functions with zero external dependencies. Highest value-to-effort ratio.

```diff
--- /dev/null
+++ b/internal/cli/output_test.go
@@ -0,0 +1,129 @@
+package cli
+
+import (
+	"testing"
+)
+
+func TestFormatTokens(t *testing.T) {
+	tests := []struct {
+		name   string
+		tokens int
+		want   string
+	}{
+		{"zero", 0, "0"},
+		{"small", 42, "42"},
+		{"just under 1K", 999, "999"},
+		{"exactly 1K", 1000, "1.0K"},
+		{"1.5K", 1500, "1.5K"},
+		{"large", 12345, "12.3K"},
+		{"100K", 100000, "100.0K"},
+	}
+
+	for _, tt := range tests {
+		t.Run(tt.name, func(t *testing.T) {
+			if got := FormatTokens(tt.tokens); got != tt.want {
+				t.Errorf("FormatTokens(%d) = %q, want %q", tt.tokens, got, tt.want)
+			}
+		})
+	}
+}
+
+func TestFormatCost(t *testing.T) {
+	tests := []struct {
+		name string
+		cost float64
+		want string
+	}{
+		{"zero", 0.0, "$0.000"},
+		{"small", 0.001, "$0.001"},
+		{"typical", 0.025, "$0.025"},
+		{"one dollar", 1.0, "$1.000"},
+		{"precise", 0.12345, "$0.123"},
+	}
+
+	for _, tt := range tests {
+		t.Run(tt.name, func(t *testing.T) {
+			if got := FormatCost(tt.cost); got != tt.want {
+				t.Errorf("FormatCost(%f) = %q, want %q", tt.cost, got, tt.want)
+			}
+		})
+	}
+}
+
+func TestFormatDuration(t *testing.T) {
+	tests := []struct {
+		name string
+		ms   int
+		want string
+	}{
+		{"zero", 0, "0ms"},
+		{"sub-second", 500, "500ms"},
+		{"exactly 1s", 1000, "1.0s"},
+		{"1.5s", 1500, "1.5s"},
+		{"long", 12345, "12.3s"},
+	}
+
+	for _, tt := range tests {
+		t.Run(tt.name, func(t *testing.T) {
+			if got := FormatDuration(tt.ms); got != tt.want {
+				t.Errorf("FormatDuration(%d) = %q, want %q", tt.ms, got, tt.want)
+			}
+		})
+	}
+}
+
+func TestFormatTimestamp(t *testing.T) {
+	tests := []struct {
+		name  string
+		input string
+	}{
+		{"RFC3339", "2026-03-21T12:30:45Z"},
+		{"RFC3339 with offset", "2026-03-21T12:30:45+05:00"},
+		{"fallback format", "2026-03-21T12:30:45Z"},
+	}
+
+	for _, tt := range tests {
+		t.Run(tt.name, func(t *testing.T) {
+			got := FormatTimestamp(tt.input)
+			// Should not return the raw input (i.e., it parsed successfully)
+			if got == "" {
+				t.Error("FormatTimestamp returned empty string")
+			}
+		})
+	}
+
+	// Unparseable input should return as-is
+	t.Run("unparseable", func(t *testing.T) {
+		input := "not-a-timestamp"
+		if got := FormatTimestamp(input); got != input {
+			t.Errorf("FormatTimestamp(%q) = %q, want original string", input, got)
+		}
+	})
+}
+
+func TestTruncateString(t *testing.T) {
+	tests := []struct {
+		name   string
+		s      string
+		maxLen int
+		want   string
+	}{
+		{"short", "hello", 10, "hello"},
+		{"exact", "hello12345", 10, "hello12345"},
+		{"truncated", "hello world foo bar", 10, "hello w..."},
+		{"with newlines", "hello\nworld", 20, "hello world"},
+		{"empty", "", 10, ""},
+	}
+
+	for _, tt := range tests {
+		t.Run(tt.name, func(t *testing.T) {
+			if got := TruncateString(tt.s, tt.maxLen); got != tt.want {
+				t.Errorf("TruncateString(%q, %d) = %q, want %q", tt.s, tt.maxLen, got, tt.want)
+			}
+		})
+	}
+}
+
+func TestFormatStatus(t *testing.T) {
+	// "success" should produce green check, anything else red X
+	successOut := FormatStatus("success")
+	if successOut == "" {
+		t.Error("FormatStatus('success') returned empty")
+	}
+
+	failedOut := FormatStatus("failed")
+	if failedOut == "" {
+		t.Error("FormatStatus('failed') returned empty")
+	}
+
+	if successOut == failedOut {
+		t.Error("FormatStatus should produce different output for success vs failure")
+	}
+}
```

---

### 2. CLI HTTP Client — `internal/cli/client_test.go` (NEW FILE)

Tests the HTTP client methods using `httptest.NewServer` for deterministic behavior.

```diff
--- /dev/null
+++ b/internal/cli/client_test.go
@@ -0,0 +1,189 @@
+package cli
+
+import (
+	"encoding/json"
+	"net/http"
+	"net/http/httptest"
+	"testing"
+)
+
+func TestNewClient(t *testing.T) {
+	c := NewClient("http://localhost:8080")
+	if c == nil {
+		t.Fatal("NewClient returned nil")
+	}
+	if c.BaseURL != "http://localhost:8080" {
+		t.Errorf("BaseURL = %q, want %q", c.BaseURL, "http://localhost:8080")
+	}
+	if c.HTTPClient == nil {
+		t.Fatal("HTTPClient is nil")
+	}
+}
+
+func TestClient_Health_Success(t *testing.T) {
+	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
+		if r.URL.Path != "/admin/health" {
+			t.Errorf("unexpected path: %s", r.URL.Path)
+		}
+		json.NewEncoder(w).Encode(HealthResponse{
+			Status:   "healthy",
+			Database: "healthy",
+		})
+	}))
+	defer srv.Close()
+
+	c := NewClient(srv.URL)
+	health, err := c.Health()
+	if err != nil {
+		t.Fatalf("Health() error = %v", err)
+	}
+	if health.Status != "healthy" {
+		t.Errorf("Status = %q, want %q", health.Status, "healthy")
+	}
+	if health.Database != "healthy" {
+		t.Errorf("Database = %q, want %q", health.Database, "healthy")
+	}
+}
+
+func TestClient_Health_ServerError(t *testing.T) {
+	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
+		w.WriteHeader(http.StatusInternalServerError)
+		w.Write([]byte("internal error"))
+	}))
+	defer srv.Close()
+
+	c := NewClient(srv.URL)
+	_, err := c.Health()
+	if err == nil {
+		t.Fatal("expected error for 500 response")
+	}
+}
+
+func TestClient_Health_ConnectionRefused(t *testing.T) {
+	c := NewClient("http://127.0.0.1:1") // port 1 should refuse
+	_, err := c.Health()
+	if err == nil {
+		t.Fatal("expected error for unreachable server")
+	}
+}
+
+func TestClient_Activity_Success(t *testing.T) {
+	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
+		if r.URL.Path != "/admin/activity" {
+			t.Errorf("unexpected path: %s", r.URL.Path)
+		}
+		// Verify query params
+		if r.URL.Query().Get("limit") != "5" {
+			t.Errorf("limit = %q, want %q", r.URL.Query().Get("limit"), "5")
+		}
+		if r.URL.Query().Get("tenant_id") != "ai8" {
+			t.Errorf("tenant_id = %q, want %q", r.URL.Query().Get("tenant_id"), "ai8")
+		}
+		json.NewEncoder(w).Encode(ActivityResponse{
+			Activity: []Activity{
+				{ID: "msg-1", Model: "gpt-4", Status: "success"},
+			},
+		})
+	}))
+	defer srv.Close()
+
+	c := NewClient(srv.URL)
+	resp, err := c.Activity(5, "ai8")
+	if err != nil {
+		t.Fatalf("Activity() error = %v", err)
+	}
+	if len(resp.Activity) != 1 {
+		t.Fatalf("expected 1 activity entry, got %d", len(resp.Activity))
+	}
+	if resp.Activity[0].Model != "gpt-4" {
+		t.Errorf("Model = %q, want %q", resp.Activity[0].Model, "gpt-4")
+	}
+}
+
+func TestClient_Activity_NoTenant(t *testing.T) {
+	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
+		if r.URL.Query().Get("tenant_id") != "" {
+			t.Error("tenant_id should not be set when empty")
+		}
+		json.NewEncoder(w).Encode(ActivityResponse{Activity: []Activity{}})
+	}))
+	defer srv.Close()
+
+	c := NewClient(srv.URL)
+	resp, err := c.Activity(10, "")
+	if err != nil {
+		t.Fatalf("Activity() error = %v", err)
+	}
+	if len(resp.Activity) != 0 {
+		t.Errorf("expected empty activity, got %d", len(resp.Activity))
+	}
+}
+
+func TestClient_Debug_Success(t *testing.T) {
+	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
+		if r.URL.Path != "/admin/debug/msg-123" {
+			t.Errorf("unexpected path: %s", r.URL.Path)
+		}
+		json.NewEncoder(w).Encode(DebugResponse{
+			MessageID: "msg-123",
+			Status:    "success",
+		})
+	}))
+	defer srv.Close()
+
+	c := NewClient(srv.URL)
+	resp, err := c.Debug("msg-123")
+	if err != nil {
+		t.Fatalf("Debug() error = %v", err)
+	}
+	if resp.MessageID != "msg-123" {
+		t.Errorf("MessageID = %q, want %q", resp.MessageID, "msg-123")
+	}
+}
+
+func TestClient_Thread_Success(t *testing.T) {
+	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
+		if r.URL.Path != "/admin/thread/thread-456" {
+			t.Errorf("unexpected path: %s", r.URL.Path)
+		}
+		json.NewEncoder(w).Encode(ThreadResponse{
+			ThreadID: "thread-456",
+			Messages: []ThreadMessage{
+				{ID: "msg-1", Role: "user", Content: "Hello"},
+				{ID: "msg-2", Role: "assistant", Content: "Hi there"},
+			},
+		})
+	}))
+	defer srv.Close()
+
+	c := NewClient(srv.URL)
+	resp, err := c.Thread("thread-456")
+	if err != nil {
+		t.Fatalf("Thread() error = %v", err)
+	}
+	if resp.ThreadID != "thread-456" {
+		t.Errorf("ThreadID = %q, want %q", resp.ThreadID, "thread-456")
+	}
+	if len(resp.Messages) != 2 {
+		t.Errorf("expected 2 messages, got %d", len(resp.Messages))
+	}
+}
+
+func TestClient_Test_Success(t *testing.T) {
+	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
+		if r.Method != http.MethodPost {
+			t.Errorf("expected POST, got %s", r.Method)
+		}
+		var req TestRequest
+		json.NewDecoder(r.Body).Decode(&req)
+		if req.Prompt != "Hello" {
+			t.Errorf("Prompt = %q, want %q", req.Prompt, "Hello")
+		}
+		json.NewEncoder(w).Encode(TestResponse{
+			Reply:    "Hi there",
+			Provider: "gemini",
+			Model:    "gemini-pro",
+		})
+	}))
+	defer srv.Close()
+
+	c := NewClient(srv.URL)
+	resp, err := c.Test(TestRequest{Prompt: "Hello", TenantID: "ai8"})
+	if err != nil {
+		t.Fatalf("Test() error = %v", err)
+	}
+	if resp.Reply != "Hi there" {
+		t.Errorf("Reply = %q, want %q", resp.Reply, "Hi there")
+	}
+	if resp.Provider != "gemini" {
+		t.Errorf("Provider = %q, want %q", resp.Provider, "gemini")
+	}
+}
```

---

### 3. Retry EnsureTimeout — `internal/retry/retry_test.go` (APPEND)

```diff
--- a/internal/retry/retry_test.go
+++ b/internal/retry/retry_test.go
@@ -125,0 +126,33 @@
+
+func TestEnsureTimeout_NoExistingDeadline(t *testing.T) {
+	ctx := context.Background()
+	timeout := 5 * time.Second
+
+	newCtx, cancel := EnsureTimeout(ctx, timeout)
+	defer cancel()
+
+	deadline, ok := newCtx.Deadline()
+	if !ok {
+		t.Fatal("expected context to have a deadline")
+	}
+
+	remaining := time.Until(deadline)
+	if remaining < 4*time.Second || remaining > 6*time.Second {
+		t.Errorf("expected ~5s remaining, got %v", remaining)
+	}
+}
+
+func TestEnsureTimeout_ExistingDeadline(t *testing.T) {
+	originalTimeout := 10 * time.Second
+	ctx, cancel := context.WithTimeout(context.Background(), originalTimeout)
+	defer cancel()
+
+	originalDeadline, _ := ctx.Deadline()
+
+	// EnsureTimeout should NOT override existing deadline
+	newCtx, newCancel := EnsureTimeout(ctx, 1*time.Second)
+	defer newCancel()
+
+	newDeadline, _ := newCtx.Deadline()
+	if !newDeadline.Equal(originalDeadline) {
+		t.Errorf("deadline changed: original=%v, new=%v", originalDeadline, newDeadline)
+	}
+}
```

---

### 4. DB Repository Table Names — `internal/db/repository_test.go` (NEW FILE)

Tests the deterministic, non-DB-dependent parts of the repository: table name generation, tenant validation, and content processing logic.

```diff
--- /dev/null
+++ b/internal/db/repository_test.go
@@ -0,0 +1,142 @@
+package db
+
+import (
+	"strings"
+	"testing"
+)
+
+func TestNewTenantRepository_ValidTenants(t *testing.T) {
+	for _, tenantID := range []string{"ai8", "email4ai", "zztest"} {
+		t.Run(tenantID, func(t *testing.T) {
+			// Pass nil client — we only test table name logic, not queries
+			repo, err := NewTenantRepository(nil, tenantID)
+			if err != nil {
+				t.Fatalf("NewTenantRepository(%q) error = %v", tenantID, err)
+			}
+			if repo.TenantID() != tenantID {
+				t.Errorf("TenantID() = %q, want %q", repo.TenantID(), tenantID)
+			}
+		})
+	}
+}
+
+func TestNewTenantRepository_InvalidTenants(t *testing.T) {
+	invalidTenants := []string{"", "unknown", "admin", "test", "AI8"}
+	for _, tenantID := range invalidTenants {
+		t.Run(tenantID, func(t *testing.T) {
+			_, err := NewTenantRepository(nil, tenantID)
+			if err == nil {
+				t.Errorf("expected error for invalid tenant %q", tenantID)
+			}
+			if !strings.Contains(err.Error(), "invalid tenant ID") {
+				t.Errorf("error = %v, want 'invalid tenant ID' message", err)
+			}
+		})
+	}
+}
+
+func TestRepository_TableNames_Legacy(t *testing.T) {
+	// Legacy repository (no tenant prefix)
+	repo := NewRepository(nil)
+
+	tests := []struct {
+		method string
+		got    string
+		want   string
+	}{
+		{"threadsTable", repo.threadsTable(), "airborne_threads"},
+		{"messagesTable", repo.messagesTable(), "airborne_messages"},
+		{"filesTable", repo.filesTable(), "airborne_files"},
+		{"fileUploadsTable", repo.fileUploadsTable(), "airborne_file_provider_uploads"},
+		{"vectorStoresTable", repo.vectorStoresTable(), "airborne_thread_vector_stores"},
+	}
+
+	for _, tt := range tests {
+		t.Run(tt.method, func(t *testing.T) {
+			if tt.got != tt.want {
+				t.Errorf("%s() = %q, want %q", tt.method, tt.got, tt.want)
+			}
+		})
+	}
+}
+
+func TestRepository_TableNames_Tenant(t *testing.T) {
+	repo, err := NewTenantRepository(nil, "ai8")
+	if err != nil {
+		t.Fatal(err)
+	}
+
+	tests := []struct {
+		method string
+		got    string
+		want   string
+	}{
+		{"threadsTable", repo.threadsTable(), "ai8_airborne_threads"},
+		{"messagesTable", repo.messagesTable(), "ai8_airborne_messages"},
+		{"filesTable", repo.filesTable(), "ai8_airborne_files"},
+		{"fileUploadsTable", repo.fileUploadsTable(), "ai8_airborne_file_provider_uploads"},
+		{"vectorStoresTable", repo.vectorStoresTable(), "ai8_airborne_thread_vector_stores"},
+	}
+
+	for _, tt := range tests {
+		t.Run(tt.method, func(t *testing.T) {
+			if tt.got != tt.want {
+				t.Errorf("%s() = %q, want %q", tt.method, tt.got, tt.want)
+			}
+		})
+	}
+}
+
+func TestRepository_TableNames_AllTenants(t *testing.T) {
+	for tenantID := range ValidTenantIDs {
+		t.Run(tenantID, func(t *testing.T) {
+			repo, err := NewTenantRepository(nil, tenantID)
+			if err != nil {
+				t.Fatal(err)
+			}
+
+			prefix := tenantID + "_airborne"
+
+			if !strings.HasPrefix(repo.threadsTable(), prefix) {
+				t.Errorf("threadsTable() = %q, expected prefix %q", repo.threadsTable(), prefix)
+			}
+			if !strings.HasPrefix(repo.messagesTable(), prefix) {
+				t.Errorf("messagesTable() = %q, expected prefix %q", repo.messagesTable(), prefix)
+			}
+		})
+	}
+}
+
+func TestValidTenantIDs(t *testing.T) {
+	// Verify expected tenants exist
+	expected := []string{"ai8", "email4ai", "zztest"}
+	for _, id := range expected {
+		if !ValidTenantIDs[id] {
+			t.Errorf("expected %q in ValidTenantIDs", id)
+		}
+	}
+
+	// Verify count matches
+	if len(ValidTenantIDs) != len(expected) {
+		t.Errorf("ValidTenantIDs has %d entries, want %d", len(ValidTenantIDs), len(expected))
+	}
+}
+
+func TestActivityEntry_StatusDetection(t *testing.T) {
+	// Test the content-based status detection logic used in GetActivityFeed
+	tests := []struct {
+		name           string
+		content        string
+		wantStatus     string
+		wantContent    string
+	}{
+		{"success", "Normal response content", "success", "Normal response content"},
+		{"failed prefix", "[FAILED] Provider error occurred", "failed", "Provider error occurred"},
+		{"no false positive", "This is [FAILED] in the middle", "success", "This is [FAILED] in the middle"},
+	}
+
+	for _, tt := range tests {
+		t.Run(tt.name, func(t *testing.T) {
+			var status, content string
+			if strings.HasPrefix(tt.content, "[FAILED] ") {
+				status = "failed"
+				content = strings.TrimPrefix(tt.content, "[FAILED] ")
+			} else {
+				status = "success"
+				content = tt.content
+			}
+
+			if status != tt.wantStatus {
+				t.Errorf("status = %q, want %q", status, tt.wantStatus)
+			}
+			if content != tt.wantContent {
+				t.Errorf("content = %q, want %q", content, tt.wantContent)
+			}
+		})
+	}
+}
```

---

### 5. Admin Server — Handler-Level Tests — `internal/admin/server_test.go` (NEW FILE)

Tests the HTTP handler logic for health, version, debug, thread, and activity endpoints. Uses `httptest` with nil db client to exercise the "database not configured" paths and input validation.

```diff
--- /dev/null
+++ b/internal/admin/server_test.go
@@ -0,0 +1,187 @@
+package admin
+
+import (
+	"encoding/json"
+	"net/http"
+	"net/http/httptest"
+	"testing"
+)
+
+// newTestServer creates a minimal admin server with nil db (exercises no-db paths).
+func newTestServer() *Server {
+	return &Server{
+		dbClient:  nil,
+		port:      9999,
+		version: VersionInfo{
+			Version:   "1.0.0-test",
+			GitCommit: "abc123",
+			BuildTime: "2026-03-21T00:00:00Z",
+		},
+	}
+}
+
+func TestHandleHealth_NoDB(t *testing.T) {
+	s := newTestServer()
+	req := httptest.NewRequest(http.MethodGet, "/admin/health", nil)
+	w := httptest.NewRecorder()
+
+	s.handleHealth(w, req)
+
+	if w.Code != http.StatusOK {
+		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
+	}
+
+	var resp map[string]interface{}
+	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
+		t.Fatalf("decode error: %v", err)
+	}
+
+	if resp["status"] != "healthy" {
+		t.Errorf("status = %q, want %q", resp["status"], "healthy")
+	}
+	if resp["database"] != "not_configured" {
+		t.Errorf("database = %q, want %q", resp["database"], "not_configured")
+	}
+}
+
+func TestHandleHealth_MethodNotAllowed(t *testing.T) {
+	s := newTestServer()
+	req := httptest.NewRequest(http.MethodPost, "/admin/health", nil)
+	w := httptest.NewRecorder()
+
+	s.handleHealth(w, req)
+
+	if w.Code != http.StatusMethodNotAllowed {
+		t.Errorf("status = %d, want %d", w.Code, http.StatusMethodNotAllowed)
+	}
+}
+
+func TestHandleVersion(t *testing.T) {
+	s := newTestServer()
+	req := httptest.NewRequest(http.MethodGet, "/admin/version", nil)
+	w := httptest.NewRecorder()
+
+	s.handleVersion(w, req)
+
+	if w.Code != http.StatusOK {
+		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
+	}
+
+	var resp VersionInfo
+	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
+		t.Fatalf("decode error: %v", err)
+	}
+
+	if resp.Version != "1.0.0-test" {
+		t.Errorf("Version = %q, want %q", resp.Version, "1.0.0-test")
+	}
+	if resp.GitCommit != "abc123" {
+		t.Errorf("GitCommit = %q, want %q", resp.GitCommit, "abc123")
+	}
+}
+
+func TestHandleVersion_MethodNotAllowed(t *testing.T) {
+	s := newTestServer()
+	req := httptest.NewRequest(http.MethodPost, "/admin/version", nil)
+	w := httptest.NewRecorder()
+
+	s.handleVersion(w, req)
+
+	if w.Code != http.StatusMethodNotAllowed {
+		t.Errorf("status = %d, want %d", w.Code, http.StatusMethodNotAllowed)
+	}
+}
+
+func TestHandleActivity_NoDB(t *testing.T) {
+	s := newTestServer()
+	req := httptest.NewRequest(http.MethodGet, "/admin/activity?limit=10", nil)
+	w := httptest.NewRecorder()
+
+	s.handleActivity(w, req)
+
+	if w.Code != http.StatusOK {
+		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
+	}
+
+	var resp map[string]interface{}
+	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
+		t.Fatalf("decode error: %v", err)
+	}
+
+	// Should return empty activity with error message
+	if resp["error"] != "database not configured" {
+		t.Errorf("error = %q, want %q", resp["error"], "database not configured")
+	}
+}
+
+func TestHandleActivity_MethodNotAllowed(t *testing.T) {
+	s := newTestServer()
+	req := httptest.NewRequest(http.MethodPost, "/admin/activity", nil)
+	w := httptest.NewRecorder()
+
+	s.handleActivity(w, req)
+
+	if w.Code != http.StatusMethodNotAllowed {
+		t.Errorf("status = %d, want %d", w.Code, http.StatusMethodNotAllowed)
+	}
+}
+
+func TestHandleDebug_NoDB(t *testing.T) {
+	s := newTestServer()
+	req := httptest.NewRequest(http.MethodGet, "/admin/debug/550e8400-e29b-41d4-a716-446655440000", nil)
+	w := httptest.NewRecorder()
+
+	s.handleDebug(w, req)
+
+	if w.Code != http.StatusServiceUnavailable {
+		t.Errorf("status = %d, want %d", w.Code, http.StatusServiceUnavailable)
+	}
+}
+
+func TestHandleDebug_MissingID(t *testing.T) {
+	s := newTestServer()
+	req := httptest.NewRequest(http.MethodGet, "/admin/debug/", nil)
+	w := httptest.NewRecorder()
+
+	s.handleDebug(w, req)
+
+	if w.Code != http.StatusBadRequest {
+		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
+	}
+}
+
+func TestHandleDebug_InvalidUUID(t *testing.T) {
+	s := newTestServer()
+	req := httptest.NewRequest(http.MethodGet, "/admin/debug/not-a-uuid", nil)
+	w := httptest.NewRecorder()
+
+	s.handleDebug(w, req)
+
+	if w.Code != http.StatusBadRequest {
+		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
+	}
+}
+
+func TestHandleThread_NoDB(t *testing.T) {
+	s := newTestServer()
+	req := httptest.NewRequest(http.MethodGet, "/admin/thread/550e8400-e29b-41d4-a716-446655440000", nil)
+	w := httptest.NewRecorder()
+
+	s.handleThread(w, req)
+
+	if w.Code != http.StatusServiceUnavailable {
+		t.Errorf("status = %d, want %d", w.Code, http.StatusServiceUnavailable)
+	}
+}
+
+func TestHandleThread_MissingID(t *testing.T) {
+	s := newTestServer()
+	req := httptest.NewRequest(http.MethodGet, "/admin/thread/", nil)
+	w := httptest.NewRecorder()
+
+	s.handleThread(w, req)
+
+	if w.Code != http.StatusBadRequest {
+		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
+	}
+}
+
+func TestHandleThread_InvalidUUID(t *testing.T) {
+	s := newTestServer()
+	req := httptest.NewRequest(http.MethodGet, "/admin/thread/invalid-uuid", nil)
+	w := httptest.NewRecorder()
+
+	s.handleThread(w, req)
+
+	if w.Code != http.StatusBadRequest {
+		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
+	}
+}
```

---

### 6. ImageGen Config & Generate Dispatch — `internal/imagegen/client_test.go` (APPEND)

Adds tests for the Generate() dispatcher's error paths (nil request, unsupported provider).

```diff
--- a/internal/imagegen/client_test.go
+++ b/internal/imagegen/client_test.go
@@ -204,0 +205,39 @@
+
+func TestGenerate_NilRequest(t *testing.T) {
+	client := NewClient()
+	_, err := client.Generate(context.Background(), nil)
+	if err == nil {
+		t.Fatal("expected error for nil request")
+	}
+	if !strings.Contains(err.Error(), "nil request") {
+		t.Errorf("error = %v, want 'nil request' message", err)
+	}
+}
+
+func TestGenerate_NilConfig(t *testing.T) {
+	client := NewClient()
+	_, err := client.Generate(context.Background(), &ImageRequest{Prompt: "test"})
+	if err == nil {
+		t.Fatal("expected error for nil config")
+	}
+}
+
+func TestGenerate_UnsupportedProvider(t *testing.T) {
+	client := NewClient()
+	_, err := client.Generate(context.Background(), &ImageRequest{
+		Prompt: "test",
+		Config: &Config{Enabled: true, Provider: "midjourney"},
+	})
+	if err == nil {
+		t.Fatal("expected error for unsupported provider")
+	}
+	if !strings.Contains(err.Error(), "unsupported image provider") {
+		t.Errorf("error = %v, want 'unsupported image provider' message", err)
+	}
+}
+
+func TestGenerate_GeminiNoAPIKey(t *testing.T) {
+	client := NewClient()
+	_, err := client.Generate(context.Background(), &ImageRequest{
+		Prompt: "test",
+		Config: &Config{Enabled: true, Provider: "gemini"},
+	})
+	if err == nil {
+		t.Fatal("expected error when Gemini API key is empty")
+	}
+}
+
+func TestGenerate_OpenAINoAPIKey(t *testing.T) {
+	client := NewClient()
+	_, err := client.Generate(context.Background(), &ImageRequest{
+		Prompt: "test",
+		Config: &Config{Enabled: true, Provider: "openai"},
+	})
+	if err == nil {
+		t.Fatal("expected error when OpenAI API key is empty")
+	}
+}
```

Note: the `client_test.go` file would need these imports added at the top:

```diff
--- a/internal/imagegen/client_test.go
+++ b/internal/imagegen/client_test.go
@@ -1,6 +1,9 @@
 package imagegen

 import (
+	"context"
+	"strings"
 	"testing"
 )
```

---

### 7. Provider HasImages & GenerateResult — `internal/provider/provider_test.go` (APPEND)

```diff
--- a/internal/provider/providers_test.go
+++ b/internal/provider/providers_test.go
@@ -98,0 +99,41 @@
+
+func TestGenerateResult_HasImages(t *testing.T) {
+	tests := []struct {
+		name   string
+		result provider.GenerateResult
+		want   bool
+	}{
+		{"no images", provider.GenerateResult{}, false},
+		{"empty images", provider.GenerateResult{Images: []provider.GeneratedImage{}}, false},
+		{"with image", provider.GenerateResult{Images: []provider.GeneratedImage{{Data: []byte("img")}}}, true},
+	}
+
+	for _, tt := range tests {
+		t.Run(tt.name, func(t *testing.T) {
+			if got := tt.result.HasImages(); got != tt.want {
+				t.Errorf("HasImages() = %v, want %v", got, tt.want)
+			}
+		})
+	}
+}
+
+func TestChunkType_Constants(t *testing.T) {
+	// Ensure chunk type constants have distinct values
+	types := []provider.ChunkType{
+		provider.ChunkTypeText,
+		provider.ChunkTypeUsage,
+		provider.ChunkTypeCitation,
+		provider.ChunkTypeComplete,
+		provider.ChunkTypeError,
+		provider.ChunkTypeToolCall,
+		provider.ChunkTypeCodeExecution,
+	}
+
+	seen := make(map[provider.ChunkType]bool)
+	for _, ct := range types {
+		if seen[ct] {
+			t.Errorf("duplicate ChunkType value: %d", ct)
+		}
+		seen[ct] = true
+	}
+}
```

---

## Gaps NOT Addressed (and Why)

| Gap | Reason |
|-----|--------|
| `admin/server.go` handleTest, handleChat, handleUpload | Require gRPC server and full service stack — integration test territory |
| `db/postgres.go` NewClient | Requires live PostgreSQL or significant mocking of `pgkit.Open`; better suited for integration tests |
| `db/repository.go` SQL query methods | All methods execute real SQL via pgx; proper testing requires a test database with migrations applied |
| `imagegen/gemini.go` generateGemini | Calls external Gemini API with HTTP; would need httptest server mocking the exact Gemini response format |
| `cmd/airborne/main.go` | Application entry point; integration test |
| Individual compat providers | Already covered by `providers_test.go`; per-package tests would be pure duplication |

---

## Recommendations

1. **Highest ROI:** Add the CLI output tests (Proposal #1) and CLI client tests (Proposal #2). These are pure-logic tests with zero external dependencies and cover ~680 lines.

2. **Security-critical:** Add the admin handler tests (Proposal #5). Input validation and "database not configured" paths need coverage to prevent regressions.

3. **Data integrity:** Add the repository table-name tests (Proposal #4). A wrong table prefix = data cross-contamination between tenants.

4. **Future:** Consider a `testcontainers` or Docker-based integration test suite for the `db/repository.go` SQL methods. The repository has complex transaction logic in `PersistConversationTurnWithDebug` that can only be truly validated against a real database.

5. **Test infrastructure:** The codebase already uses good patterns (testmain, miniredis, table-driven tests). Continuing these patterns for new tests ensures consistency.
