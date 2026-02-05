Date Created: 2026-01-24 22:48:00
TOTAL_SCORE: 78/100

# Airborne Quick Codebase Analysis

## Project Overview

**Airborne** is a multi-tenant, multi-provider AI orchestration platform written in Go (1.25.5) with a React/Next.js dashboard. It acts as a unified gateway to 14+ LLM providers (OpenAI, Gemini, Anthropic, Cohere, DeepSeek, Fireworks, Perplexity, Mistral, Cerebras, Grok, etc.) via gRPC with PostgreSQL persistence, Redis caching, and RAG capabilities.

**Version:** 1.7.11 | **Lines:** 16,862 (source) + 11,276 (tests)

---

## Section 1: AUDIT - Security and Code Quality Issues

### Issue 1.1: Admin Server CORS Wildcard (MEDIUM)

**File:** `internal/admin/server.go:87`
**Issue:** Uses `Access-Control-Allow-Origin: *` allowing any origin to access admin endpoints
**Risk:** XSS/CSRF attacks from any domain

```diff
--- a/internal/admin/server.go
+++ b/internal/admin/server.go
@@ -84,7 +84,11 @@ func (s *Server) corsMiddleware(next http.Handler) http.Handler {
 	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
-		w.Header().Set("Access-Control-Allow-Origin", "*")
+		allowedOrigin := s.config.CORSAllowedOrigin
+		if allowedOrigin == "" {
+			allowedOrigin = "https://dashboard.example.com"
+		}
+		w.Header().Set("Access-Control-Allow-Origin", allowedOrigin)
 		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
 		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Tenant-ID")
```

### Issue 1.2: Silent Error in Doppler Config Loader (MEDIUM)

**File:** `internal/tenant/doppler.go`
**Issue:** `body, _ := io.ReadAll(resp.Body)` ignores read error
**Risk:** Silent failure to load tenant configs

```diff
--- a/internal/tenant/doppler.go
+++ b/internal/tenant/doppler.go
@@ -45,7 +45,10 @@ func (d *DopplerClient) FetchSecrets(ctx context.Context) (map[string]string, er
 	}
 	defer resp.Body.Close()

-	body, _ := io.ReadAll(resp.Body)
+	body, err := io.ReadAll(resp.Body)
+	if err != nil {
+		return nil, fmt.Errorf("failed to read doppler response: %w", err)
+	}

 	if resp.StatusCode != http.StatusOK {
 		return nil, fmt.Errorf("doppler request failed: %s - %s", resp.Status, string(body))
```

### Issue 1.3: Temp File Cleanup Not Guaranteed (LOW)

**File:** `internal/db/postgres.go`
**Issue:** CA cert temp file may not be cleaned up on error paths

```diff
--- a/internal/db/postgres.go
+++ b/internal/db/postgres.go
@@ -112,12 +112,17 @@ func writeCACertToFile(caCert string) (string, error) {
 	if err != nil {
 		return "", fmt.Errorf("failed to create temp file for CA cert: %w", err)
 	}
-	defer tmpFile.Close()
+	tmpPath := tmpFile.Name()
+
+	cleanup := func() {
+		tmpFile.Close()
+		os.Remove(tmpPath)
+	}

 	if _, err := tmpFile.WriteString(caCert); err != nil {
-		os.Remove(tmpFile.Name())
+		cleanup()
 		return "", fmt.Errorf("failed to write CA cert to temp file: %w", err)
 	}

-	return tmpFile.Name(), nil
+	tmpFile.Close()
+	return tmpPath, nil
 }
```

---

## Section 2: TESTS - Proposed Unit Tests

### Test 2.1: Admin Server Endpoint Tests

**File:** `internal/admin/server_test.go` (new or extend existing)
**Target:** Untested HTTP handlers (handleActivity, handleDebug, handleThread, handleChat, handleUpload)

```diff
--- /dev/null
+++ b/internal/admin/server_test.go
@@ -0,0 +1,145 @@
+package admin
+
+import (
+	"bytes"
+	"context"
+	"encoding/json"
+	"net/http"
+	"net/http/httptest"
+	"testing"
+
+	"github.com/stretchr/testify/assert"
+	"github.com/stretchr/testify/require"
+)
+
+func TestHandleActivity(t *testing.T) {
+	tests := []struct {
+		name           string
+		tenantID       string
+		limit          string
+		expectedStatus int
+	}{
+		{
+			name:           "valid request with default limit",
+			tenantID:       "test-tenant",
+			limit:          "",
+			expectedStatus: http.StatusOK,
+		},
+		{
+			name:           "valid request with custom limit",
+			tenantID:       "test-tenant",
+			limit:          "50",
+			expectedStatus: http.StatusOK,
+		},
+		{
+			name:           "limit exceeds maximum",
+			tenantID:       "test-tenant",
+			limit:          "500",
+			expectedStatus: http.StatusOK, // should cap at 200
+		},
+		{
+			name:           "invalid limit",
+			tenantID:       "test-tenant",
+			limit:          "invalid",
+			expectedStatus: http.StatusBadRequest,
+		},
+		{
+			name:           "missing tenant",
+			tenantID:       "",
+			limit:          "",
+			expectedStatus: http.StatusBadRequest,
+		},
+	}
+
+	for _, tt := range tests {
+		t.Run(tt.name, func(t *testing.T) {
+			srv := newTestServer(t)
+
+			req := httptest.NewRequest("GET", "/activity", nil)
+			if tt.tenantID != "" {
+				req.Header.Set("X-Tenant-ID", tt.tenantID)
+			}
+			if tt.limit != "" {
+				q := req.URL.Query()
+				q.Set("limit", tt.limit)
+				req.URL.RawQuery = q.Encode()
+			}
+
+			w := httptest.NewRecorder()
+			srv.handleActivity(w, req)
+
+			assert.Equal(t, tt.expectedStatus, w.Code)
+		})
+	}
+}
+
+func TestHandleDebug(t *testing.T) {
+	tests := []struct {
+		name           string
+		method         string
+		expectedStatus int
+	}{
+		{
+			name:           "GET returns debug info",
+			method:         "GET",
+			expectedStatus: http.StatusOK,
+		},
+		{
+			name:           "POST not allowed",
+			method:         "POST",
+			expectedStatus: http.StatusMethodNotAllowed,
+		},
+	}
+
+	for _, tt := range tests {
+		t.Run(tt.name, func(t *testing.T) {
+			srv := newTestServer(t)
+
+			req := httptest.NewRequest(tt.method, "/debug", nil)
+			w := httptest.NewRecorder()
+			srv.handleDebug(w, req)
+
+			assert.Equal(t, tt.expectedStatus, w.Code)
+		})
+	}
+}
+
+func TestHandleUpload(t *testing.T) {
+	tests := []struct {
+		name           string
+		contentType    string
+		body           []byte
+		expectedStatus int
+	}{
+		{
+			name:           "missing content type",
+			contentType:    "",
+			body:           []byte("test"),
+			expectedStatus: http.StatusBadRequest,
+		},
+		{
+			name:           "empty body",
+			contentType:    "multipart/form-data",
+			body:           nil,
+			expectedStatus: http.StatusBadRequest,
+		},
+	}
+
+	for _, tt := range tests {
+		t.Run(tt.name, func(t *testing.T) {
+			srv := newTestServer(t)
+
+			req := httptest.NewRequest("POST", "/upload", bytes.NewReader(tt.body))
+			if tt.contentType != "" {
+				req.Header.Set("Content-Type", tt.contentType)
+			}
+			w := httptest.NewRecorder()
+			srv.handleUpload(w, req)
+
+			assert.Equal(t, tt.expectedStatus, w.Code)
+		})
+	}
+}
+
+func newTestServer(t *testing.T) *Server {
+	t.Helper()
+	// Create server with mock dependencies
+	return &Server{
+		config: &Config{},
+		// Add mock repository, etc.
+	}
+}
```

### Test 2.2: DB Repository CRUD Tests

**File:** `internal/db/repository_test.go` (extend existing)
**Target:** Multi-tenant query building edge cases

```diff
--- a/internal/db/repository_test.go
+++ b/internal/db/repository_test.go
@@ -100,6 +100,78 @@ func TestRepository_TenantIsolation(t *testing.T) {
+func TestRepository_CreateThread(t *testing.T) {
+	tests := []struct {
+		name      string
+		tenantID  string
+		userID    string
+		title     string
+		wantErr   bool
+		errMsg    string
+	}{
+		{
+			name:     "valid thread creation",
+			tenantID: "tenant1",
+			userID:   "user1",
+			title:    "Test Thread",
+			wantErr:  false,
+		},
+		{
+			name:     "empty tenant ID",
+			tenantID: "",
+			userID:   "user1",
+			title:    "Test Thread",
+			wantErr:  true,
+			errMsg:   "tenant ID required",
+		},
+		{
+			name:     "invalid tenant ID",
+			tenantID: "invalid-tenant",
+			userID:   "user1",
+			title:    "Test Thread",
+			wantErr:  true,
+			errMsg:   "invalid tenant",
+		},
+		{
+			name:     "title at max length",
+			tenantID: "tenant1",
+			userID:   "user1",
+			title:    string(make([]byte, 255)),
+			wantErr:  false,
+		},
+		{
+			name:     "title exceeds max length",
+			tenantID: "tenant1",
+			userID:   "user1",
+			title:    string(make([]byte, 256)),
+			wantErr:  true,
+			errMsg:   "title too long",
+		},
+	}
+
+	for _, tt := range tests {
+		t.Run(tt.name, func(t *testing.T) {
+			repo := setupTestRepo(t)
+			ctx := context.Background()
+
+			thread, err := repo.CreateThread(ctx, tt.tenantID, tt.userID, tt.title)
+
+			if tt.wantErr {
+				require.Error(t, err)
+				assert.Contains(t, err.Error(), tt.errMsg)
+				return
+			}
+
+			require.NoError(t, err)
+			assert.NotEmpty(t, thread.ID)
+			assert.Equal(t, tt.tenantID, thread.TenantID)
+			assert.Equal(t, tt.userID, thread.UserID)
+			assert.Equal(t, tt.title, thread.Title)
+		})
+	}
+}
+
+func TestRepository_ConcurrentAccess(t *testing.T) {
+	repo := setupTestRepo(t)
+	ctx := context.Background()
+
+	const numGoroutines = 10
+	errors := make(chan error, numGoroutines)
+
+	for i := 0; i < numGoroutines; i++ {
+		go func(idx int) {
+			_, err := repo.CreateThread(ctx, "tenant1", fmt.Sprintf("user%d", idx), "Concurrent Thread")
+			errors <- err
+		}(i)
+	}
+
+	for i := 0; i < numGoroutines; i++ {
+		err := <-errors
+		assert.NoError(t, err)
+	}
+}
```

### Test 2.3: RAG Service Tests

**File:** `internal/rag/service_test.go` (new or extend)
**Target:** Vector search and embedding operations

```diff
--- /dev/null
+++ b/internal/rag/service_test.go
@@ -0,0 +1,85 @@
+package rag
+
+import (
+	"context"
+	"testing"
+
+	"github.com/stretchr/testify/assert"
+	"github.com/stretchr/testify/mock"
+	"github.com/stretchr/testify/require"
+)
+
+type mockVectorStore struct {
+	mock.Mock
+}
+
+func (m *mockVectorStore) Search(ctx context.Context, query []float32, limit int) ([]SearchResult, error) {
+	args := m.Called(ctx, query, limit)
+	return args.Get(0).([]SearchResult), args.Error(1)
+}
+
+func (m *mockVectorStore) Upsert(ctx context.Context, id string, vector []float32, metadata map[string]interface{}) error {
+	args := m.Called(ctx, id, vector, metadata)
+	return args.Error(0)
+}
+
+type mockEmbedder struct {
+	mock.Mock
+}
+
+func (m *mockEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
+	args := m.Called(ctx, text)
+	return args.Get(0).([]float32), args.Error(1)
+}
+
+func TestService_Search(t *testing.T) {
+	tests := []struct {
+		name        string
+		query       string
+		limit       int
+		mockResults []SearchResult
+		mockErr     error
+		wantErr     bool
+	}{
+		{
+			name:  "successful search",
+			query: "test query",
+			limit: 5,
+			mockResults: []SearchResult{
+				{ID: "doc1", Score: 0.95, Content: "relevant content"},
+				{ID: "doc2", Score: 0.85, Content: "somewhat relevant"},
+			},
+			wantErr: false,
+		},
+		{
+			name:    "empty query",
+			query:   "",
+			limit:   5,
+			wantErr: true,
+		},
+		{
+			name:    "limit zero",
+			query:   "test",
+			limit:   0,
+			wantErr: true,
+		},
+	}
+
+	for _, tt := range tests {
+		t.Run(tt.name, func(t *testing.T) {
+			mockStore := new(mockVectorStore)
+			mockEmbed := new(mockEmbedder)
+
+			if !tt.wantErr {
+				mockEmbed.On("Embed", mock.Anything, tt.query).Return([]float32{0.1, 0.2, 0.3}, nil)
+				mockStore.On("Search", mock.Anything, mock.Anything, tt.limit).Return(tt.mockResults, tt.mockErr)
+			}
+
+			svc := NewService(mockStore, mockEmbed)
+			results, err := svc.Search(context.Background(), tt.query, tt.limit)
+
+			if tt.wantErr {
+				require.Error(t, err)
+				return
+			}
+
+			require.NoError(t, err)
+			assert.Len(t, results, len(tt.mockResults))
+		})
+	}
+}
```

---

## Section 3: FIXES - Bugs and Code Smells

### Fix 3.1: Add Missing Error Return in Doppler

**File:** `internal/tenant/doppler.go`
**Issue:** JSON unmarshal error not checked after body read

```diff
--- a/internal/tenant/doppler.go
+++ b/internal/tenant/doppler.go
@@ -52,7 +52,10 @@ func (d *DopplerClient) FetchSecrets(ctx context.Context) (map[string]string, er
 	}

 	var response DopplerResponse
-	json.Unmarshal(body, &response)
+	if err := json.Unmarshal(body, &response); err != nil {
+		return nil, fmt.Errorf("failed to parse doppler response: %w", err)
+	}

 	secrets := make(map[string]string)
 	for key, value := range response.Secrets {
```

### Fix 3.2: Add Query Result Pagination to handleActivity

**File:** `internal/admin/server.go`
**Issue:** No pagination support for potentially large result sets

```diff
--- a/internal/admin/server.go
+++ b/internal/admin/server.go
@@ -120,6 +120,7 @@ func (s *Server) handleActivity(w http.ResponseWriter, r *http.Request) {
 	tenantID := r.Header.Get("X-Tenant-ID")
 	limitStr := r.URL.Query().Get("limit")
+	offsetStr := r.URL.Query().Get("offset")

 	limit := 100
 	if limitStr != "" {
@@ -132,7 +133,16 @@ func (s *Server) handleActivity(w http.ResponseWriter, r *http.Request) {
 		limit = 200
 	}

-	activities, err := s.repo.GetRecentActivity(r.Context(), tenantID, limit)
+	offset := 0
+	if offsetStr != "" {
+		parsed, err := strconv.Atoi(offsetStr)
+		if err != nil || parsed < 0 {
+			http.Error(w, "invalid offset parameter", http.StatusBadRequest)
+			return
+		}
+		offset = parsed
+	}
+
+	activities, err := s.repo.GetRecentActivity(r.Context(), tenantID, limit, offset)
 	if err != nil {
 		http.Error(w, err.Error(), http.StatusInternalServerError)
 		return
```

### Fix 3.3: Close Response Body on Error Path

**File:** `internal/provider/openai/client.go`
**Issue:** Response body not closed when status check fails before body read

```diff
--- a/internal/provider/openai/client.go
+++ b/internal/provider/openai/client.go
@@ -145,6 +145,7 @@ func (c *Client) doRequest(ctx context.Context, req *http.Request) (*http.Respon
 	}

 	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
+		defer resp.Body.Close()
 		body, _ := io.ReadAll(resp.Body)
-		resp.Body.Close()
 		return nil, fmt.Errorf("request failed with status %d: %s", resp.StatusCode, string(body))
 	}
```

---

## Section 4: REFACTOR - Code Quality Improvements

### Refactor 4.1: Split Large Files

The following files exceed 1,000 lines and should be refactored:

| File | Lines | Recommended Split |
|------|-------|-------------------|
| `internal/provider/gemini/client.go` | 1,209 | Extract `gemini_streaming.go`, `gemini_pricing.go` |
| `internal/service/chat.go` | 1,260 | Extract `chat_streaming.go`, `chat_context.go` |
| `internal/admin/server.go` | 1,284 | Extract `admin_handlers.go`, `admin_middleware.go` |

### Refactor 4.2: Extract Common HTTP Handling

Create `internal/provider/httputil/client.go` to consolidate:
- Retry logic with exponential backoff
- Common error response parsing
- Request/response logging
- Timeout handling

This would reduce duplication across 14 provider implementations.

### Refactor 4.3: Add Interface Abstractions for Testing

Current issue: Many services directly instantiate dependencies, making unit testing difficult.

Recommendation: Add interfaces for:
- `Repository` interface for DB operations
- `VectorStore` interface for RAG operations
- `Embedder` interface for embedding generation
- `ProviderClient` interface for LLM providers

### Refactor 4.4: Consolidate Configuration Loading

Multiple config loading patterns exist:
- Environment variables directly
- Doppler secrets
- Static config files

Consider unifying into a single `ConfigLoader` interface with pluggable backends.

### Refactor 4.5: Add Request Tracing

No distributed tracing is currently implemented. Consider adding:
- OpenTelemetry integration
- Request ID propagation
- Span creation for provider calls

This would significantly improve observability for debugging production issues.

---

## Summary

| Category | Grade | Issues Found |
|----------|-------|--------------|
| Security | A- (85) | 2 medium, 1 low |
| Testing | B (75) | 3 major gaps identified |
| Bug Fixes | B+ (80) | 3 fixes recommended |
| Refactoring | B (75) | 5 opportunities |

**Overall: 78/100** - A solid, well-architected codebase with excellent security foundations. Primary areas for improvement are test coverage expansion and large file refactoring.
