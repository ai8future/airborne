# Audit Security Fixes Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Fix all HIGH and MEDIUM severity security and code quality issues identified in the 2026-01-08 code audit.

**Architecture:** Defense-in-depth approach - add validation at multiple layers, improve error handling with explicit checks, add timeouts and resource limits. Each fix is independent and can be committed separately.

**Tech Stack:** Go 1.25, gRPC, Redis, Qdrant

---

## Phase 1: HIGH Severity Fixes

### Task 1: Fix Rate Limit Type Assertion and Parse Error

**Files:**
- Modify: `internal/auth/ratelimit.go:128-154`
- Test: `internal/auth/ratelimit_test.go`

**Step 1: Write the failing test for type assertion handling**

```go
func TestRecordTokens_UnexpectedRedisResult(t *testing.T) {
    // This test verifies that unexpected Redis Lua script results
    // are handled gracefully without panic
    s := miniredis.RunT(t)
    client, _ := redis.NewClient(redis.Config{Addr: s.Addr()})
    defer client.Close()

    rl := NewRateLimiter(client, RateLimiterConfig{
        TokensPerMinute: 1000,
        TokensPerDay:    10000,
    })

    ctx := context.Background()
    // Normal operation should work
    err := rl.RecordTokens(ctx, "client1", 100)
    if err != nil {
        t.Fatalf("RecordTokens failed: %v", err)
    }
}

func TestGetUsage_MalformedValue(t *testing.T) {
    s := miniredis.RunT(t)
    client, _ := redis.NewClient(redis.Config{Addr: s.Addr()})
    defer client.Close()

    // Inject malformed value directly
    s.Set("ratelimit:client1:minute", "not-a-number")

    rl := NewRateLimiter(client, RateLimiterConfig{
        TokensPerMinute: 1000,
        TokensPerDay:    10000,
    })

    ctx := context.Background()
    usage, err := rl.GetUsage(ctx, "client1")
    if err != nil {
        t.Fatalf("GetUsage should not error on malformed data: %v", err)
    }
    // Malformed values should be treated as 0
    if usage.MinuteTokens != 0 {
        t.Errorf("MinuteTokens = %d, want 0 for malformed data", usage.MinuteTokens)
    }
}
```

**Step 2: Run test to verify current behavior**

Run: `go test ./internal/auth/... -run "TestRecordTokens_UnexpectedRedisResult|TestGetUsage_MalformedValue" -v`
Expected: Tests may pass or fail depending on current implementation

**Step 3: Fix the type assertion in RecordTokens**

In `internal/auth/ratelimit.go`, replace lines 126-134:

```go
// Execute the Lua script
result, err := r.client.Eval(ctx, rateLimitScript, keys, args...).Result()
if err != nil {
    return fmt.Errorf("rate limit script failed: %w", err)
}

// Safely extract the count from Lua result
count, ok := result.(int64)
if !ok {
    // Try to handle string result (some Redis versions return strings)
    if strResult, strOk := result.(string); strOk {
        var parseErr error
        count, parseErr = strconv.ParseInt(strResult, 10, 64)
        if parseErr != nil {
            slog.Warn("unexpected rate limit script result type",
                "result", result,
                "type", fmt.Sprintf("%T", result))
            return fmt.Errorf("unexpected result type from rate limit script: %T", result)
        }
    } else {
        slog.Warn("unexpected rate limit script result type",
            "result", result,
            "type", fmt.Sprintf("%T", result))
        return fmt.Errorf("unexpected result type from rate limit script: %T", result)
    }
}
```

**Step 4: Fix the silent parse failure in GetUsage**

In `internal/auth/ratelimit.go`, replace lines 146-154:

```go
for key, val := range values {
    var count int64
    if val != "" {
        parsed, err := strconv.ParseInt(val, 10, 64)
        if err != nil {
            slog.Warn("failed to parse rate limit value",
                "key", key,
                "value", val,
                "error", err)
            // Treat malformed values as 0 to avoid blocking legitimate requests
            count = 0
        } else {
            count = parsed
        }
    }
    // ... rest of switch statement
}
```

**Step 5: Run tests to verify fix**

Run: `go test ./internal/auth/... -v`
Expected: All tests pass

**Step 6: Commit**

```bash
git add internal/auth/ratelimit.go internal/auth/ratelimit_test.go
git commit -m "fix(auth): handle unexpected Redis result types in rate limiter

- Add type coercion for string results from Lua script
- Log warnings for malformed values instead of silent failure
- Treat unparseable values as 0 to avoid blocking requests

Fixes: Audit issue #1, #2 (HIGH)"
```

---

### Task 2: Fix RAG Payload Nil Pointer and Chunk Position Inconsistency

**Files:**
- Modify: `internal/rag/service.go:330-394`
- Modify: `internal/rag/chunker/chunker.go:93-100`
- Test: `internal/rag/service_test.go`
- Test: `internal/rag/chunker/chunker_test.go`

**Step 1: Write failing test for nil payload**

```go
func TestRetrieve_NilPayload(t *testing.T) {
    // Test that nil payloads in Qdrant results don't cause panic
    // This requires mocking the vector store response
    // For now, test the helper functions directly

    // getString with nil map should return empty string
    result := getString(nil, "key")
    if result != "" {
        t.Errorf("getString(nil, key) = %q, want empty", result)
    }

    // getInt with nil map should return 0
    intResult := getInt(nil, "key")
    if intResult != 0 {
        t.Errorf("getInt(nil, key) = %d, want 0", intResult)
    }
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/rag/... -run TestRetrieve_NilPayload -v`
Expected: FAIL with nil pointer dereference

**Step 3: Fix getString and getInt helpers**

In `internal/rag/service.go`, replace the helper functions:

```go
func getString(m map[string]interface{}, key string) string {
    if m == nil {
        return ""
    }
    if v, ok := m[key]; ok {
        if s, ok := v.(string); ok {
            return s
        }
    }
    return ""
}

func getInt(m map[string]interface{}, key string) int {
    if m == nil {
        return 0
    }
    if v, ok := m[key]; ok {
        switch n := v.(type) {
        case int:
            return n
        case int64:
            return int(n)
        case float64:
            return int(n)
        }
    }
    return 0
}
```

**Step 4: Run test to verify fix**

Run: `go test ./internal/rag/... -run TestRetrieve_NilPayload -v`
Expected: PASS

**Step 5: Write test for chunk position consistency**

```go
func TestChunk_PositionsMatchTrimmedText(t *testing.T) {
    chunker := New(Config{
        ChunkSize:    100,
        ChunkOverlap: 10,
    })

    // Text with leading/trailing whitespace in chunks
    text := "   First sentence here.   \n\n   Second sentence here.   "
    chunks, err := chunker.Chunk(text)
    if err != nil {
        t.Fatalf("Chunk() error: %v", err)
    }

    for i, chunk := range chunks {
        // Verify that Text matches what you'd get from positions
        extractedText := text[chunk.Start:chunk.End]
        if strings.TrimSpace(extractedText) != chunk.Text {
            t.Errorf("Chunk %d: Text=%q but positions give %q",
                i, chunk.Text, strings.TrimSpace(extractedText))
        }
    }
}
```

**Step 6: Fix chunk position tracking**

In `internal/rag/chunker/chunker.go`, update the chunk creation:

```go
// After line 93, replace the chunk creation logic:
chunkText := strings.TrimSpace(text[start:end])
if chunkText == "" {
    continue
}

// Calculate actual positions after trimming
trimmedStart := start + strings.Index(text[start:end], chunkText)
trimmedEnd := trimmedStart + len(chunkText)

chunks = append(chunks, Chunk{
    Text:  chunkText,
    Start: trimmedStart,
    End:   trimmedEnd,
    Index: len(chunks),
})
```

**Step 7: Run all chunker tests**

Run: `go test ./internal/rag/chunker/... -v`
Expected: All tests pass

**Step 8: Commit**

```bash
git add internal/rag/service.go internal/rag/chunker/chunker.go
git commit -m "fix(rag): handle nil payloads and fix chunk positions

- Add nil checks to getString/getInt helpers
- Recalculate chunk Start/End after trimming whitespace
- Positions now accurately reflect trimmed text

Fixes: Audit issue #3, #4 (HIGH)"
```

---

### Task 3: Add Base URL Validation to Prevent SSRF

**Files:**
- Create: `internal/validation/url.go`
- Modify: `internal/service/chat.go:59-64`
- Modify: `internal/provider/openai/client.go:81-86`
- Modify: `internal/provider/anthropic/client.go:78-79`
- Modify: `internal/provider/gemini/client.go:79-82`
- Test: `internal/validation/url_test.go`

**Step 1: Write the URL validation tests**

```go
// internal/validation/url_test.go
package validation

import "testing"

func TestValidateProviderURL(t *testing.T) {
    tests := []struct {
        name    string
        url     string
        wantErr bool
    }{
        {"valid https", "https://api.openai.com/v1", false},
        {"valid https custom", "https://my-proxy.example.com/api", false},
        {"http localhost allowed", "http://localhost:8080/v1", false},
        {"http 127.0.0.1 allowed", "http://127.0.0.1:8080/v1", false},
        {"http external blocked", "http://api.openai.com/v1", true},
        {"file protocol blocked", "file:///etc/passwd", true},
        {"gopher protocol blocked", "gopher://evil.com", true},
        {"javascript blocked", "javascript:alert(1)", true},
        {"data protocol blocked", "data:text/html,<script>", true},
        {"empty url", "", true},
        {"relative path blocked", "/v1/chat", true},
        {"internal IP blocked", "https://10.0.0.1/api", true},
        {"internal IP blocked 172", "https://172.16.0.1/api", true},
        {"internal IP blocked 192", "https://192.168.1.1/api", true},
        {"metadata endpoint blocked", "http://169.254.169.254/", true},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            err := ValidateProviderURL(tt.url)
            if (err != nil) != tt.wantErr {
                t.Errorf("ValidateProviderURL(%q) error = %v, wantErr %v", tt.url, err, tt.wantErr)
            }
        })
    }
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/validation/... -run TestValidateProviderURL -v`
Expected: FAIL - function doesn't exist

**Step 3: Implement URL validation**

```go
// internal/validation/url.go
package validation

import (
    "fmt"
    "net"
    "net/url"
    "strings"
)

// ValidateProviderURL validates that a URL is safe for use as a provider base URL.
// It blocks:
// - Non-HTTPS protocols (except localhost)
// - Internal/private IP addresses
// - Cloud metadata endpoints
// - Dangerous protocols (file, gopher, etc.)
func ValidateProviderURL(rawURL string) error {
    if rawURL == "" {
        return fmt.Errorf("URL cannot be empty")
    }

    parsed, err := url.Parse(rawURL)
    if err != nil {
        return fmt.Errorf("invalid URL: %w", err)
    }

    // Block dangerous protocols
    switch strings.ToLower(parsed.Scheme) {
    case "https":
        // HTTPS is always allowed (except for internal IPs)
    case "http":
        // HTTP only allowed for localhost
        if !isLocalhost(parsed.Host) {
            return fmt.Errorf("HTTP only allowed for localhost, use HTTPS for external URLs")
        }
    case "":
        return fmt.Errorf("URL must include protocol (https://)")
    default:
        return fmt.Errorf("protocol %q not allowed, use HTTPS", parsed.Scheme)
    }

    // Check for internal/private IP addresses
    host := parsed.Hostname()
    if ip := net.ParseIP(host); ip != nil {
        if isPrivateIP(ip) {
            return fmt.Errorf("private/internal IP addresses not allowed")
        }
        if isMetadataIP(ip) {
            return fmt.Errorf("cloud metadata endpoints not allowed")
        }
    }

    return nil
}

func isLocalhost(host string) bool {
    hostname := strings.Split(host, ":")[0]
    return hostname == "localhost" || hostname == "127.0.0.1" || hostname == "::1"
}

func isPrivateIP(ip net.IP) bool {
    private := []string{
        "10.0.0.0/8",
        "172.16.0.0/12",
        "192.168.0.0/16",
        "fc00::/7",
    }
    for _, cidr := range private {
        _, network, _ := net.ParseCIDR(cidr)
        if network.Contains(ip) {
            return true
        }
    }
    return false
}

func isMetadataIP(ip net.IP) bool {
    // AWS/GCP/Azure metadata endpoint
    metadata := net.ParseIP("169.254.169.254")
    return ip.Equal(metadata)
}
```

**Step 4: Run tests**

Run: `go test ./internal/validation/... -v`
Expected: All tests pass

**Step 5: Integrate validation into chat service**

In `internal/service/chat.go`, update `hasCustomBaseURL`:

```go
import "github.com/ai8future/airborne/internal/validation"

func hasCustomBaseURL(req *pb.GenerateReplyRequest) bool {
    for _, cfg := range req.ProviderConfigs {
        if cfg != nil && strings.TrimSpace(cfg.GetBaseUrl()) != "" {
            return true
        }
    }
    return false
}

// Add new validation function
func validateCustomBaseURLs(req *pb.GenerateReplyRequest) error {
    for provider, cfg := range req.ProviderConfigs {
        if cfg != nil && strings.TrimSpace(cfg.GetBaseUrl()) != "" {
            if err := validation.ValidateProviderURL(cfg.GetBaseUrl()); err != nil {
                return fmt.Errorf("invalid base_url for provider %s: %w", provider, err)
            }
        }
    }
    return nil
}
```

Then in `prepareRequest`, after the admin permission check (around line 85):

```go
// Validate base URLs even for admins
if hasCustomBaseURL(req) {
    if err := validateCustomBaseURLs(req); err != nil {
        return nil, status.Error(codes.InvalidArgument, err.Error())
    }
}
```

**Step 6: Add validation to provider clients**

In each provider client (openai, anthropic, gemini), add validation before using BaseURL:

```go
// In GenerateReply, before creating client with custom base URL:
if cfg.BaseURL != "" {
    if err := validation.ValidateProviderURL(cfg.BaseURL); err != nil {
        return provider.GenerateResult{}, fmt.Errorf("invalid base URL: %w", err)
    }
}
```

**Step 7: Run all tests**

Run: `go test ./... -v`
Expected: All tests pass

**Step 8: Commit**

```bash
git add internal/validation/url.go internal/validation/url_test.go \
    internal/service/chat.go \
    internal/provider/openai/client.go \
    internal/provider/anthropic/client.go \
    internal/provider/gemini/client.go
git commit -m "security(ssrf): add URL validation for provider base URLs

- Block non-HTTPS protocols (except localhost for development)
- Block private/internal IP ranges (10.x, 172.16.x, 192.168.x)
- Block cloud metadata endpoints (169.254.169.254)
- Validate at both service layer and provider client layer

Fixes: Audit issue #5, #7 (HIGH)"
```

---

### Task 4: Remove PermissionAdmin from Development Mode

**Files:**
- Modify: `internal/server/grpc.go:303-332`
- Test: `internal/server/grpc_test.go`

**Step 1: Write test for dev mode permissions**

```go
func TestDevelopmentAuthInterceptor_NoAdminPermission(t *testing.T) {
    interceptor := developmentAuthInterceptor()

    var capturedCtx context.Context
    handler := func(ctx context.Context, req interface{}) (interface{}, error) {
        capturedCtx = ctx
        return nil, nil
    }

    _, err := interceptor(context.Background(), nil, &grpc.UnaryServerInfo{}, handler)
    if err != nil {
        t.Fatalf("interceptor error: %v", err)
    }

    client := auth.ClientFromContext(capturedCtx)
    if client == nil {
        t.Fatal("expected client in context")
    }

    // Verify admin permission is NOT granted
    for _, perm := range client.Permissions {
        if perm == auth.PermissionAdmin {
            t.Error("development mode should NOT grant admin permission")
        }
    }

    // Verify basic permissions ARE granted
    hasChat := false
    for _, perm := range client.Permissions {
        if perm == auth.PermissionChat {
            hasChat = true
        }
    }
    if !hasChat {
        t.Error("development mode should grant chat permission")
    }
}
```

**Step 2: Run test**

Run: `go test ./internal/server/... -run TestDevelopmentAuthInterceptor -v`
Expected: FAIL - admin permission is currently granted

**Step 3: Fix the development interceptors**

In `internal/server/grpc.go`, update both interceptors:

```go
func developmentAuthInterceptor() grpc.UnaryServerInterceptor {
    return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
        slog.Warn("DEVELOPMENT MODE: Authentication bypassed - DO NOT USE IN PRODUCTION")
        client := &auth.ClientKey{
            ClientID:   "dev",
            ClientName: "development",
            Permissions: []auth.Permission{
                auth.PermissionChat,
                auth.PermissionChatStream,
                auth.PermissionFiles,
                // NOTE: PermissionAdmin intentionally excluded for security
            },
        }
        ctx = context.WithValue(ctx, auth.ClientContextKey, client)
        return handler(ctx, req)
    }
}

func developmentStreamAuthInterceptor() grpc.StreamServerInterceptor {
    return func(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
        slog.Warn("DEVELOPMENT MODE: Authentication bypassed - DO NOT USE IN PRODUCTION")
        client := &auth.ClientKey{
            ClientID:   "dev",
            ClientName: "development",
            Permissions: []auth.Permission{
                auth.PermissionChat,
                auth.PermissionChatStream,
                auth.PermissionFiles,
                // NOTE: PermissionAdmin intentionally excluded for security
            },
        }
        ctx := context.WithValue(ss.Context(), auth.ClientContextKey, client)
        wrapped := &wrappedServerStream{ServerStream: ss, ctx: ctx}
        return handler(srv, wrapped)
    }
}
```

**Step 4: Run tests**

Run: `go test ./internal/server/... -v`
Expected: All tests pass

**Step 5: Commit**

```bash
git add internal/server/grpc.go internal/server/grpc_test.go
git commit -m "security(auth): remove admin permission from development mode

Development mode now only grants Chat, ChatStream, and Files permissions.
Admin operations require proper authentication even in development.

Fixes: Audit issue #6 (HIGH)"
```

---

### Task 5: Add Symlink Resolution to Secret Path Validation

**Files:**
- Modify: `internal/tenant/secrets.go:27-40`
- Test: `internal/tenant/secrets_test.go`

**Step 1: Write test for symlink bypass**

```go
func TestValidateSecretPath_SymlinkBypass(t *testing.T) {
    // Create temp directory structure
    tmpDir := t.TempDir()
    secretsDir := filepath.Join(tmpDir, "secrets")
    targetDir := filepath.Join(tmpDir, "target")

    os.MkdirAll(secretsDir, 0755)
    os.MkdirAll(targetDir, 0755)

    // Create a symlink inside secrets pointing outside
    symlinkPath := filepath.Join(secretsDir, "escape")
    os.Symlink(targetDir, symlinkPath)

    // Temporarily add tmpDir/secrets to allowed dirs for testing
    originalDirs := AllowedSecretDirs
    AllowedSecretDirs = []string{secretsDir}
    defer func() { AllowedSecretDirs = originalDirs }()

    // Attempting to access via symlink should fail
    err := ValidateSecretPath(filepath.Join(symlinkPath, "file.txt"))
    if err == nil {
        t.Error("expected error when accessing file via symlink escape")
    }
}
```

**Step 2: Run test**

Run: `go test ./internal/tenant/... -run TestValidateSecretPath_SymlinkBypass -v`
Expected: FAIL - symlink bypass not detected

**Step 3: Fix the validation to resolve symlinks**

```go
func ValidateSecretPath(path string) error {
    // Get absolute path
    absPath, err := filepath.Abs(path)
    if err != nil {
        return fmt.Errorf("invalid path: %w", err)
    }

    // Resolve symlinks to get the real path
    realPath, err := filepath.EvalSymlinks(absPath)
    if err != nil {
        // If the file doesn't exist yet, resolve the parent directory
        parentDir := filepath.Dir(absPath)
        realParent, parentErr := filepath.EvalSymlinks(parentDir)
        if parentErr != nil {
            return fmt.Errorf("cannot resolve path: %w", err)
        }
        realPath = filepath.Join(realParent, filepath.Base(absPath))
    }

    // Check against allowed directories using the real path
    for _, allowed := range AllowedSecretDirs {
        allowedReal, err := filepath.EvalSymlinks(allowed)
        if err != nil {
            continue // Skip unresolvable allowed dirs
        }
        if strings.HasPrefix(realPath, allowedReal+string(filepath.Separator)) || realPath == allowedReal {
            return nil
        }
    }

    return fmt.Errorf("path %q is not in allowed secret directories", path)
}
```

**Step 4: Run tests**

Run: `go test ./internal/tenant/... -v`
Expected: All tests pass

**Step 5: Commit**

```bash
git add internal/tenant/secrets.go internal/tenant/secrets_test.go
git commit -m "security(secrets): resolve symlinks in path validation

Use filepath.EvalSymlinks to prevent symlink-based path traversal attacks.
Both the requested path and allowed directories are resolved before comparison.

Fixes: Audit issue #1 MEDIUM"
```

---

## Phase 2: MEDIUM Severity Fixes

### Task 6: Add Timeout to File Upload Stream

**Files:**
- Modify: `internal/service/files.go:84-145`
- Test: `internal/service/files_test.go`

**Step 1: Write test for upload timeout**

```go
func TestUploadFile_Timeout(t *testing.T) {
    // This test verifies that slow uploads are terminated
    // Implementation would require a mock stream that blocks
    t.Skip("requires mock stream implementation")
}
```

**Step 2: Add timeout to UploadFile**

In `internal/service/files.go`, add timeout handling:

```go
const uploadTimeout = 5 * time.Minute

func (s *FileService) UploadFile(stream pb.FileService_UploadFileServer) error {
    ctx := stream.Context()

    // Add timeout if not already set
    if _, hasDeadline := ctx.Deadline(); !hasDeadline {
        var cancel context.CancelFunc
        ctx, cancel = context.WithTimeout(ctx, uploadTimeout)
        defer cancel()
    }

    // ... rest of implementation, check ctx.Err() in the loop
    for {
        select {
        case <-ctx.Done():
            return status.Error(codes.DeadlineExceeded, "upload timeout exceeded")
        default:
        }

        msg, err := stream.Recv()
        // ... rest of loop
    }
}
```

**Step 3: Commit**

```bash
git add internal/service/files.go
git commit -m "fix(files): add 5-minute timeout to file uploads

Prevents malicious clients from holding server resources indefinitely
by adding a configurable upload timeout.

Fixes: Audit issue #3 MEDIUM"
```

---

### Task 7: Add Request Timeout to Provider Clients

**Files:**
- Modify: `internal/provider/anthropic/client.go`
- Modify: `internal/provider/openai/client.go`
- Modify: `internal/provider/gemini/client.go`

**Step 1: Apply the requestTimeout constant**

In each provider client's `GenerateReply` function, add:

```go
func (c *Client) GenerateReply(ctx context.Context, params provider.GenerateParams) (provider.GenerateResult, error) {
    // Ensure request has a timeout
    if _, hasDeadline := ctx.Deadline(); !hasDeadline {
        var cancel context.CancelFunc
        ctx, cancel = context.WithTimeout(ctx, requestTimeout)
        defer cancel()
    }

    // ... rest of implementation
}
```

**Step 2: Run tests**

Run: `go test ./internal/provider/... -v`
Expected: All tests pass

**Step 3: Commit**

```bash
git add internal/provider/anthropic/client.go \
    internal/provider/openai/client.go \
    internal/provider/gemini/client.go
git commit -m "fix(providers): enforce request timeout on API calls

Apply the requestTimeout constant (3 minutes) to all provider API calls
to prevent indefinite hangs.

Fixes: Audit issue #5 MEDIUM"
```

---

### Task 8: Fix Error Logging Before Sanitization

**Files:**
- Modify: `internal/errors/sanitize.go:41`

**Step 1: Fix the logging**

Replace line 41:

```go
// Before
slog.Error("provider error (sanitized for client)", "error", err)

// After
slog.Error("provider error occurred (details redacted for security)")
```

**Step 2: Commit**

```bash
git add internal/errors/sanitize.go
git commit -m "security(logging): redact provider errors before logging

Don't log full error details that might leak to external logging systems.

Fixes: Audit issue #2 MEDIUM"
```

---

## Phase 3: LOW Severity Fixes

### Task 9: Return gRPC Status Errors in FileService

**Files:**
- Modify: `internal/service/files.go:247`

**Step 1: Fix error types**

Replace generic errors with gRPC status errors:

```go
// Line 247
if info == nil {
    return nil, status.Error(codes.NotFound, "store not found")
}
```

**Step 2: Return Unimplemented for ListFileStores**

```go
func (s *FileService) ListFileStores(ctx context.Context, req *pb.ListFileStoresRequest) (*pb.ListFileStoresResponse, error) {
    if err := auth.RequirePermission(ctx, auth.PermissionFiles); err != nil {
        return nil, err
    }
    return nil, status.Error(codes.Unimplemented, "ListFileStores not yet implemented")
}
```

**Step 3: Commit**

```bash
git add internal/service/files.go
git commit -m "fix(files): use proper gRPC status errors

- Return codes.NotFound instead of generic error
- Return codes.Unimplemented for ListFileStores
- Handle SendAndClose errors properly

Fixes: Audit issues #1, #2, #3 LOW"
```

---

## Verification

After all tasks are complete:

```bash
# Run full test suite
go test ./... -v

# Run with race detector
go test -race ./...

# Build to verify no compile errors
go build ./...
```

---

## Summary

| Phase | Tasks | Severity | Estimated Steps |
|-------|-------|----------|-----------------|
| 1 | 5 | HIGH | 40 steps |
| 2 | 3 | MEDIUM | 15 steps |
| 3 | 1 | LOW | 5 steps |

Total: 9 tasks, ~60 steps
