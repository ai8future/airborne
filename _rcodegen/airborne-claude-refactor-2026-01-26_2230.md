# Airborne Codebase Refactoring Assessment

Date Created: 2026-01-26 22:30:00 UTC
TOTAL_SCORE: 82/100

---

## Executive Summary

Airborne is a well-architected, production-grade multi-tenant LLM orchestration platform. The codebase demonstrates strong software engineering practices with clear separation of concerns, comprehensive test coverage, and thoughtful abstraction patterns. The refactoring opportunities identified are mostly optimizations rather than fundamental issues.

**Version Analyzed:** 1.7.12
**Total Go Files:** 112
**Total Lines of Code:** ~28,000 (Go backend)
**Test Files:** 40 test files with comprehensive coverage

---

## Scoring Breakdown

| Category | Score | Max | Notes |
|----------|-------|-----|-------|
| Code Organization | 16/20 | 20 | Excellent package structure, some large files |
| DRY Principles | 14/20 | 20 | Good use of compat layer, some retry duplication |
| API Design | 18/20 | 20 | Clean provider interface, well-defined contracts |
| Error Handling | 16/20 | 20 | Consistent patterns, good sanitization |
| Maintainability | 18/20 | 20 | Clear code, good documentation, no TODOs |

---

## Strengths

### 1. Provider Abstraction (Excellent)
The `provider.Provider` interface is well-designed and enables clean separation:
- Common interface for all 17+ AI providers
- `compat.Client` elegantly reuses OpenAI-compatible implementations for 13 providers
- Only 3 providers (OpenAI, Gemini, Anthropic) need custom implementations

```go
// Example: DeepSeek client is just ~60 lines
type Client struct {
    *compat.Client
}
```

### 2. Multi-Tenant Architecture (Strong)
- Clear tenant isolation via `Repository.tablePrefix`
- Tenant-aware interceptors for gRPC
- Environment-based secrets management

### 3. Centralized Retry Logic
The `internal/retry` package provides unified retry handling:
- `IsRetryable()` - consistent error classification
- `SleepWithBackoff()` - exponential backoff with jitter
- `MaxAttempts` and `RequestTimeout` constants

### 4. Test Coverage (Comprehensive)
- 40 test files covering critical paths
- Mock utilities in `rag/testutil/mocks.go`
- Provider-specific tests for edge cases

### 5. Security Patterns
- SSRF prevention via `ValidateProviderURL`
- Error sanitization preventing internal detail leakage
- Admin permission checks for custom base URLs

---

## Refactoring Opportunities

### Category A: Code Duplication (Medium Priority)

#### A1. Retry Loop Pattern Duplication
**Location:** `internal/provider/*/client.go`, `internal/provider/compat/openai_compat.go`

The retry loop pattern is duplicated across provider implementations:
- `openai/client.go:235-268`
- `gemini/client.go:257-309`
- `anthropic/client.go:183-247`
- `compat/openai_compat.go:171-212`

**Pattern appearing 4+ times:**
```go
for attempt := 1; attempt <= retry.MaxAttempts; attempt++ {
    // context timeout setup
    // execute request
    if err != nil {
        // timeout check
        // retryable check
        // backoff sleep
    }
    // success handling
}
```

**Recommendation:** Extract to a generic retry executor:
```go
func ExecuteWithRetry[T any](ctx context.Context, name string, fn func(context.Context) (T, error)) (T, error)
```
This would reduce ~150 lines of duplicated code while maintaining provider-specific error handling.

---

#### A2. HTTP Client Configuration Duplication
**Location:** Provider initialization code

Each major provider repeats HTTP client setup:
```go
httpCfg, err := httputil.NewCapturedClientConfig(cfg.APIKey, cfg.BaseURL)
if err != nil {
    return provider.GenerateResult{}, fmt.Errorf("client setup: %w", err)
}
```

The `httputil` package already exists but isn't used consistently. Some providers still have inline HTTP client creation.

---

#### A3. Streaming Implementation Pattern
**Location:** `*_client.go` files

`GenerateReplyStream` implementations share significant structure:
- Channel creation with buffer size
- Goroutine spawning pattern
- Error channel handling
- Chunk type conversions

**Recommendation:** Consider a base streaming handler that providers can customize.

---

### Category B: File Size / Complexity (Low Priority)

#### B1. Large Files Exceeding 800 Lines
| File | Lines | Concern |
|------|-------|---------|
| `admin/server.go` | 1284 | Multiple HTTP handlers |
| `service/chat.go` | 1260 | Core service + helpers |
| `provider/gemini/client.go` | 1209 | Complex API integration |

These files are coherent but could benefit from extraction:
- `admin/server.go` → extract handler groups (activity, debug, chat)
- `service/chat.go` → extract image generation, RAG formatting helpers
- `gemini/client.go` → extract tool building, response parsing

---

#### B2. Repository Methods Could Use Query Builder
**Location:** `internal/db/repository.go` (812 lines)

SQL queries use `fmt.Sprintf` for table names, which is safe but verbose:
```go
query := fmt.Sprintf(`
    SELECT id, user_id, provider, model...
    FROM %s
    WHERE id = $1
`, r.threadsTable())
```

A lightweight query builder or string constants could reduce repetition.

---

### Category C: Design Improvements (Low Priority)

#### C1. Provider Selection Logic
**Location:** `internal/service/chat.go`

The `selectProviderWithTenant` method combines multiple responsibilities:
1. Parse provider enum from request
2. Apply tenant defaults
3. Map to provider instance

Consider separating tenant resolution from provider instantiation.

---

#### C2. Constants Scattered Across Files
Various constants are defined locally:
- `maxHistoryChars = 50000` in both `gemini/client.go` and `anthropic/client.go`
- `ragSnippetMaxLen = 200` in `service/chat.go`

**Recommendation:** Centralize in `internal/constants` or within relevant packages.

---

#### C3. File Store Implementation Split
File store operations are split between:
- `service/files.go` - gRPC service handlers
- `provider/openai/filestore.go` - OpenAI vector store
- `provider/gemini/filestore.go` - Gemini file search store

This is logical but makes understanding the full flow require reading multiple files. Consider adding architectural documentation.

---

### Category D: Minor Improvements (Very Low Priority)

#### D1. Dashboard TypeScript Patterns
**Location:** `dashboard/src/components/ConversationPanel.tsx`

The React component includes:
- Class-based error boundary (modern pattern would use functional with hooks)
- Multiple inline interface definitions

This is functional but could be modernized in a future refactor.

---

#### D2. Hardcoded Tenant IDs
**Location:** `internal/db/repository.go:15-19`

```go
var ValidTenantIDs = map[string]bool{
    "ai8":      true,
    "email4ai": true,
    "zztest":   true,
}
```

These are hardcoded. Consider moving to configuration for easier management.

---

## What NOT to Change

The following patterns are correct and should be preserved:

1. **Provider Interface Design** - The current interface is well-balanced between flexibility and simplicity.

2. **Compat Layer Architecture** - The OpenAI-compatible abstraction is elegant and appropriate.

3. **gRPC + HTTP Hybrid** - The dual-protocol approach serves different use cases well.

4. **Error Sanitization** - The `internal/errors/sanitize.go` pattern is secure and should remain.

5. **Tenant Isolation** - The table-prefix approach is simple and effective for the current scale.

---

## Recommended Refactoring Priority

### High Value / Low Risk
1. Extract retry loop to generic helper (A1)
2. Add architectural documentation for file store flow

### Medium Value / Low Risk
3. Centralize shared constants (C2)
4. Extract admin server handlers into separate files (B1)

### Lower Priority
5. Modernize dashboard error boundaries
6. Add query builder for repository

---

## Metrics Summary

- **No TODOs/FIXMEs found** - Clean codebase
- **No security vulnerabilities identified** - Good SSRF, injection protection
- **Consistent code style** - Follows Go conventions
- **Good separation of concerns** - Clear package boundaries
- **Test coverage appears adequate** - 40 test files covering critical paths

---

## Conclusion

Airborne is a mature, well-engineered codebase. The score of **82/100** reflects:
- Excellent architectural decisions
- Minor duplication that could be reduced
- Some large files that remain coherent
- Strong security and error handling patterns

The identified refactoring opportunities are optimizations that would improve maintainability but are not blocking issues. The codebase is production-ready and demonstrates solid software engineering practices.

---

*Report generated by Claude:Opus 4.5*
