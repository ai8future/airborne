Date Created: 2026-02-15T23:01:23Z
TOTAL_SCORE: 79/100

---

# Airborne Quick Analysis Report

**Agent**: Claude:Opus 4.6 | **Codebase**: AI Gateway / LLM Proxy (Go 1.25.5, gRPC)
**Version**: 1.8.3 | **Files**: ~50 source + 47 test files

**Score Breakdown**:
- Security: 18/25 (strong SSRF protection, minor CA cert & info leak issues)
- Code Quality: 20/25 (clean architecture, some long functions, magic numbers)
- Test Coverage: 18/25 (47 test files, but missing admin server & integration tests)
- Maintainability: 23/25 (well-structured, good patterns, proper error handling)

---

## 1. AUDIT — Security & Code Quality Issues

### AUDIT-1: CA Certificate Written to World-Accessible `/tmp` Directory (MEDIUM)

**File**: `internal/db/postgres.go:159-174`
**Risk**: `/tmp` is a shared directory. While file permissions are 0600, the directory `/tmp/airborne-certs` could be created by an attacker before the app starts (symlink attack / TOCTOU race). On shared hosts, another user could preempt directory creation.

```diff
--- a/internal/db/postgres.go
+++ b/internal/db/postgres.go
@@ -157,9 +157,16 @@ func (c *Client) logQuery(query string, args ...interface{}) {
 // writeCACertToFile writes a PEM-encoded CA certificate to a temporary file.
 // Returns the path to the certificate file.
 func writeCACertToFile(certPEM string) (string, error) {
-	// Use a stable path so we don't create multiple files on restarts
-	certDir := "/tmp/airborne-certs"
-	if err := os.MkdirAll(certDir, 0700); err != nil {
+	// Use XDG_RUNTIME_DIR or fall back to user-specific directory under /tmp.
+	// Avoids world-accessible /tmp for sensitive certificate material.
+	certDir := os.Getenv("XDG_RUNTIME_DIR")
+	if certDir == "" {
+		certDir = fmt.Sprintf("/tmp/airborne-certs-%d", os.Getuid())
+	} else {
+		certDir = filepath.Join(certDir, "airborne-certs")
+	}
+
+	if err := os.MkdirAll(certDir, 0700); err != nil {
 		return "", fmt.Errorf("failed to create cert directory: %w", err)
 	}
```

### AUDIT-2: Internal Error Messages Leaked to Clients in Admin Endpoints (MEDIUM)

**File**: `internal/admin/server.go:352,419,931,960,981`
**Risk**: Multiple admin endpoints return raw `err.Error()` to clients. While admin endpoints are behind auth, internal errors can leak database table names, file paths, or infrastructure details.

```diff
--- a/internal/admin/server.go
+++ b/internal/admin/server.go
@@ -349,7 +349,7 @@ func (s *Server) handleDebug(w http.ResponseWriter, r *http.Request) {
 		} else {
 			w.WriteHeader(http.StatusInternalServerError)
 			json.NewEncoder(w).Encode(map[string]interface{}{
-				"error": err.Error(),
+				"error": "internal error fetching debug data",
 			})
 		}
 		return
```

Apply same pattern at lines 419 (handleThread), 931 (handleUpload multipart error), and 981 (handleUpload Gemini error). Keep specific user-input errors (like "invalid format") but sanitize infrastructure errors.

### AUDIT-3: CORS Allows All Origins on Admin Server (LOW)

**File**: `internal/admin/server.go:123-126`
**Risk**: `AllowOrigins: []string{"*"}` allows any website to make authenticated requests to admin endpoints if a user's browser has a valid session. This is acceptable only if admin is behind a VPN/firewall.

```diff
--- a/internal/admin/server.go
+++ b/internal/admin/server.go
@@ -120,7 +120,7 @@ func NewServer(dbClient *db.Client, cfg Config) *Server {
 	handler := httpkit.Recovery(logger)(
 		guard.CORS(guard.CORSConfig{
-			AllowOrigins: []string{"*"},
+			AllowOrigins: cfg.AllowedOrigins, // Configure explicitly per environment
 			AllowMethods: []string{"GET", "POST", "OPTIONS"},
 			AllowHeaders: []string{"Content-Type", "Authorization"},
 		})(
```

Would require adding `AllowedOrigins []string` to `Config` struct and defaulting to `["*"]` for dev/`["https://admin.yourdomain.com"]` for prod.

### AUDIT-4: Byte-Based String Truncation Can Split Multi-Byte UTF-8 Characters (LOW)

**File**: `internal/admin/server.go:888`
**Risk**: `content[:maxAIResponseChars]` uses byte indexing. If the 500th byte falls in the middle of a multi-byte UTF-8 character (e.g., emoji, CJK), the resulting string is malformed.

```diff
--- a/internal/admin/server.go
+++ b/internal/admin/server.go
@@ -885,7 +885,11 @@ func buildCompressedHistory(dbMessages []db.Message, previousResponseID *string)
 		if msg.Role == "assistant" {
 			if aiResponseCount > dropAIResponsesLimit {
 				continue
 			}
 			if currentAIResponse > fullAIResponsesLimit && len(content) > maxAIResponseChars {
-				content = content[:maxAIResponseChars] + "..."
+				// Use rune-safe truncation to avoid splitting multi-byte chars
+				runes := []rune(content)
+				if len(runes) > maxAIResponseChars {
+					content = string(runes[:maxAIResponseChars]) + "..."
+				}
 			}
 		}
```

---

## 2. TESTS — Proposed Unit Tests

### TEST-1: Admin Server Handler Tests (Missing — No `admin/server_test.go`)

The admin server has 10+ HTTP handlers with no unit tests. This is the largest untested surface area in the codebase.

```diff
--- /dev/null
+++ b/internal/admin/server_test.go
@@ -0,0 +1,119 @@
+package admin
+
+import (
+	"encoding/json"
+	"net/http"
+	"net/http/httptest"
+	"testing"
+)
+
+func TestHandleVersion(t *testing.T) {
+	s := &Server{
+		version: VersionInfo{
+			Version:   "1.8.3",
+			GitCommit: "abc123",
+			BuildTime: "2026-01-01T00:00:00Z",
+		},
+	}
+
+	req := httptest.NewRequest(http.MethodGet, "/admin/version", nil)
+	w := httptest.NewRecorder()
+
+	s.handleVersion(w, req)
+
+	if w.Code != http.StatusOK {
+		t.Errorf("expected 200, got %d", w.Code)
+	}
+
+	var resp VersionInfo
+	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
+		t.Fatalf("failed to decode response: %v", err)
+	}
+	if resp.Version != "1.8.3" {
+		t.Errorf("expected version 1.8.3, got %s", resp.Version)
+	}
+}
+
+func TestHandleVersion_MethodNotAllowed(t *testing.T) {
+	s := &Server{}
+	req := httptest.NewRequest(http.MethodPost, "/admin/version", nil)
+	w := httptest.NewRecorder()
+
+	s.handleVersion(w, req)
+
+	if w.Code != http.StatusMethodNotAllowed {
+		t.Errorf("expected 405, got %d", w.Code)
+	}
+}
+
+func TestHandleHealth_NoDB(t *testing.T) {
+	s := &Server{dbClient: nil}
+
+	req := httptest.NewRequest(http.MethodGet, "/admin/health", nil)
+	w := httptest.NewRecorder()
+
+	s.handleHealth(w, req)
+
+	if w.Code != http.StatusOK {
+		t.Errorf("expected 200, got %d", w.Code)
+	}
+
+	var resp map[string]interface{}
+	json.NewDecoder(w.Body).Decode(&resp)
+	if resp["status"] != "healthy" {
+		t.Errorf("expected healthy status, got %v", resp["status"])
+	}
+}
+
+func TestHandleDebug_MissingMessageID(t *testing.T) {
+	s := &Server{}
+	req := httptest.NewRequest(http.MethodGet, "/admin/debug/", nil)
+	w := httptest.NewRecorder()
+
+	s.handleDebug(w, req)
+
+	if w.Code != http.StatusBadRequest {
+		t.Errorf("expected 400, got %d", w.Code)
+	}
+}
+
+func TestHandleDebug_InvalidUUID(t *testing.T) {
+	s := &Server{}
+	req := httptest.NewRequest(http.MethodGet, "/admin/debug/not-a-uuid", nil)
+	w := httptest.NewRecorder()
+
+	s.handleDebug(w, req)
+
+	if w.Code != http.StatusBadRequest {
+		t.Errorf("expected 400, got %d", w.Code)
+	}
+}
+
+func TestHandleDebug_NoDB(t *testing.T) {
+	s := &Server{dbClient: nil}
+	req := httptest.NewRequest(http.MethodGet, "/admin/debug/550e8400-e29b-41d4-a716-446655440000", nil)
+	w := httptest.NewRecorder()
+
+	s.handleDebug(w, req)
+
+	if w.Code != http.StatusServiceUnavailable {
+		t.Errorf("expected 503, got %d", w.Code)
+	}
+}
+
+func TestHandleUpload_MethodNotAllowed(t *testing.T) {
+	s := &Server{}
+	req := httptest.NewRequest(http.MethodGet, "/admin/upload", nil)
+	w := httptest.NewRecorder()
+
+	s.handleUpload(w, req)
+
+	if w.Code != http.StatusMethodNotAllowed {
+		t.Errorf("expected 405, got %d", w.Code)
+	}
+}
+
+func TestBuildCompressedHistory_Empty(t *testing.T) {
+	var prevID string
+	result := buildCompressedHistory(nil, &prevID)
+	if len(result) != 0 {
+		t.Errorf("expected empty result, got %d messages", len(result))
+	}
+}
```

### TEST-2: Error Sanitization Edge Cases (Supplement existing `errors/sanitize_test.go`)

```diff
--- a/internal/errors/sanitize_test.go
+++ b/internal/errors/sanitize_test.go
@@ -end,0 +end,30 @@
+
+func TestSanitizeForClient_NilError(t *testing.T) {
+	result := SanitizeForClient(nil)
+	if result != "" {
+		t.Errorf("expected empty string for nil error, got %q", result)
+	}
+}
+
+func TestSanitizeForClient_APIKeyLeak(t *testing.T) {
+	// Ensure API key patterns in error messages get sanitized to generic message
+	err := fmt.Errorf("failed with API key sk-abc123xyz: connection refused to internal-api.corp.net:8443")
+	result := SanitizeForClient(err)
+	if strings.Contains(result, "sk-abc") {
+		t.Error("API key leaked through sanitization")
+	}
+	if strings.Contains(result, "internal-api") {
+		t.Error("internal hostname leaked through sanitization")
+	}
+}
+
+func TestSanitizeForClient_UnknownError(t *testing.T) {
+	err := fmt.Errorf("some completely unknown internal error with details")
+	result := SanitizeForClient(err)
+	if result != "provider temporarily unavailable" {
+		t.Errorf("expected generic message, got %q", result)
+	}
+}
```

### TEST-3: `db/postgres.go` — CA Cert File Writing

```diff
--- /dev/null
+++ b/internal/db/postgres_cert_test.go
@@ -0,0 +1,45 @@
+package db
+
+import (
+	"os"
+	"path/filepath"
+	"testing"
+)
+
+func TestWriteCACertToFile(t *testing.T) {
+	// Use a temp directory to avoid writing to /tmp in tests
+	tmpDir := t.TempDir()
+	origDir := "/tmp/airborne-certs"
+	// Note: This test validates current behavior; see AUDIT-1 for improvement
+
+	certPEM := "-----BEGIN CERTIFICATE-----\nMIIBxTCCAWugAwIBAgIRAIPY...\n-----END CERTIFICATE-----"
+
+	path, err := writeCACertToFile(certPEM)
+	if err != nil {
+		t.Fatalf("writeCACertToFile failed: %v", err)
+	}
+	defer os.RemoveAll(origDir)
+
+	// Verify file exists
+	if _, err := os.Stat(path); os.IsNotExist(err) {
+		t.Fatal("certificate file was not created")
+	}
+
+	// Verify permissions
+	info, _ := os.Stat(path)
+	if info.Mode().Perm() != 0600 {
+		t.Errorf("expected 0600 permissions, got %o", info.Mode().Perm())
+	}
+
+	// Verify directory permissions
+	dirInfo, _ := os.Stat(filepath.Dir(path))
+	if dirInfo.Mode().Perm() != 0700 {
+		t.Errorf("expected 0700 directory permissions, got %o", dirInfo.Mode().Perm())
+	}
+
+	// Verify content
+	data, err := os.ReadFile(path)
+	if err != nil {
+		t.Fatalf("failed to read cert file: %v", err)
+	}
+	if string(data) != certPEM {
+		t.Error("certificate content mismatch")
+	}
+}
```

### TEST-4: Validation URL — DNS Rebinding & Edge Cases

```diff
--- a/internal/validation/url_test.go
+++ b/internal/validation/url_test.go
@@ -end,0 +end,35 @@
+
+func TestValidateProviderURL_IPv6Mapped(t *testing.T) {
+	// IPv4-mapped IPv6 addresses should be caught
+	err := ValidateProviderURL("https://[::ffff:10.0.0.1]/api")
+	if err == nil {
+		t.Error("expected error for IPv4-mapped IPv6 private address")
+	}
+}
+
+func TestValidateProviderURL_ZeroIP(t *testing.T) {
+	err := ValidateProviderURL("https://0.0.0.0/api")
+	if err == nil {
+		t.Error("expected error for 0.0.0.0")
+	}
+}
+
+func TestValidateProviderURL_URLWithCredentials(t *testing.T) {
+	// URLs with embedded credentials should work but the credentials
+	// shouldn't cause SSRF bypass
+	err := ValidateProviderURL("https://user:pass@api.openai.com/v1")
+	if err != nil {
+		t.Errorf("unexpected error for URL with credentials: %v", err)
+	}
+}
+
+func TestValidateProviderURL_EmptyScheme(t *testing.T) {
+	err := ValidateProviderURL("://evil.com")
+	if err == nil {
+		t.Error("expected error for URL with empty scheme")
+	}
+}
```

---

## 3. FIXES — Bugs, Issues & Code Smells

### FIX-1: `buildCompressedHistory` Nil Pointer Dereference on `previousResponseID` (BUG)

**File**: `internal/admin/server.go:870-873`
**Issue**: If `previousResponseID` is nil, the line `*previousResponseID = *msg.ResponseID` will panic. The function signature accepts `*string` but callers might pass nil.

```diff
--- a/internal/admin/server.go
+++ b/internal/admin/server.go
@@ -867,7 +867,7 @@ func buildCompressedHistory(dbMessages []db.Message, previousResponseID *string)
 	for _, msg := range dbMessages {
 		// Track previous response ID for OpenAI native continuity
-		if msg.Role == "assistant" && msg.ResponseID != nil && *msg.ResponseID != "" {
+		if previousResponseID != nil && msg.Role == "assistant" && msg.ResponseID != nil && *msg.ResponseID != "" {
 			*previousResponseID = *msg.ResponseID
 			currentAIResponse++
 		}
```

### FIX-2: `handleActivity` Returns 200 OK With Error Body (CODE SMELL)

**File**: `internal/admin/server.go:213`
**Issue**: When fetching activity fails, the handler returns `200 OK` with an error in the body. The comment says "matches Bizops pattern" — but this confuses monitoring and makes error detection harder. The activity array is always empty on error, so clients can't distinguish "no activity" from "database down."

```diff
--- a/internal/admin/server.go
+++ b/internal/admin/server.go
@@ -210,8 +210,8 @@ func (s *Server) handleActivity(w http.ResponseWriter, r *http.Request) {
 	if err != nil {
 		slog.Error("failed to fetch activity", "error", err)
 		w.Header().Set("Content-Type", "application/json")
-		w.WriteHeader(http.StatusOK) // Return 200 with error in body (matches Bizops pattern)
+		w.WriteHeader(http.StatusInternalServerError)
 		json.NewEncoder(w).Encode(map[string]interface{}{
 			"activity": []interface{}{},
-			"error":    err.Error(),
+			"error":    "failed to fetch activity data",
 		})
```

### FIX-3: Hardcoded Default Tenant `email4ai` in Upload Handler (CODE SMELL)

**File**: `internal/admin/server.go:950-951`
**Issue**: Default tenant is hardcoded as `"email4ai"`. This should be configurable or at minimum a named constant. If the default tenant is removed, uploads will silently fail with a confusing error.

```diff
--- a/internal/admin/server.go
+++ b/internal/admin/server.go
@@ -36,6 +36,9 @@ type Server struct {
+// defaultTenantID is the fallback tenant when no tenant_id is specified.
+const defaultTenantID = "email4ai"
+
 // Server is the HTTP admin server for operational endpoints.
 type Server struct {
@@ -948,7 +951,7 @@ func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
 	tenantID := r.FormValue("tenant_id")
 	if tenantID == "" {
-		tenantID = "email4ai" // Default tenant
+		tenantID = defaultTenantID
 	}
```

### FIX-4: `waitForCompletion` Unbounded Poll Loop (DEFENSIVE)

**File**: `internal/provider/openai/client.go:596-625`
**Issue**: While context timeout provides a safety net, adding a max iteration count provides defense-in-depth against stuck polling when the context has a very long timeout.

```diff
--- a/internal/provider/openai/client.go
+++ b/internal/provider/openai/client.go
@@ -594,7 +594,10 @@ func waitForCompletion(ctx context.Context, client openai.Client, resp *response
 	}

+	const maxPollIterations = 120 // Safety limit: ~10 min with exponential backoff
 	pollInterval := pollInitial
-	for {
+	for i := 0; i < maxPollIterations; i++ {
 		select {
 		case <-ctx.Done():
 			return nil, ctx.Err()
@@ -623,6 +626,7 @@ func waitForCompletion(ctx context.Context, client openai.Client, resp *response
 		// Increase poll interval
 		pollInterval = min(pollInterval*2, pollMax)
 	}
+	return nil, fmt.Errorf("response polling exceeded %d iterations", maxPollIterations)
 }
```

### FIX-5: `json.NewEncoder(w).Encode()` Errors Silently Ignored (LOW)

**File**: `internal/admin/server.go` (38 occurrences)
**Issue**: All `json.NewEncoder(w).Encode(...)` calls ignore the returned error. While encoding usually succeeds, connection resets or write failures are silently dropped. At minimum, log the error.

```diff
--- a/internal/admin/server.go
+++ b/internal/admin/server.go
@@ -293,1 +293,3 @@ func (s *Server) handleVersion(w http.ResponseWriter, r *http.Request) {
 	w.Header().Set("Content-Type", "application/json")
-	json.NewEncoder(w).Encode(s.version)
+	if err := json.NewEncoder(w).Encode(s.version); err != nil {
+		slog.Debug("failed to encode response", "error", err)
+	}
```

This pattern should be applied to high-value endpoints (version, activity, chat responses). Not critical for error-path encodings.

---

## 4. REFACTOR — Improvement Opportunities

### REFACTOR-1: Extract Magic Numbers to Named Constants

Multiple timeout and buffer values are scattered as literals:
- `internal/retry/retry.go:13` — `2 * time.Minute` request timeout
- `internal/provider/anthropic/client.go:21` — `15 * time.Minute` thinking timeout
- `internal/admin/server.go:849-853` — compression limits (`30000`, `500`, `3`, `6`)
- `internal/admin/server.go:926` — `100 << 20` upload limit
- `internal/redis/client.go` — `10` pool size, `2*time.Minute` idle time

Extract to package-level constants with descriptive names.

### REFACTOR-2: Admin Handler Response Pattern — DRY Up JSON Error Responses

The admin server has ~38 instances of the pattern:
```go
w.Header().Set("Content-Type", "application/json")
w.WriteHeader(status)
json.NewEncoder(w).Encode(map[string]interface{}{"error": msg})
```

Consider a helper: `func jsonError(w http.ResponseWriter, status int, msg string)` — or better, use `httpkit.JSONError` consistently (it's already used in some places).

### REFACTOR-3: Reduce `buildCompressedHistory` Complexity

**File**: `internal/admin/server.go:848-906`
This function mixes three concerns: AI response counting, progressive compression, and char-limit enforcement. Extract the compression strategy into a separate type:
```go
type compressionStrategy struct { ... }
func (c *compressionStrategy) shouldInclude(msg db.Message) bool
func (c *compressionStrategy) compress(content string) string
```

### REFACTOR-4: Provider Client Initialization in `ChatService`

**File**: `internal/service/chat.go:54-58`
Provider clients are created directly in `NewChatService`. This makes testing harder and prevents configuration. Accept providers via dependency injection (the `provider.Provider` interface already exists).

### REFACTOR-5: Tenant ID Validation — Move from Hardcoded List to Config

**File**: `internal/db/models.go:23-31`
Valid tenant IDs are hardcoded as constants. When a new tenant is added, code must be changed. Move the valid tenant list to configuration (already have tenant configs loaded from Doppler — derive valid IDs from there).

### REFACTOR-6: Consistent Error Handling in Admin Endpoints

Some handlers use `httpkit.JSONError(w, r, status, msg)` while others manually set headers and encode JSON. Standardize on `httpkit.JSONError` throughout for consistency and to automatically include request-id/tracing context.
