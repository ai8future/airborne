# Security Audit Report: AIBox (Remaining Issues)

**Date:** 2026-01-07
**Auditor:** Claude Code (Claude:Opus 4.5)
**Version Audited:** 0.4.2
**Last Updated:** 2026-01-07 (removed fixed issues through v0.5.3)

---

## Remaining Issues

The following issues from the original audit have NOT yet been fixed. These are mostly low-priority configuration recommendations and design improvements.

---

### 1. Token Recording Race Condition

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
Use a Lua script for atomic increment + conditional expire (similar to `rateLimitScript`).

---

### 2. External Services Use HTTP (Not HTTPS) by Default

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

### 3. bcrypt DefaultCost May Be Insufficient

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

### 4. Timing Side-Channel in Key Validation

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
Always perform bcrypt comparison against a dummy hash when key is not found.

---

### 5. Error Messages Leak Internal Details

**Location:** `internal/rag/vectorstore/qdrant.go:77,223`
**Severity:** LOW
**CWE:** CWE-209 (Information Exposure Through Error Message)

Raw Qdrant errors are returned to callers:

```go
return false, fmt.Errorf("qdrant error (status %d): %s", resp.StatusCode, string(body))
```

**Risk:** Internal infrastructure details (Qdrant version, internal paths, configuration) may be exposed.

**Recommendation:**
Use error sanitization before returning to clients.

---

### 6. No Key Rotation Mechanism

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

### 7. TLS Disabled by Default

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

### 8. Redis Connection Without TLS

**Location:** `internal/redis/client.go:25-34`
**Severity:** LOW
**CWE:** CWE-319 (Cleartext Transmission)

Redis client has no TLS configuration option.

**Risk:** Redis credentials and cached data (API key hashes, rate limit data) transmitted in plaintext.

**Recommendation:**
Add TLS support to Redis client configuration.

---

## Code Quality Issues

### Unused Parameter in Lua Script
**Location:** `internal/auth/ratelimit.go:19`
```go
local limit = tonumber(ARGV[1])  -- Passed but never used in script
```

### Inconsistent Error Handling
Some methods return `fmt.Errorf` while others return `status.Error`. Consider consistent gRPC error codes.

---

## Summary

| Category | Count |
|----------|-------|
| Medium Severity | 3 |
| Low Severity | 5 |
| Code Quality | 2 |

Most remaining issues are configuration recommendations and design improvements that can be addressed in future releases.

---

*Report generated by Claude Code security audit*
*Updated: 2026-01-07 - Removed issues fixed in versions 0.4.5 through 0.5.3*
