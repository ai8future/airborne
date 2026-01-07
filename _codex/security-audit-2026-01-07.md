# Security Audit Report: AIBox

**Date:** 2026-01-07
**Auditor:** Claude Code (Claude:Opus 4.5)
**Version Audited:** 0.4.2
**Scope:** Full codebase security review

---

## Executive Summary

AIBox is a Go-based gRPC gateway service providing unified access to multiple AI providers (OpenAI, Gemini, Anthropic) with multi-tenancy, RAG capabilities, and API key authentication. This audit identified **15 security issues** ranging from critical to informational.

| Severity | Count |
|----------|-------|
| Critical | 3 |
| High | 2 |
| Medium | 5 |
| Low | 5 |

---

## Critical Findings

### 1. Development Mode Bypasses All Authentication

**Location:** `internal/server/grpc.go:62-65`
**Severity:** CRITICAL
**CWE:** CWE-287 (Improper Authentication)

When Redis is unavailable in non-production mode, authentication and rate limiting are completely disabled:

```go
if cfg.StartupMode.IsProduction() {
    return nil, fmt.Errorf("redis required in production mode: %w", err)
}
slog.Warn("Redis not available - auth and rate limiting disabled (development mode)", "error", err)
```

**Risk:** If accidentally deployed in development mode, the entire API is unauthenticated. An attacker could access all endpoints without credentials.

**Recommendation:**
- Never fully bypass authentication
- Implement in-memory fallback for development
- Add startup warnings/blocks if auth is disabled
- Consider fail-closed behavior

---

### 2. Arbitrary File Read via FILE= Prefix

**Location:** `internal/tenant/secrets.go:39-46`
**Severity:** CRITICAL
**CWE:** CWE-22 (Path Traversal)

The secret loading mechanism allows reading arbitrary files without path validation:

```go
if strings.HasPrefix(value, "FILE=") {
    path := strings.TrimSpace(strings.TrimPrefix(value, "FILE="))
    data, err := os.ReadFile(path)  // No validation on path!
    ...
}
```

**Risk:** If tenant configuration is user-controlled or can be influenced by an attacker, they could read sensitive files such as:
- `/etc/passwd`
- `/etc/shadow` (if running as root)
- Private keys (`~/.ssh/id_rsa`)
- Other application secrets

**Proof of Concept:**
```yaml
providers:
  openai:
    api_key: "FILE=../../../etc/passwd"
```

**Recommendation:**
- Validate file paths against an allowlist of directories
- Reject paths containing `..` or absolute paths outside allowed directories
- Run the service with minimal filesystem permissions
- Consider using a secrets manager instead of file-based secrets

---

### 3. FileService Missing Authentication Check

**Location:** `internal/service/files.go` (all methods)
**Severity:** CRITICAL
**CWE:** CWE-862 (Missing Authorization)

Unlike `ChatService`, the `FileService` doesn't call `auth.RequirePermission()`:

```go
func (s *FileService) UploadFile(stream pb.FileService_UploadFileServer) error {
    ctx := stream.Context()
    // NO auth.RequirePermission(ctx, auth.PermissionFiles) check!
    ...
}

func (s *FileService) CreateFileStore(ctx context.Context, req *pb.CreateFileStoreRequest) (*pb.CreateFileStoreResponse, error) {
    // NO permission check
    if req.ClientId == "" {
        return nil, fmt.Errorf("client_id is required")
    }
    ...
}
```

**Affected Methods:**
- `CreateFileStore`
- `UploadFile`
- `DeleteFileStore`
- `GetFileStore`
- `ListFileStores`

**Risk:** Any authenticated user can upload, delete, and manage files regardless of their assigned permissions. The `PermissionFiles` permission exists but is never enforced.

**Recommendation:**
Add permission checks to all FileService methods:
```go
if err := auth.RequirePermission(ctx, auth.PermissionFiles); err != nil {
    return nil, err
}
```

---

## High Findings

### 4. Hardcoded Default Tenant ID in FileService

**Location:** `internal/service/files.go:112,157,185`
**Severity:** HIGH
**CWE:** CWE-284 (Improper Access Control)

FileService uses hardcoded `tenantID = "default"` instead of extracting from context:

```go
// Extract tenant ID from context or use a default
// In a real implementation, this would come from the auth interceptor
tenantID := "default"
```

**Risk:** All file operations from all tenants go to the same storage location, completely breaking tenant isolation. Tenant A can access Tenant B's files.

**Recommendation:**
Extract tenant ID from auth context consistently:
```go
tenantID := getTenantID(ctx)
if tenantID == "" {
    return nil, status.Error(codes.InvalidArgument, "tenant_id required")
}
```

---

### 5. API Keys Passed in Request Body

**Location:** `internal/service/chat.go:441-444`
**Severity:** HIGH
**CWE:** CWE-522 (Insufficiently Protected Credentials)

Provider API keys can be passed directly in gRPC requests:

```go
if pbCfg, ok := req.ProviderConfigs[providerName]; ok {
    if pbCfg.ApiKey != "" {
        cfg.APIKey = pbCfg.ApiKey
    }
}
```

**Risk:**
- API keys appear in request logs
- Keys may be cached in various layers
- Clients shouldn't hold provider API keys (violates principle of least privilege)
- Keys could be intercepted if TLS is misconfigured

**Recommendation:**
- Remove ability to pass API keys in requests
- Only allow API keys from secure server-side configuration (tenant config or environment variables)
- If client-provided keys are required, use a separate secure channel

---

## Medium Findings

### 6. Token Recording Race Condition

**Location:** `internal/auth/ratelimit.go:86-94`
**Severity:** MEDIUM
**CWE:** CWE-362 (Race Condition)

Non-atomic check for first increment in token recording:

```go
count, err := r.redis.IncrBy(ctx, key, tokens)
if err != nil {
    return fmt.Errorf("failed to record tokens: %w", err)
}

// Race condition: another request could have incremented first
if count == tokens {
    _ = r.redis.Expire(ctx, key, time.Minute)
}
```

**Risk:** If two requests arrive simultaneously, both may think they're first, or neither sets the TTL, leading to:
- Unbounded counter growth (no expiration)
- Memory leak in Redis

**Recommendation:**
Use a Lua script for atomic increment + conditional expire (similar to `rateLimitScript`):
```lua
local count = redis.call('INCRBY', KEYS[1], ARGV[1])
if count == tonumber(ARGV[1]) then
    redis.call('EXPIRE', KEYS[1], ARGV[2])
end
return count
```

---

### 7. No Collection Name Validation

**Location:** `internal/rag/service.go:296-298`
**Severity:** MEDIUM
**CWE:** CWE-20 (Improper Input Validation)

Collection names are constructed without sanitization:

```go
func (s *Service) collectionName(tenantID, storeID string) string {
    return fmt.Sprintf("%s_%s", tenantID, storeID)
}
```

**Risk:** Malicious `tenantID` or `storeID` could contain:
- Path traversal characters
- Special characters affecting Qdrant queries
- URL encoding issues

**Recommendation:**
Sanitize collection names:
```go
func sanitizeCollectionName(s string) string {
    return regexp.MustCompile(`[^a-zA-Z0-9_-]`).ReplaceAllString(s, "")
}
```

---

### 8. External Services Use HTTP (Not HTTPS) by Default

**Location:** `internal/config/config.go:162-169`
**Severity:** MEDIUM
**CWE:** CWE-319 (Cleartext Transmission)

All external service URLs default to plaintext HTTP:

```go
RAG: RAGConfig{
    OllamaURL:      "http://localhost:11434",
    QdrantURL:      "http://localhost:6333",
    DocboxURL:      "http://localhost:41273",
}
```

**Risk:** Data in transit is unencrypted:
- Document content sent to Docbox
- Embeddings sent to/from Ollama
- Vector data sent to/from Qdrant

**Recommendation:**
- Default to HTTPS for all external services
- Support mTLS for service-to-service communication
- Add certificate validation options

---

### 9. No Request/Response Size Limits for File Upload

**Location:** `internal/service/files.go:92-108`
**Severity:** MEDIUM
**CWE:** CWE-400 (Uncontrolled Resource Consumption)

File upload reads entire file into memory without size validation:

```go
var buf bytes.Buffer
for {
    msg, err := stream.Recv()
    if err == io.EOF {
        break
    }
    chunk := msg.GetChunk()
    buf.Write(chunk)  // No size check!
}
```

**Risk:** Denial of Service via memory exhaustion. An attacker could upload a multi-gigabyte file.

**Recommendation:**
```go
const MaxFileSize = 100 * 1024 * 1024 // 100MB

if buf.Len() > MaxFileSize {
    return status.Error(codes.InvalidArgument, "file too large")
}
```

---

### 10. bcrypt DefaultCost May Be Insufficient

**Location:** `internal/auth/keys.go:91`
**Severity:** MEDIUM
**CWE:** CWE-916 (Use of Password Hash With Insufficient Computational Effort)

```go
hash, err := bcrypt.GenerateFromPassword([]byte(secret), bcrypt.DefaultCost)
```

`bcrypt.DefaultCost` is 10. For API keys (high-value, long-lived secrets), this may be insufficient given modern GPU capabilities.

**Recommendation:**
- Use at least cost 12-14 for production
- Make cost configurable
- Consider Argon2id for new implementations

---

## Low Findings

### 11. Timing Side-Channel in Key Validation

**Location:** `internal/auth/keys.go:119-142`
**Severity:** LOW
**CWE:** CWE-208 (Observable Timing Discrepancy)

Key lookup happens before bcrypt comparison:

```go
key, err := s.getKey(ctx, keyID)  // Timing leak: key exists vs doesn't
if err != nil {
    return nil, err  // Fast return if key doesn't exist
}
// Slow bcrypt comparison only if key exists
if err := bcrypt.CompareHashAndPassword([]byte(key.SecretHash), []byte(secret)); err != nil {
    return nil, ErrInvalidKey
}
```

**Risk:** Attackers could enumerate valid key IDs by measuring response times.

**Recommendation:**
Always perform bcrypt comparison:
```go
dummyHash := "$2a$10$..." // Pre-computed dummy hash
key, err := s.getKey(ctx, keyID)
if err != nil {
    bcrypt.CompareHashAndPassword([]byte(dummyHash), []byte(secret))
    return nil, ErrInvalidKey
}
```

---

### 12. Error Messages Leak Internal Details

**Location:** `internal/rag/vectorstore/qdrant.go:77,223`
**Severity:** LOW
**CWE:** CWE-209 (Information Exposure Through Error Message)

Raw Qdrant errors are returned to callers:

```go
return false, fmt.Errorf("qdrant error (status %d): %s", resp.StatusCode, string(body))
```

**Risk:** Internal infrastructure details (Qdrant version, internal paths, configuration) may be exposed.

**Recommendation:**
Use error sanitization before returning to clients:
```go
slog.Error("qdrant error", "status", resp.StatusCode, "body", string(body))
return false, errors.New("vector store temporarily unavailable")
```

---

### 13. No Key Rotation Mechanism

**Location:** Design issue
**Severity:** LOW
**CWE:** CWE-324 (Use of a Key Past its Expiration Date)

No API key rotation or versioning mechanism exists. Keys can only be created or deleted.

**Risk:** Compromised keys require manual revocation with potential service disruption.

**Recommendation:**
- Implement key versioning (allow multiple active keys per client)
- Add grace period for key rotation
- Support key regeneration without service interruption

---

### 14. TLS Disabled by Default

**Location:** `internal/config/config.go:126-128`
**Severity:** LOW-MEDIUM
**CWE:** CWE-319 (Cleartext Transmission)

```go
TLS: TLSConfig{
    Enabled: false,
},
```

**Risk:** gRPC traffic including API keys and user data is unencrypted by default.

**Recommendation:**
- Enable TLS by default in production mode
- Require explicit opt-out with warning
- Document security implications

---

### 15. Redis Connection Without TLS

**Location:** `internal/redis/client.go:25-34`
**Severity:** LOW
**CWE:** CWE-319 (Cleartext Transmission)

Redis client has no TLS configuration option:

```go
rdb := redis.NewClient(&redis.Options{
    Addr:     cfg.Addr,
    Password: cfg.Password,
    // No TLS configuration
})
```

**Risk:** Redis credentials and cached data (API key hashes, rate limit data) transmitted in plaintext.

**Recommendation:**
Add TLS support:
```go
type Config struct {
    TLSEnabled bool
    TLSConfig  *tls.Config
}
```

---

## Code Quality Issues

### Unused Parameter in Lua Script
**Location:** `internal/auth/ratelimit.go:19`
```go
local limit = tonumber(ARGV[1])  -- Passed but never used in script
```

### Ignored Error on Token Recording
**Location:** `internal/service/chat.go:164`
```go
_ = s.rateLimiter.RecordTokens(ctx, client.ClientID, ...)
```
Token recording failures are silently ignored.

### Inconsistent Error Handling
Some methods return `fmt.Errorf` while others return `status.Error`. Consider consistent gRPC error codes.

---

## Positive Security Observations

| Aspect | Implementation | Assessment |
|--------|---------------|------------|
| Password Hashing | bcrypt for API key secrets | Good |
| Error Sanitization | `errors/sanitize.go` | Good |
| Input Validation | Size limits on user input | Good |
| Request ID Validation | Alphanumeric pattern | Good |
| Panic Recovery | gRPC interceptors | Good |
| Rate Limiting | Atomic Lua scripts | Good |
| Context-based Auth | Proper Go context usage | Good |
| Tenant Isolation | Collection prefixing | Partial (see #4) |
| Structured Logging | slog usage | Good |
| Permission Model | Granular permissions defined | Good (not enforced everywhere) |

---

## Remediation Priority

### Immediate (P0) - Fix within 24 hours
1. **#3** - Add auth checks to FileService
2. **#2** - Validate FILE= paths

### Urgent (P1) - Fix within 1 week
3. **#4** - Fix hardcoded tenant ID
4. **#5** - Remove API keys from requests
5. **#1** - Improve dev mode auth handling

### High (P2) - Fix within 2 weeks
6. **#9** - Add file upload size limits
7. **#6** - Fix token recording race
8. **#7** - Validate collection names

### Medium (P3) - Fix within 1 month
9. **#14** - Enable TLS by default
10. **#8** - HTTPS for external services
11. **#10** - Increase bcrypt cost

### Low (P4) - Track for future
12. **#11** - Timing side-channel
13. **#12** - Error message sanitization
14. **#13** - Key rotation mechanism
15. **#15** - Redis TLS support

---

## Appendix: Files Reviewed

| File | Lines | Security-Critical |
|------|-------|-------------------|
| `internal/auth/keys.go` | 236 | Yes |
| `internal/auth/interceptor.go` | 179 | Yes |
| `internal/auth/ratelimit.go` | 156 | Yes |
| `internal/auth/tenant_interceptor.go` | 160 | Yes |
| `internal/auth/errors.go` | 24 | No |
| `internal/tenant/secrets.go` | 61 | Yes |
| `internal/server/grpc.go` | 298 | Yes |
| `internal/service/chat.go` | 680 | Yes |
| `internal/service/files.go` | 214 | Yes |
| `internal/config/config.go` | 282 | Yes |
| `internal/validation/limits.go` | 89 | Yes |
| `internal/errors/sanitize.go` | 44 | Yes |
| `internal/redis/client.go` | 126 | Yes |
| `internal/rag/service.go` | 323 | Moderate |
| `internal/rag/vectorstore/qdrant.go` | 256 | Moderate |
| `internal/rag/extractor/docbox.go` | 202 | Moderate |
| `internal/provider/openai/client.go` | 451 | Moderate |
| `internal/provider/anthropic/client.go` | 372 | Moderate |
| `internal/provider/gemini/client.go` | 380 | Moderate |

---

*Report generated by Claude Code security audit*
