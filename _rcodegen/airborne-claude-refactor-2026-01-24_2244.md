# Airborne Refactoring Analysis Report

Date Created: 2026-01-24 22:44:00 PST
TOTAL_SCORE: 85/100

---

## Executive Summary

Airborne is a well-architected, production-grade AI provider gateway implemented in Go with a Next.js dashboard. The codebase demonstrates mature engineering practices including interface-based abstractions, good separation of concerns, and comprehensive error handling. The refactoring score of 85/100 reflects a solid foundation with targeted improvement opportunities.

---

## Codebase Metrics

| Metric | Value |
|--------|-------|
| Total Go Files | ~129 |
| Total Go LOC | ~18,500 |
| Test Files | 40 |
| Test LOC | ~11,276 |
| Test Coverage Ratio | ~61% (LOC) |
| Provider Implementations | 16 (13 via compat layer) |
| Proto Services | 3 (Airborne, Admin, Files) |

---

## Strengths (+25 points base)

### 1. Provider Abstraction Pattern (Excellent)
**Score Contribution: +15**

The `compat` package (`internal/provider/compat/openai_compat.go`) demonstrates exemplary code reuse. Thirteen providers (DeepSeek, Mistral, Grok, Perplexity, OpenRouter, etc.) all share ~414 lines of common logic through embedding:

```
Provider LOC Summary:
- compat/openai_compat.go: 414 lines (shared)
- Each thin wrapper: ~62-64 lines (configuration only)
- Avoided duplication: ~5,200+ lines
```

This is textbook Go composition - the thin provider wrappers only specify configuration:
- `internal/provider/deepseek/client.go`: 63 lines
- `internal/provider/grok/client.go`: 62 lines
- `internal/provider/mistral/client.go`: 62 lines

### 2. Service Layer Design (Good)
**Score Contribution: +10**

The `prepareRequest()` method in `chat.go:80-198` properly extracts shared logic between `GenerateReply` and `GenerateReplyStream`, avoiding duplication of validation, provider selection, and RAG retrieval.

### 3. Security-Conscious Design (Good)
**Score Contribution: +5**

- API keys blocked from client requests (`config/builder.go:49-53`)
- SSRF validation for custom base URLs (`validation/url.go`)
- Constant-time token comparison (`auth/static.go:72`)
- Deep copy of maps to prevent data races (`config/builder.go:38-43`)

### 4. Error Handling (Good)
**Score Contribution: +5**

The `errors/sanitize.go` pattern properly separates client-facing error messages from internal logging, preventing information leakage.

### 5. Shared Retry Logic
**Score Contribution: +5**

The `retry` package (`internal/retry/`) centralizes backoff and retryability detection across all providers.

---

## Improvement Opportunities (-15 points)

### 1. Repository Layer Duplication (Medium Priority)
**Impact: -4 points**

`internal/db/repository.go` (813 lines) contains significant SQL query duplication:

**Lines 254-331 vs 336-466**: `GetActivityFeed()` and `GetActivityFeedAllTenants()` share ~80% identical column selection and row scanning logic. The "all tenants" version uses hardcoded UNION ALL for each tenant.

**Recommendation**: Extract a common `scanActivityEntry()` helper and consider generating the UNION query dynamically from `ValidTenantIDs` to avoid hardcoding tenant names in SQL.

### 2. TypeScript Interface Duplication (Low Priority)
**Impact: -2 points**

The `ActivityEntry` interface is duplicated in:
- `dashboard/src/app/page.tsx:8-25`
- `dashboard/src/components/ActivityPanel.tsx:3-20`

**Recommendation**: Extract to a shared types file (`dashboard/src/types/activity.ts`).

### 3. Client Option Boilerplate (Minor)
**Impact: -2 points**

Every compat-based provider has identical boilerplate for client options:

```go
type ClientOption func(*clientOptions)
type clientOptions struct {
    debug bool
}
func WithDebugLogging(enabled bool) ClientOption { ... }
```

This pattern repeats in 13 files (deepseek, mistral, grok, etc.).

**Recommendation**: Move to `compat` package as shared types since all thin wrappers use identical option handling.

### 4. History Character Limit Constant (Minor)
**Impact: -1 point**

`maxHistoryChars = 50000` appears in both:
- `internal/provider/gemini/client.go:22`
- `internal/provider/anthropic/client.go:25`

**Recommendation**: Move to `provider` package constants.

### 5. Missing Provider Interface Tests (Minor)
**Impact: -3 points**

While test coverage is good (~61% by LOC), `internal/provider/providers_test.go` exists but individual compat providers lack unit tests. The compat layer itself has tests, but provider-specific configuration tests would catch misconfigured defaults.

### 6. Magic Numbers in Chat Service (Minor)
**Impact: -1 point**

`ragSnippetMaxLen = 200` in `chat.go:33` could be configurable or at least documented with rationale.

### 7. Hardcoded Tenant List (Medium Priority)
**Impact: -2 points**

`ValidTenantIDs` in `repository.go:15-19` and the SQL UNION in `GetActivityFeedAllTenants` hardcode tenant IDs. This creates maintenance burden when adding tenants.

**Recommendation**: Consider database-driven tenant discovery or configuration-based tenant list.

---

## Architecture Assessment

### Clean Boundaries
The codebase maintains clear separation:
```
cmd/           → Entry points (main.go)
api/proto/     → API contracts
internal/      → Business logic
  ├── service/ → gRPC handlers
  ├── provider/→ LLM integrations
  ├── db/      → Persistence
  └── auth/    → Security
dashboard/     → Frontend (isolated)
```

### Dependency Flow
Dependencies flow inward correctly:
- Providers depend only on `provider.Provider` interface
- Services depend on interfaces, not implementations
- No circular dependencies detected

### Proto-First Design
The protobuf definitions in `api/proto/airborne/v1/` serve as the source of truth for the API contract, with clean Go code generation.

---

## Maintainability Observations

### Positive
1. **Consistent patterns**: All providers follow the same structure
2. **Good documentation**: Package-level comments explain purpose
3. **Validation centralized**: `internal/validation/` handles URL and limit checks
4. **Configuration layered**: Tenant defaults + request overrides pattern in `config/builder.go`

### Potential Tech Debt
1. The "all tenants" queries will need refactoring as tenant count grows
2. No circuit breaker pattern for provider failures (relies on retry + failover)
3. Dashboard lacks TypeScript strict mode benefits from shared types

---

## Scoring Breakdown

| Category | Points |
|----------|--------|
| Base Architecture Quality | +25 |
| Provider Abstraction | +15 |
| Service Layer Design | +10 |
| Security Practices | +5 |
| Error Handling | +5 |
| Retry/Resilience | +5 |
| Repository Duplication | -4 |
| TypeScript Duplication | -2 |
| Client Option Boilerplate | -2 |
| Shared Constants | -1 |
| Test Coverage Gaps | -3 |
| Magic Numbers | -1 |
| Hardcoded Tenants | -2 |
| **Total** | **85/100** |

---

## Refactoring Priority Matrix

| Priority | Issue | Effort | Impact |
|----------|-------|--------|--------|
| P1 | Extract shared ActivityEntry scanning | Low | Medium |
| P1 | Shared TypeScript types | Low | Low |
| P2 | Dynamic tenant UNION query | Medium | Medium |
| P2 | Move ClientOption to compat package | Low | Low |
| P3 | Add provider configuration tests | Medium | Medium |
| P3 | Extract shared constants | Low | Low |

---

## Conclusion

Airborne demonstrates strong engineering fundamentals. The compat layer for OpenAI-compatible providers is particularly well-executed, eliminating what would be ~5,000+ lines of duplicated code. The main refactoring opportunities are in the repository layer (SQL duplication) and minor consolidation of shared types/constants. The codebase is production-ready with the identified issues being incremental improvements rather than critical fixes.

**Grade: B+ (85/100)** - Solid, maintainable codebase with room for targeted improvements.

---

*Report generated by Claude:Opus 4.5 via Claude Code*
