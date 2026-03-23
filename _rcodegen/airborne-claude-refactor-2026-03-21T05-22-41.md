Date Created: 2026-03-21T05:22:41Z
TOTAL_SCORE: 72/100

# Airborne Refactoring Audit Report

**Agent:** Claude Code:Opus 4.6
**Codebase:** airborne (multi-provider AI gateway)
**Version:** 1.8.11
**Language:** Go 1.25 | ~18,972 LoC (internal/) | ~150 Go files | 47 test files

---

## Score Breakdown

| Category | Weight | Score | Notes |
|----------|--------|-------|-------|
| Architecture & Structure | 20% | 17/20 | Clean package boundaries, good separation; dinged for hardcoded tenant IDs |
| Code Duplication | 20% | 11/20 | ~810 lines of pure boilerplate in 13 compat provider wrappers; SQL UNION triplication |
| Error Handling | 15% | 10/15 | Good `%w` wrapping in most code; silent failures in config loading and RAG pipeline |
| Security | 10% | 9/10 | Strong: SSRF, bcrypt, rate limiting, constant-time auth. Minor: unbounded reads in extractors |
| Test Coverage | 10% | 8/10 | 47 test files across most packages; compat providers untested |
| Maintainability | 10% | 6/10 | Magic numbers scattered, 3 duplicate RateLimits types, inconsistent env var patterns |
| Interface Design | 10% | 8/10 | Clean Provider/Embedder/Extractor/Store interfaces; false capability flags on 2 providers |
| Documentation | 5% | 3/5 | Excellent README; inline docs sparse in some packages |

---

## Critical Findings

### 1. Provider Boilerplate Duplication (~810 wasted lines) - HIGH

**Files:** `internal/provider/{deepseek,grok,mistral,perplexity,together,fireworks,openrouter,deepinfra,cohere,hyperbolic,nebius,cerebras,upstage}/client.go`

All 13 OpenAI-compatible provider wrappers are 62-63 lines each with identical structure:

```go
type ClientOption func(*clientOptions)
type clientOptions struct { debug bool }
func WithDebugLogging(enabled bool) ClientOption { ... }
func NewClient(apiKey string, opts ...ClientOption) *Client { ... }
```

Every wrapper does the same thing: define options, parse them, build a `compat.ProviderConfig`, and delegate to `compat.NewClient()`. The only differences are 3-4 constants (`defaultBaseURL`, `defaultModel`, `APIKeyEnvVar`) and occasional boolean flags (`SupportsWebSearch`).

**Recommendation:** Create a generic factory in `compat/factory.go` that accepts a config struct and returns a `*compat.Client`. Replace 13 packages with a single provider registry table. This would eliminate ~810 lines of copy-paste code.

---

### 2. Hardcoded Tenant IDs in SQL UNION Queries - HIGH

**File:** `internal/db/repository.go` lines 336-419

`GetActivityFeedAllTenants()` contains three nearly identical SQL queries glued together with `UNION ALL`, each hardcoded for a specific tenant ID (`ai8`, `email4ai`, `zztest`). Adding a tenant requires editing this function in three places.

**Also at:** `internal/db/repository.go` lines 15-19 (ValidTenantIDs map), plus hardcoded references at lines 697, 801.

**Recommendation:** Build the UNION query dynamically by iterating over the ValidTenantIDs map. Better yet, move tenant IDs to configuration so adding a tenant doesn't require code changes.

---

### 3. False Capability Claims on Compat Providers - MEDIUM

**Files:** `internal/provider/perplexity/client.go`, `internal/provider/cohere/client.go`

Both set `SupportsWebSearch: true` in their `ProviderConfig`, but `compat/openai_compat.go` has zero implementation for web search. The flag is a lie - no caller can activate web search through the compat layer.

**Recommendation:** Either implement web search in the compat layer or remove the false flags.

---

### 4. Compat Providers Are Dead Code From Service Layer - MEDIUM

**File:** `internal/service/chat.go` lines 627-671

`selectProviderWithTenant()` only handles OpenAI, Gemini, and Anthropic. None of the 13 compat providers can be selected. These providers exist in the codebase but are unreachable from the service layer.

**Recommendation:** Implement a provider registry pattern so compat providers can actually be used, or document them as placeholder/future code.

---

## Moderate Findings

### 5. God Functions

| Function | File | Lines | Concern Count |
|----------|------|-------|---------------|
| `prepareRequest()` | service/chat.go:78-199 | 122 | 10+ (validation, security, commands, RAG, provider selection, params) |
| `GetActivityFeedAllTenants()` | db/repository.go:336-467 | 132 | Hardcoded SQL x3 |
| `applyEnvOverrides()` | config/config.go:264-343 | 80 | 20+ config items handled repetitively |

**Recommendation:** Split `prepareRequest()` into `validateAndSecure()`, `parseCommands()`, `selectProvider()`, `buildParams()`. Build SQL dynamically. Use config struct tags or a map for env overrides.

---

### 6. Three Duplicate RateLimits Type Definitions

**Files:**
- `internal/auth/keys.go` - `RateLimits` struct
- `internal/config/config.go` - `RateLimitConfig` struct
- `internal/tenant/config.go` - `RateLimitConfig` struct

Three separate type definitions for the same concept (RPM, RPD, TPM).

**Recommendation:** Single definition in `internal/auth/` or a shared types package, imported everywhere.

---

### 7. Token Extraction Duplication

**Files:** `internal/auth/interceptor.go` (`extractAPIKey`) and `internal/auth/static.go` (`extractStaticToken`)

Nearly identical logic: check `authorization` header, strip "Bearer " prefix, fall back to `x-api-key` header.

**Recommendation:** Extract to a shared `extractToken()` in `internal/auth/keys.go`.

---

### 8. HTTP Client Configuration Duplication

**Files:** `internal/rag/embedder/ollama.go`, `internal/rag/extractor/docbox.go`, `internal/rag/vectorstore/qdrant.go`, `internal/imagegen/client.go`

Four components repeat the same HTTP client construction pattern with `call.New(WithTimeout, WithRetry, WithCircuitBreaker)`. Each has slightly different but undocumented timeout/retry values:

| Component | Timeout | Retries | Retry Delay | Circuit Breaker |
|-----------|---------|---------|-------------|-----------------|
| Ollama | 30s | 3 | 500ms | 5/30s |
| Docbox | 120s | 3 | 500ms | 5/30s |
| Qdrant | 30s | 3 | 500ms | 5/30s |
| Gemini Image | 90s | 2 | 1s | 3/60s |

**Recommendation:** Extract to a factory function with documented defaults. Docbox's 4x timeout and Gemini's different retry config should be explicitly justified.

---

### 9. Silent Configuration Failures

**File:** `internal/config/config.go` lines 286-304

DATABASE_URL and CA certificate loading failures are logged to stderr via `fmt.Fprintf` and not propagated as errors. The server can start with a broken database configuration.

**File:** `internal/db/repository.go` line 564-565

Citation serialization failures are logged as warnings and silently dropped, causing data loss.

**Recommendation:** Return errors for critical config issues. For citations, decide explicitly: fail the persist or document the data loss as acceptable.

---

### 10. Magic Numbers

Scattered across the codebase without named constants:

| Value | Location | Meaning |
|-------|----------|---------|
| 64 | tenant/loader.go:100 | Max tenant ID length |
| 128000 | tenant/loader.go:125 | Max output tokens |
| 2.0 | tenant/loader.go:138 | Max temperature |
| 2000 | rag/chunker/chunker.go:39 | Default chunk size |
| 200 | rag/chunker/chunker.go:40 | Default chunk overlap |
| 3000 | rag/extractor/docbox.go:192 | Chars-per-page estimate |
| 128 | rag/service.go:17 | Max collection name length |
| 50MB | imagegen/gemini.go:24 | Max response size |
| 85 | imagegen/gemini.go:23 | JPEG quality |
| 125 | imagegen/client.go:102 | Alt text truncation |

**Recommendation:** Define named constants with brief rationale comments.

---

### 11. Inconsistent Env Var Resolution

Three different patterns used across the codebase:
1. `envutil.GetStringEnv()` / `GetIntEnv()` / `GetBoolEnv()` (internal/config/envutil)
2. Direct `os.Getenv()` with manual parsing (config.go, tenant/env.go)
3. Custom strconv.Atoi with error handling (tenant/env.go)

**Recommendation:** Consolidate to `envutil` everywhere.

---

### 12. Mixed Logging Approaches

Early startup code in `internal/config/config.go` and `internal/tenant/manager.go` uses `fmt.Fprintf(os.Stderr, ...)` while the rest of the codebase properly uses `slog`.

**Recommendation:** Use `slog` consistently, even at startup. If the logger isn't initialized yet, defer the messages or initialize slog earlier.

---

## Minor Findings

### 13. Deprecated Function Still Present
`internal/db/repository.go:34-36` - `NewRepository()` creates a repository with empty `tablePrefix`, routing queries to legacy table names. Marked deprecated but still callable.

### 14. Native Provider Parameter Duplication
All three native providers (OpenAI, Anthropic, Gemini) duplicate Temperature/TopP/MaxOutputTokens parameter-building logic. Could be extracted to a shared helper.

### 15. Unbounded Reads in Extractors
`internal/rag/extractor/docbox.go` uses `io.ReadAll()` without size limits. Only `internal/imagegen/gemini.go` uses `io.LimitReader`. A large uploaded file could exhaust memory.

### 16. Global Mutable State in markdownsvc
`internal/markdownsvc/client.go` uses package-level variables with `sync.RWMutex`. While correct, dependency injection would be more testable and less fragile.

### 17. Doppler Default Config Duplicated
`config = "prod"` hardcoded in both `internal/config/config.go:412` and `internal/tenant/doppler.go:41`.

### 18. Default Ports/Dirs Scattered
Port 50051 appears in `internal/config/config.go` (lines 201, 218) and `internal/tenant/env.go:42`. The `configs` directory is hardcoded in three places.

---

## Positive Observations

- **Clean architecture:** Well-defined package boundaries with minimal circular dependencies
- **Strong security posture:** SSRF protection, bcrypt key hashing, atomic Lua-based rate limiting, constant-time token comparison, non-root Docker, TLS support
- **Good interface design:** Provider, Embedder, Extractor, and Store interfaces are clean and composable
- **Proper concurrency:** RWMutex and atomic operations used correctly throughout
- **Solid test coverage:** 47 test files covering auth, config, tenant, providers, RAG, validation, and more
- **Error wrapping:** Consistent use of `fmt.Errorf("...: %w", err)` in most packages
- **Resource cleanup:** Proper use of `defer` for closing readers, responses, and connections
- **Comprehensive README:** Excellent documentation of architecture, API, configuration, and deployment
- **Compat layer design:** The shared OpenAI-compatible client layer is a good abstraction - the problem is the 13 unnecessary wrapper packages around it

---

## Refactoring Priority Matrix

| Priority | Issue | Effort | Impact |
|----------|-------|--------|--------|
| 1 | Eliminate 13 compat provider wrappers via factory | Medium | Removes ~810 lines, prevents future copy-paste |
| 2 | Dynamic tenant query building | Low | Fixes maintainability, enables config-driven tenants |
| 3 | Split god functions | Medium | Improves readability and testability |
| 4 | Consolidate RateLimits types | Low | Single source of truth |
| 5 | Extract HTTP client factory | Low | Standardizes timeouts/retries |
| 6 | Fix false capability flags | Low | Prevents misleading behavior |
| 7 | Named constants for magic numbers | Low | Improves maintainability |
| 8 | Wire compat providers into service layer | High | Unlocks 13 providers |
| 9 | Standardize env var handling | Low | Consistency |
| 10 | Add file size limits to extractors | Low | Prevents memory exhaustion |
