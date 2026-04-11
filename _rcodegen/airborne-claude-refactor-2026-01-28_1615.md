Date Created: 2026-01-28 16:15:00 UTC
TOTAL_SCORE: 72/100

# Airborne Codebase Refactoring Analysis Report

## Executive Summary

Airborne is a well-architected Go-based AI gateway service supporting 18+ LLM providers. The codebase demonstrates solid engineering practices but has significant opportunities for improvement in code duplication, function complexity, and test coverage. This report identifies refactoring opportunities to improve maintainability without changing functionality.

---

## Scoring Breakdown

| Category | Score | Max | Notes |
|----------|-------|-----|-------|
| Code Duplication | 12 | 20 | Significant duplication across providers (~1,500+ lines) |
| Function Complexity | 14 | 20 | Multiple 200+ line functions, deep nesting |
| Error Handling | 16 | 20 | Generally good, one critical ignored error |
| Test Coverage | 14 | 20 | Strong in service layer, gaps in DB and providers |
| Code Organization | 16 | 20 | Clean structure, some mixed concerns |
| **TOTAL** | **72** | **100** | |

---

## 1. Code Duplication Issues

### 1.1 OpenAI-Compatible Provider Boilerplate (CRITICAL - 13 files)

**Impact: HIGH** | **Duplicated Lines: ~900+**

All 13 OpenAI-compatible providers contain identical boilerplate code:

**Affected Files:**
- `internal/provider/cerebras/client.go`
- `internal/provider/cohere/client.go`
- `internal/provider/deepinfra/client.go`
- `internal/provider/deepseek/client.go`
- `internal/provider/fireworks/client.go`
- `internal/provider/grok/client.go`
- `internal/provider/hyperbolic/client.go`
- `internal/provider/mistral/client.go`
- `internal/provider/nebius/client.go`
- `internal/provider/openrouter/client.go`
- `internal/provider/perplexity/client.go`
- `internal/provider/together/client.go`
- `internal/provider/upstage/client.go`

**Duplicated Pattern (lines 20-63 in each):**
```go
type clientOptions struct {
    debug bool
}

type ClientOption func(*clientOptions)

func WithDebugLogging(enabled bool) ClientOption {
    return func(opts *clientOptions) {
        opts.debug = enabled
    }
}

func NewClient(opts ...ClientOption) *Client {
    clientOpts := &clientOptions{}
    for _, opt := range opts {
        if opt != nil {
            opt(clientOpts)
        }
    }
    // ... provider-specific config ...
}
```

**Recommended Refactoring:**
Create a factory function in the `compat` package:
```go
func NewOpenAICompatClient(config ProviderConfig, opts ...ClientOption) *Client
```

This would reduce each provider to ~10 lines of configuration instead of 63.

---

### 1.2 Request Timeout Initialization (4 files)

**Impact: MEDIUM** | **Duplicated Lines: ~24**

**Affected Files:**
- `internal/provider/anthropic/client.go:108-113, 300-304`
- `internal/provider/openai/client.go:86-91, 330-334`
- `internal/provider/gemini/client.go:78-83, 369-373`
- `internal/provider/compat/openai_compat.go:101-106, 245-249`

**Pattern:**
```go
if _, hasDeadline := ctx.Deadline(); !hasDeadline {
    var cancel context.CancelFunc
    ctx, cancel = context.WithTimeout(ctx, timeout)
    defer cancel()
}
```

**Recommendation:** Extract to `internal/retry/context.go`:
```go
func EnsureTimeout(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc)
```

---

### 1.3 Model Selection Logic (4 files)

**Impact: LOW** | **Duplicated Lines: ~24**

**Pattern (lines 86-92 in anthropic, similar in others):**
```go
model := cfg.Model
if model == "" {
    model = defaultModel
}
if strings.TrimSpace(params.OverrideModel) != "" {
    model = params.OverrideModel
}
```

**Recommendation:** Add utility in `internal/provider/`:
```go
func SelectModel(configModel, defaultModel, overrideModel string) string
```

---

### 1.4 Retry Loop Pattern (4 files)

**Impact: HIGH** | **Duplicated Lines: ~230**

Each major provider has ~58 lines of nearly identical retry logic with exponential backoff.

**Affected:**
- `internal/provider/anthropic/client.go:182-240`
- `internal/provider/openai/client.go:234-269`
- `internal/provider/gemini/client.go:256-291`
- `internal/provider/compat/openai_compat.go:169-205`

**Recommendation:** Create a generic retry wrapper:
```go
func WithRetry[T any](ctx context.Context, provider string, fn func() (T, error)) (T, error)
```

---

### 1.5 SQL Query Duplication in Repository

**Impact: MEDIUM** | **Duplicated Lines: ~75**

**File:** `internal/db/repository.go:336-467`

The `GetActivityFeedAllTenants` function contains 3 nearly identical UNION blocks for different tenants:
- Lines 337-361: ai8 tenant
- Lines 363-388: email4ai tenant
- Lines 390-415: zztest tenant

**Recommendation:** Generate SQL dynamically from tenant list or use a query builder.

---

## 2. Function Complexity Issues

### 2.1 Excessively Long Functions (>200 lines)

| File | Function | Lines | Concern |
|------|----------|-------|---------|
| `gemini/client.go` | `GenerateReplyStream` | 323 | Streaming, state, response parsing combined |
| `gemini/client.go` | `GenerateReply` | 291 | Request building, API call, parsing combined |
| `openai/client.go` | `GenerateReplyStream` | 262 | Similar issues |
| `openai/client.go` | `GenerateReply` | 243 | Similar issues |

**Recommendation:** Split into:
- `buildRequest()` - prepare API request
- `executeWithRetry()` - API call with retry logic
- `parseResponse()` - extract response data
- `buildResult()` - construct provider.GenerateResult

### 2.2 Functions with Excessive Parameters

| File | Function | Parameters |
|------|----------|------------|
| `db/repository.go:502` | `PersistConversationTurnWithDebug` | **15 parameters** |
| `db/repository.go:497` | `PersistConversationTurn` | **12 parameters** |

**Recommendation:** Introduce parameter objects:
```go
type ConversationTurnParams struct {
    ThreadID        uuid.UUID
    UserID          string
    UserContent     string
    AssistantContent string
    Provider        string
    Model           string
    // ...
}
```

### 2.3 Deep Nesting (6+ levels)

**Files with problematic nesting:**
- `internal/service/chat.go:433-548` - GenerateReplyStream switch statement
- `internal/provider/gemini/client.go` - GenerateReply nested conditionals

**Recommendation:** Extract nested blocks into separate functions with descriptive names.

---

## 3. Error Handling Issues

### 3.1 CRITICAL: Ignored Error in Stream Processing

**File:** `internal/provider/anthropic/client.go:397`

```go
_ = message.Accumulate(event)  // ERROR IGNORED
```

**Risk:** Stream event accumulation errors are silently dropped. This could result in:
- Incomplete token counts
- Missing usage information
- Lost streaming data

**Recommendation:**
```go
if err := message.Accumulate(event); err != nil {
    slog.Warn("failed to accumulate stream event", "error", err)
}
```

### 3.2 Silent Error Suppression

**File:** `internal/db/repository.go:562-566`

```go
citationsJSON, err := CitationsToJSON(citations)
if err != nil {
    slog.Warn("failed to serialize citations", "error", err)
    // Continues without citations
}
```

**Issue:** Citation data may be silently dropped without caller awareness.

### 3.3 Inconsistent Log Levels

**File:** `internal/auth/ratelimit.go:166-180`

Uses `slog.Warn()` but then returns an error. Should use `slog.Error()` for error conditions.

### 3.4 Positive Patterns (Maintain These)

- Proper error wrapping with `%w` format throughout database layer
- Custom error types in `internal/auth/errors.go` used consistently
- Error sanitization via `internal/errors/sanitize.go` protects clients
- Proper deferred resource cleanup patterns

---

## 4. Test Coverage Gaps

### 4.1 Overall Coverage

| Metric | Value |
|--------|-------|
| Total Test Functions | 397 |
| Total Test Lines | 11,276 |
| Production Code Lines | ~16,863 |
| Test-to-Code Ratio | 67% |

### 4.2 Critical Gaps

#### Database Layer (CRITICAL - 0% coverage)

**Untested Files:**
- `internal/db/repository.go` (812 lines) - **CRITICAL**
- `internal/db/postgres.go` (connection management)
- `internal/db/models.go` (data structures)

**Risk:** All conversation persistence code is untested.

#### Provider Implementations (12/18 untested)

**Providers WITHOUT tests:**
1. cerebras
2. cohere
3. deepinfra
4. deepseek
5. fireworks
6. grok
7. hyperbolic
8. nebius
9. openrouter
10. perplexity
11. together
12. upstage

**Context:** These use the compat wrapper, but have zero direct tests.

### 4.3 Test Helper Consolidation Needed

Multiple test files define similar mocks inline:
- `mockProvider` - defined in chat_test.go
- `mockRedisClient` - defined in admin_test.go
- `mockServerStream` - defined in multiple files

**Recommendation:** Create `internal/testutil/` package for shared mocks.

---

## 5. Code Organization Issues

### 5.1 Mixed Concerns in Key Functions

**`persistConversation` (chat.go:1021-1170)** handles:
- Tenant validation
- Thread creation/management
- Citation JSON serialization
- Pricing calculations
- Database persistence
- Error handling
- Logging

**Recommendation:** Split into:
- `validateTenant()`
- `calculatePricing()`
- `persistToDatabase()`

### 5.2 God Objects

**ChatService** (`internal/service/chat.go:37-48`) has 8 dependencies:
- 3 providers (openai, gemini, anthropic)
- Rate limiter
- RAG service
- Image generation client
- Database client
- Config builder

**Recommendation:** Consider splitting by concern:
- `ProviderRouter` - provider selection and failover
- `PersistenceService` - database operations
- `EnrichmentService` - RAG, image generation

---

## 6. Architectural Recommendations

### 6.1 High Priority

1. **Extract Provider Factory** - Eliminate 900+ lines of duplicated boilerplate
2. **Add Database Tests** - Critical persistence layer has zero coverage
3. **Fix Ignored Error** - Stream accumulation errors must be handled

### 6.2 Medium Priority

4. **Create Parameter Objects** - Replace 12-15 parameter functions
5. **Extract Retry Logic** - Create generic retry wrapper
6. **Consolidate Test Helpers** - Create shared testutil package

### 6.3 Low Priority

7. **Split Large Functions** - Break down 200+ line functions
8. **Add Provider Tests** - At minimum, test NewClient() for all providers
9. **Reduce Nesting** - Extract deeply nested blocks

---

## 7. Positive Observations

The codebase demonstrates several strong practices:

1. **Clean Package Structure** - Clear separation by domain (auth, db, provider, service)
2. **Consistent Error Wrapping** - Uses `fmt.Errorf("context: %w", err)` pattern
3. **Good Retry Implementation** - `internal/retry/` package with configurable backoff
4. **Provider Abstraction** - Clean interface allows easy provider additions
5. **Error Sanitization** - Protects clients from internal error details
6. **Structured Logging** - Consistent use of `slog` throughout
7. **Context Propagation** - Proper context handling for timeouts and cancellation
8. **Graceful Degradation** - Non-critical failures (RAG) don't block main flow

---

## Summary

**Overall Grade: 72/100 (Good with improvement opportunities)**

The Airborne codebase is well-structured and maintainable, but would benefit from:

1. **Reducing duplication** in provider implementations (~1,500 lines could become ~300)
2. **Adding database tests** to cover critical persistence logic
3. **Splitting complex functions** to improve readability and testability
4. **Fixing the ignored stream error** to prevent silent data loss

The architecture is sound and the code follows Go best practices. The identified refactoring opportunities would reduce maintenance burden and improve code quality without requiring architectural changes.
