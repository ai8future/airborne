Date Created: 2026-02-15T23:45:00Z
TOTAL_SCORE: 74/100

# Airborne Refactoring Report

**Auditor**: Claude:Opus 4.6
**Codebase**: airborne (unified AI gateway / gRPC microservice)
**Language**: Go 1.25.5 with chassis-go v5 framework
**Scope**: ~100 non-test Go source files, 47 test files, 20+ provider integrations

---

## Executive Summary

Airborne is a well-architected Go microservice with clean hexagonal architecture, solid test coverage, and good use of the `compat` pattern to reduce provider boilerplate. The main refactoring opportunities center on three areas: (1) the monolithic `admin/server.go` file that mixes many HTTP handler concerns, (2) repeated patterns across the three tier-1 providers (OpenAI, Anthropic, Gemini) that could share more infrastructure, and (3) pervasive unchecked `json.Encode` calls in the admin layer. The codebase is production-grade and maintainable, with the issues identified being evolutionary improvements rather than urgent fixes.

---

## Scoring Breakdown

| Category | Score | Max | Notes |
|---|---|---|---|
| Architecture & Separation of Concerns | 15 | 20 | Good hexagonal layout; admin server is a monolith |
| Code Duplication | 12 | 20 | Compat pattern helps, but tier-1 providers still duplicate retry/timeout/validation logic |
| Code Quality & Consistency | 14 | 20 | Generally strong; unchecked json.Encode, magic numbers, brittle error matching |
| Test Coverage | 15 | 15 | 47 test files across 25+ packages — excellent |
| Maintainability & Readability | 10 | 15 | Some files >1000 lines; deeply nested functions |
| Type Safety & Error Handling | 8 | 10 | `map[string]interface{}` used heavily in admin; string-based error detection |
| **TOTAL** | **74** | **100** | |

---

## Detailed Findings

### 1. Admin Server Monolith (Impact: High)

**File**: `internal/admin/server.go` — **1,324 lines**

This single file handles HTTP routing, health checks, activity feeds, debug endpoints, thread viewing, test endpoints, chat relay, and file uploads. It imports 15 packages and has 38 instances of `json.NewEncoder(w).Encode()`.

**Refactoring Opportunity**: Split into domain-specific handler files:
- `admin/activity.go` — activity feed handlers
- `admin/health.go` — health check endpoints
- `admin/debug.go` — debug and thread inspection
- `admin/chat.go` — chat relay endpoint (lines 580–850)
- `admin/upload.go` — file upload handler (lines 880–1000)
- `admin/image.go` — image generation relay (lines 1050–1320)

**Benefit**: Each handler file would be 100–250 lines, dramatically improving navigability and making individual endpoint changes safer.

---

### 2. Unchecked JSON Encode Calls (Impact: High)

**File**: `internal/admin/server.go` — **38 instances**

Every `json.NewEncoder(w).Encode(...)` call ignores the returned error. While encoding errors writing to an HTTP response are rare, they can happen (client disconnect, broken pipe) and silently failing can mask issues in production logs.

**Example** (line 186):
```go
json.NewEncoder(w).Encode(map[string]interface{}{
    "activity": []interface{}{},
    "error":    "database not configured",
})
```

**Recommendation**: Create a helper function like `writeJSON(w, status, data)` that handles Content-Type, status code, and error logging in one place. This also addresses the DRY issue of repeatedly setting `Content-Type` headers before each encode.

---

### 3. Tier-1 Provider Duplication (Impact: Medium-High)

**Files**: `internal/provider/{anthropic,openai,gemini}/client.go` and `internal/provider/compat/openai_compat.go`

The four primary provider implementations repeat substantial logic:

| Pattern | Occurrences | Lines Each |
|---|---|---|
| `retry.EnsureTimeout(ctx, ...)` + `defer cancel()` | 8 (2 per file) | 2 |
| API key validation (`strings.TrimSpace(cfg.APIKey) == ""`) | 8 (2 per file) | 3 |
| `provider.SelectModel(cfg.Model, defaultModel, params.OverrideModel)` | 8 (2 per file) | 1 |
| HTTP client setup via `httputil.NewCapturedClientConfig` | 6 (2 per 3 files) | 8 |
| Retry loop with backoff (timeout check → retryable check → sleep) | 4 | ~50 |
| Streaming goroutine with channel + defer close | 3 | ~20 |

**Recommendation**: Extract a `provider.BaseRequest` helper that handles timeout setup, API key validation, model selection, and HTTP client configuration. The retry loop is the highest-value extraction target — a `retry.Do(ctx, func() error)` wrapper that encapsulates the attempt loop, timeout detection, retryable check, and backoff sleep would eliminate ~200 lines of duplicated logic.

---

### 4. Compat Provider Boilerplate (Impact: Medium)

**Files**: 13 provider packages (`deepseek`, `grok`, `mistral`, `cohere`, `fireworks`, `together`, `deepinfra`, `hyperbolic`, `nebius`, `upstage`, `cerebras`, `openrouter`, `perplexity`)

Each of these files is ~60 lines with identical structure:
```go
type Client struct { *compat.Client }
type ClientOption func(*clientOptions)
type clientOptions struct { debug bool }
func WithDebugLogging(enabled bool) ClientOption { ... }
func NewClient(opts ...ClientOption) *Client { ... }
```

The only differences are: provider name, base URL, default model, and feature flags.

**Recommendation**: Consider a registry-based approach:
```go
var DeepSeek = compat.Register(compat.ProviderConfig{
    Name: "deepseek", DefaultBaseURL: "https://api.deepseek.com/v1", ...
})
```

This would replace 13 × 60 = ~780 lines with a single registry file of ~130 lines. However, the current approach is also valid — it's explicit, each provider is independently testable, and adding a new provider is a copy-paste-modify operation. This is a judgment call based on how frequently new providers are added.

---

### 5. `map[string]interface{}` Overuse in Admin (Impact: Medium)

**File**: `internal/admin/server.go` — **16+ instances**

Admin handlers construct responses using untyped maps:
```go
json.NewEncoder(w).Encode(map[string]interface{}{
    "activity": activity,
    "error":    err.Error(),
})
```

**Issue**: No compile-time checking of field names, no documentation of response shape, easy to introduce typos. The `TestResponse`, `ChatResponse`, and `UploadResponse` structs (defined later in the same file) demonstrate the better approach — but the activity, health, debug, and thread endpoints don't follow this pattern.

**Recommendation**: Define response structs for the remaining endpoints (`ActivityResponse`, `HealthResponse`, `DebugResponse`, `ThreadResponse`). This improves IDE support, catches typos at compile time, and serves as API documentation.

---

### 6. Brittle String-Based Error Detection (Impact: Medium)

**File**: `internal/admin/server.go:344`

```go
if strings.Contains(err.Error(), "not found") {
```

This is fragile — any change to the error message in the `db` package would silently break this detection. Additionally, it matches any error that happens to contain "not found" in its text.

**Recommendation**: Define a sentinel error or error type in the `db` package:
```go
var ErrNotFound = errors.New("not found")
// Then check with: errors.Is(err, db.ErrNotFound)
```

---

### 7. Magic Numbers (Impact: Low-Medium)

**File**: `internal/admin/server.go`

| Value | Line | Meaning |
|---|---|---|
| `50` | 173 | Default activity limit |
| `200` | 175 | Max activity limit |
| `5 * time.Second` | 194, 265, 336, 403 | Query timeout |
| `2 * 1024 * 1024` | 115 | 2MB body limit |

**File**: `cmd/airborne/main.go`

| Value | Line | Meaning |
|---|---|---|
| `30 * time.Second` | 131 | Middleware timeout |
| `5 * time.Minute` | 142 | Write timeout |
| `5 * time.Second` | 158 | Admin shutdown timeout |
| `3 * time.Second` | 193 | Health check timeout |

**Recommendation**: Group these as named constants at the top of each file. The admin server timeouts in particular would benefit from a single `const adminQueryTimeout = 5 * time.Second` rather than repeating the literal 4 times.

---

### 8. Large Functions (Impact: Low-Medium)

**`internal/service/chat.go:prepareRequest()`** — ~120 lines
Coordinates: input validation, command parsing, provider selection, config building, RAG context retrieval, and parameter conversion. This function has high cyclomatic complexity due to multiple branching paths.

**Recommendation**: Extract into focused sub-functions: `parseCommands()`, `selectProvider()`, `buildRAGContext()`, `buildGenerateParams()`. Each would be 20–30 lines and independently testable.

**`internal/admin/server.go:handleActivity()`** — ~88 lines
**`internal/db/repository.go`** — 812 lines with several 50+ line methods

These are not critical but would benefit from incremental decomposition as the codebase evolves.

---

### 9. Test Coverage Gaps (Impact: Low)

The project has excellent test coverage (47 test files across 25+ packages). Notable areas **with** tests:
- Auth interceptors, rate limiting, key validation
- Tenant config, secrets, manager
- Provider implementations (OpenAI, Anthropic, Gemini, Mistral, Compat)
- RAG service, chunker, vectorstore, embedder, extractor
- Retry logic, HTTP capture, validation, pricing

Notable areas **without** tests:
- `internal/admin/server.go` — No unit tests for the 1,324-line admin server (only `internal/service/admin_test.go` exists for the gRPC admin service, not the HTTP admin)
- `internal/cli/` — CLI client and output formatting untested
- `internal/provider/gemini/filestore.go` and `internal/provider/openai/filestore.go` — File store operations untested

**Note**: The admin server being untested is directly related to finding #1 — splitting it into smaller files would make it far more testable.

---

### 10. Provider Feature Support Method Pattern (Impact: Low)

The four primary providers all implement:
```go
func (c *Client) Name() string { ... }
func (c *Client) SupportsFileSearch() bool { ... }
func (c *Client) SupportsWebSearch() bool { ... }
func (c *Client) SupportsNativeContinuity() bool { ... }
func (c *Client) SupportsStreaming() bool { ... }
```

The `compat.Client` already handles this via `ProviderConfig` fields. For the tier-1 providers (Anthropic, OpenAI, Gemini), a similar config-driven approach could eliminate these 5-method blocks, though this is minor since each block is only ~20 lines.

---

## Strengths Worth Preserving

1. **Hexagonal architecture** — Clean separation between `cmd/`, `internal/`, `api/`, `gen/`
2. **The `compat` package** — Excellent abstraction for OpenAI-compatible providers, reducing 13 provider implementations to thin wrappers
3. **Comprehensive test suite** — 47 test files with unit tests, mock infrastructure (miniredis), and test utilities
4. **chassis-go integration** — Leveraging a shared framework for logging, gRPC, lifecycle, CORS, and health checks
5. **Retry package** — Well-designed retry/backoff logic with clear constants and good test coverage
6. **No TODO/FIXME/HACK comments** — The codebase is free of acknowledged technical debt markers
7. **Consistent package documentation** — Every package has a doc comment
8. **Validation package** — Centralized input validation with clear limits and URL checking

---

## Priority Recommendations

| Priority | Item | Effort | Impact |
|---|---|---|---|
| **P1** | Extract `writeJSON` helper for admin server | Small | Fixes 38 unchecked error sites + DRY |
| **P1** | Define `db.ErrNotFound` sentinel error | Small | Replaces brittle string matching |
| **P2** | Split `admin/server.go` into domain files | Medium | Improves navigability, testability |
| **P2** | Extract retry loop into `retry.Do()` wrapper | Medium | Eliminates ~200 lines of duplication across 4 providers |
| **P3** | Define typed response structs for admin endpoints | Small | Compile-time safety, API documentation |
| **P3** | Extract `prepareRequest()` into sub-functions | Small | Reduces complexity, improves testability |
| **P3** | Named constants for magic numbers | Small | Self-documenting code |
| **P4** | Consider provider registry for compat providers | Medium | Reduces ~780 lines to ~130, but trades explicitness |
| **P4** | Add unit tests for admin HTTP handlers | Medium | Coverage for largest untested file |
