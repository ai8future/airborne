# Airborne Codebase Refactoring Report

**Date Created:** 2026-01-21 21:15 UTC

**Agent:** Claude:Opus 4.5

---

## Executive Summary

This report provides a comprehensive code quality analysis of the Airborne codebase (v1.7.2), a unified multi-provider LLM gateway service. The analysis covers:

- **Backend:** ~15,000 lines of Go code across 66 source files
- **Frontend:** ~3,000 lines of TypeScript/React (Next.js dashboard)
- **Test Coverage:** 387 tests across 28 test files (~42% package coverage)

**Key Findings:**
- **780+ lines** of duplicated boilerplate in provider layer
- **12 instances** of repeated config extraction in file service
- **3 files** with duplicated `ActivityEntry` interface definition
- **23,438 lines** of database code with zero test coverage
- **1,303-line** React component that should be split into 5+ files

---

## Table of Contents

1. [Provider Layer Issues](#1-provider-layer-issues)
2. [Service Layer Issues](#2-service-layer-issues)
3. [Authentication & Configuration Issues](#3-authentication--configuration-issues)
4. [RAG Pipeline Issues](#4-rag-pipeline-issues)
5. [Dashboard Frontend Issues](#5-dashboard-frontend-issues)
6. [Test Coverage Gaps](#6-test-coverage-gaps)
7. [Priority Matrix](#7-priority-matrix)
8. [Recommended Actions](#8-recommended-actions)

---

## 1. Provider Layer Issues

**Directory:** `internal/provider/`
**Total Lines:** ~5,183 across 20 provider implementations

### 1.1 Boilerplate Wrapper Duplication (HIGH PRIORITY)

**Affected Files:** deepseek, cohere, mistral, grok, perplexity, openrouter, hyperbolic, fireworks, together, cerebras, nebius, deepinfra, upstage (13 providers)

**Problem:** Identical ~60-line wrapper pattern repeated across all OpenAI-compatible providers:

```go
// This structure is copy-pasted 13 times:
type Client struct { *compat.Client }
type ClientOption func(*clientOptions)
type clientOptions struct { debug bool }
func WithDebugLogging() ClientOption
func NewClient() *Client  // Only difference: Name, DefaultBaseURL, DefaultModel, APIKeyEnvVar
```

**Impact:** ~780 lines of duplicate code

**Recommendation:** Create a factory function or code generation:
```go
func NewCompatClient(
    name, defaultBaseURL, defaultModel, apiKeyEnvVar string,
    supportsFileSearch, supportsWebSearch, supportsStreaming bool,
) *compat.Client
```

### 1.2 Retry Loop Pattern Duplication (HIGH PRIORITY)

**Affected Files:** `openai/client.go`, `anthropic/client.go`, `gemini/client.go`

**Problem:** Same retry loop (~80 lines) duplicated in each major provider's `GenerateReply()`:

```go
var lastErr error
for attempt := 1; attempt <= retry.MaxAttempts; attempt++ {
    slog.Info("[provider] request", "attempt", attempt, ...)
    reqCtx, reqCancel := context.WithTimeout(ctx, retry.RequestTimeout)
    // API call
    reqCancel()
    if err != nil {
        if errors.Is(err, context.DeadlineExceeded) && ctx.Err() == nil { /* retry */ }
        if !retry.IsRetryable(err) { return provider.GenerateResult{}, lastErr }
        retry.SleepWithBackoff(ctx, attempt)
        continue
    }
    // Extract and return result
}
```

**Impact:** ~240 lines duplicated

**Recommendation:** Extract to common helper:
```go
func executeWithRetry(
    ctx context.Context,
    execute func(context.Context) (provider.GenerateResult, error),
) (provider.GenerateResult, error)
```

### 1.3 Helper Function Duplication (MEDIUM PRIORITY)

| Function | Locations | Issue |
|----------|-----------|-------|
| `buildMessages()` / `buildContents()` | openai, anthropic, gemini, compat | 4 different implementations with similar patterns |
| `extractText()` | openai, gemini, compat, anthropic | 4 implementations with nearly identical logic |
| `extractUsage()` | openai, gemini, compat, anthropic | 4 implementations |
| `validateAPIKey()` | All major providers | Same validation with different error messages |

**Impact:** ~100+ lines duplicated

### 1.4 Inconsistent Error Handling (MEDIUM PRIORITY)

**Stream Cleanup Inconsistency:**
- Anthropic/Gemini properly clean up context cancel on error
- OpenAI missing cleanup in some paths

**Empty Response Handling:**
- OpenAI: Retries on empty response
- Anthropic: Retries on empty response
- Gemini: Only continues if not blocked by safety

### 1.5 Unused Code (LOW PRIORITY)

**File:** `anthropic/client.go` lines 524-537

```go
func extractText(resp *anthropic.Message) string {
    // This function is defined but never called
    // extractContent() is used instead
}
```

### 1.6 Magic Constants Scattered (LOW PRIORITY)

```go
// openai/client.go
pollInitial = 500 * time.Millisecond
pollMax = 5 * time.Second

// anthropic/client.go
maxHistoryChars = 50000

// gemini/client.go
maxHistoryChars = 50000  // Same value, different file
```

**Recommendation:** Centralize constants in `internal/provider/constants.go`

---

## 2. Service Layer Issues

**Directory:** `internal/service/`
**Files:** `chat.go` (1,200+ lines), `files.go` (750+ lines), `admin.go` (100+ lines)

### 2.1 Configuration Extraction Duplication (HIGH PRIORITY)

**File:** `files.go` - 12 instances

**Problem:** Config extraction is repeated identically across all provider-specific methods:

```go
// Repeated at lines: 84-86, 118-120, 304-306, 344-346, 460-462, 490-492, 571-573, 597-599, 670-672, 703-705
cfg := openai.FileStoreConfig{
    APIKey:  req.Config.GetApiKey(),
    BaseURL: req.Config.GetBaseUrl(),
}
```

**Recommendation:** Create helper functions:
```go
func extractOpenAIConfig(req interface{}) (openai.FileStoreConfig, error)
func extractGeminiConfig(req interface{}) (gemini.FileStoreConfig, error)
func validateProviderConfig(cfg interface{}) error
```

### 2.2 Provider Routing Pattern (HIGH PRIORITY)

**File:** `files.go` - 5 locations

**Problem:** Same switch/case pattern repeated in `CreateFileStore`, `UploadFile`, `DeleteFileStore`, `GetFileStore`, `ListFileStores`:

```go
switch req.Provider {
case pb.Provider_PROVIDER_OPENAI:
    return s.createOpenAIVectorStore(ctx, req)
case pb.Provider_PROVIDER_GEMINI:
    return s.createGeminiFileSearchStore(ctx, req)
default:
    return s.createInternalStore(ctx, req)
}
```

**Recommendation:** Create provider dispatcher pattern:
```go
type ProviderHandler interface {
    CreateStore(ctx, req) (*pb.CreateFileStoreResponse, error)
    Upload(ctx, stream, metadata) error
    Delete(ctx, req) (*pb.DeleteFileStoreResponse, error)
    Get(ctx, req) (*pb.GetFileStoreResponse, error)
    List(ctx, req) (*pb.ListFileStoresResponse, error)
}
```

### 2.3 Permission Checking Pattern (MEDIUM PRIORITY)

**Affected Files:** All services (~20 methods)

**Problem:** Repeated guard clause:
```go
if err := auth.RequirePermission(ctx, auth.PermissionXXX); err != nil {
    return nil, err
}
```

**Recommendation:** Move to gRPC interceptor for method-level permission declarations

### 2.4 Conversion Functions Location (MEDIUM PRIORITY)

**File:** `chat.go` lines 892-1043

**Problem:** 11 conversion functions mixed with business logic:
- `convertHistory`, `convertUsage`, `convertCitation`, `convertTools`, `convertToolResults`, `convertToolCall`, `convertCodeExecution`, `convertGeneratedImage`, `convertStructuredMetadata`

**Recommendation:** Extract to `internal/service/converters.go` or `internal/convert/` package

### 2.5 Persistence Layer Coupling (MEDIUM PRIORITY)

**File:** `chat.go` `persistConversation()` (lines 1048-1160)

**Problem:** 100+ lines of database persistence logic tightly coupled with ChatService

**Recommendation:** Extract to dedicated persistence layer:
```go
type ConversationPersister interface {
    PersistTurn(ctx, turn *ConversationTurn) error
}
```

---

## 3. Authentication & Configuration Issues

**Directories:** `internal/auth/`, `internal/config/`

### 3.1 Token Extraction Duplication (HIGH PRIORITY)

**Files:** `interceptor.go` (lines 130-160), `static.go` (lines 85-98)

**Problem:** Identical token extraction logic duplicated:
```go
// In interceptor.go
func extractAPIKey(md metadata.MD) string { ... }

// In static.go - EXACT SAME CODE
func extractStaticToken(md metadata.MD) string { ... }
```

**Recommendation:** Extract to shared `metadata.go` utility

### 3.2 Config Type Conversion Repetition (MEDIUM PRIORITY)

**File:** `config.go` lines 216-385

**Problem:** Same strconv pattern repeated 12+ times:
```go
if port := os.Getenv("AIRBORNE_GRPC_PORT"); port != "" {
    if p, err := strconv.Atoi(port); err == nil {
        c.Server.GRPCPort = p
    } else {
        slog.Warn("invalid AIRBORNE_GRPC_PORT, using default", "value", port, "error", err)
    }
}
```

**Recommendation:** Create helper functions:
```go
func getIntEnv(envVar string, defaultValue int) int
func getBoolEnv(envVar string, defaultValue bool) bool
func getStringEnv(envVar string, defaultValue string) string
```

### 3.3 Missing Authenticator Interface (MEDIUM PRIORITY)

**Problem:** `Authenticator` and `StaticAuthenticator` have no common interface

**Current Pattern:**
```go
if cfg.Auth.AuthMode == "redis" && keyStore != nil {
    authenticator := auth.NewAuthenticator(keyStore, rateLimiter)
    // ...
} else {
    staticAuth := auth.NewStaticAuthenticator(cfg.Auth.AdminToken)
    // ...
}
```

**Recommendation:** Define common interface:
```go
type Authenticator interface {
    UnaryInterceptor() grpc.UnaryServerInterceptor
    StreamInterceptor() grpc.StreamServerInterceptor
}
```

### 3.4 Rate Limit Script Logic Issue (MEDIUM PRIORITY)

**File:** `ratelimit.go` lines 19-46

**Problem:** Lua script increments BEFORE checking limit - allows some requests to slip through

**Current:**
```lua
local current = redis.call('INCR', key)
return current
-- In Go: if int(count) > limit { return error }
```

**Recommendation:** Check BEFORE incrementing to prevent over-limit requests

### 3.5 Missing Input Validation (LOW PRIORITY)

**File:** `keys.go` `GenerateAPIKey()` lines 84-208

**Problem:** No validation for:
- Empty clientID
- Empty clientName
- Empty permissions array
- Negative rate limits

---

## 4. RAG Pipeline Issues

**Directory:** `internal/rag/`

### 4.1 Repeated Validation Logic (MEDIUM PRIORITY)

**File:** `service.go` lines 34-57

**Problem:** `validateCollectionParts()` called 6 times across methods with identical pattern

**Recommendation:** Create validation middleware or struct

### 4.2 Silent Failures in Qdrant Store (HIGH PRIORITY)

**File:** `vectorstore/qdrant.go` lines 167-176

**Problem:** Silent failures that hide bugs:
```go
resultsRaw, ok := resp["result"].([]any)
if !ok {
    return nil, nil  // Returns nil instead of error!
}

rm, ok := r.(map[string]any)
if !ok {
    continue  // Silently drops malformed results
}
```

**Recommendation:** Log warnings or return errors for unexpected response formats

### 4.3 HTTP Request Pattern Duplication (MEDIUM PRIORITY)

**Files:** `ollama.go`, `docbox.go`, `qdrant.go`

**Problem:** HTTP request setup pattern repeated in 4+ locations

**Recommendation:** Create shared HTTP client helper in `internal/http/`

### 4.4 Nested Type Assertion Chains (LOW PRIORITY)

**File:** `qdrant.go` lines 100-109

**Problem:** Pyramid of doom:
```go
if config, ok := result["config"].(map[string]any); ok {
    if params, ok := config["params"].(map[string]any); ok {
        if vectors, ok := params["vectors"].(map[string]any); ok {
            if size, ok := vectors["size"].(float64); ok {
                dimensions = int(size)
            }
        }
    }
}
```

**Recommendation:** Create helper: `func getNestedFloat(m map[string]any, keys ...string) (float64, bool)`

---

## 5. Dashboard Frontend Issues

**Directory:** `dashboard/src/`

### 5.1 Type Definition Duplication (HIGH PRIORITY)

**Problem:** `ActivityEntry` interface defined in THREE separate files:
- `app/page.tsx` (lines 8-25)
- `components/ActivityPanel.tsx` (lines 3-20)
- `components/ConversationPanel.tsx` (lines 62-79)

**Recommendation:** Create `/src/types/index.ts` shared module

### 5.2 Giant Component (HIGH PRIORITY)

**File:** `ConversationPanel.tsx` - 1,303 lines

**Problem:** Single component handles:
- Message bubble rendering (164-592 lines)
- Error boundary implementation (19-39 lines)
- Grounding sources display (106-147 lines)
- Main conversation logic (609+ lines)
- Nested components: `MessageErrorBoundary`, `GroundingSources`, `MessageBubble`, `ViewToggle`

**Recommendation:** Extract to `/src/components/conversation/` subdirectory with 5+ smaller components

### 5.3 Utility Duplication (MEDIUM PRIORITY)

**Duplicated Functions:**
- `getProviderColor()` - ActivityPanel.tsx AND ConversationPanel.tsx
- `formatTokens()` - ActivityPanel.tsx (similar logic in ConversationPanel.tsx)
- ReactMarkdown configuration - duplicated twice within MessageBubble

**Recommendation:** Create `/src/utils/` with:
- `formatTokens.ts`
- `providerColors.ts`
- `markdownComponents.tsx`

### 5.4 State Management Sprawl (MEDIUM PRIORITY)

**File:** `ConversationPanel.tsx`

**Problem:** 11 state variables in one component:
```typescript
const [messages, setMessages] = useState<ThreadMessage[]>([]);
const [loading, setLoading] = useState(false);
const [inputValue, setInputValue] = useState("");
const [sending, setSending] = useState(false);
const [pendingMessageId, setPendingMessageId] = useState<string | null>(null);
const [sendStartTime, setSendStartTime] = useState<number | null>(null);
const [selectedFile, setSelectedFile] = useState<File | null>(null);
const [systemPromptType, setSystemPromptType] = useState<"email4ai" | "custom">("email4ai");
const [customPromptText, setCustomPromptText] = useState("");
const [showPromptModal, setShowPromptModal] = useState(false);
const [showPromptDropdown, setShowPromptDropdown] = useState(false);
```

**Recommendation:** Extract to custom hook `useConversation()`

### 5.5 API Route Error Handling (MEDIUM PRIORITY)

**Files:** `api/activity/route.ts`, `api/threads/[threadId]/route.ts`

**Problem:** Returns HTTP 200 even on backend errors:
```typescript
if (!response.ok) {
  return NextResponse.json({
    activity: [],
    error: `...`,
  }, { status: 200 }); // Wrong status code!
}
```

**Recommendation:** Return appropriate HTTP status codes (500, 502, etc.)

### 5.6 Unused Component (LOW PRIORITY)

**File:** `DebugModal.tsx` (456 lines)

**Problem:** Component defined but NOT imported anywhere in codebase

### 5.7 Missing Memoization (LOW PRIORITY)

**File:** `ConversationPanel.tsx` line 131

**Problem:** Sorting on every render:
```typescript
{[...activity].sort((a, b) => new Date(b.timestamp).getTime() - new Date(a.timestamp).getTime()).map(...)}
```

**Recommendation:** Use `useMemo()` for sorted lists

---

## 6. Test Coverage Gaps

### 6.1 Critical Untested Code

| File | Lines | Risk Level |
|------|-------|------------|
| `internal/db/repository.go` | 23,438 | **CRITICAL** - Core persistence logic |
| `internal/db/models.go` | 10,050 | **CRITICAL** - Data model definitions |
| `internal/admin/server.go` | ~500 | **HIGH** - Admin HTTP endpoints |
| `internal/pricing/pricing.go` | ~200 | **MEDIUM** - Billing calculations |

### 6.2 Provider Test Coverage

**Tested (4):** openai, anthropic, mistral, gemini

**Untested (9+):** cohere, deepseek, fireworks, grok, hyperbolic, nebius, openrouter, perplexity, together, upstage, cerebras

### 6.3 CI/CD Gap

**Problem:** GitHub Actions only builds Docker, doesn't run tests

**File:** `.github/workflows/docker-build.yml`

**Recommendation:** Add `make test-coverage` step before Docker build

---

## 7. Priority Matrix

### Critical (Immediate Action)

| Issue | Location | Effort | Impact |
|-------|----------|--------|--------|
| Provider wrapper duplication | `internal/provider/` | High | 780 LOC saved |
| Retry loop duplication | openai, anthropic, gemini | Medium | 240 LOC saved |
| Silent Qdrant failures | `internal/rag/vectorstore/` | Low | Bug prevention |
| Database test coverage | `internal/db/` | High | Quality assurance |
| ConversationPanel size | `dashboard/src/components/` | Medium | Maintainability |

### High Priority

| Issue | Location | Effort | Impact |
|-------|----------|--------|--------|
| Config extraction duplication | `internal/service/files.go` | Low | 12 instances fixed |
| Provider routing pattern | `internal/service/files.go` | Medium | Extensibility |
| Token extraction duplication | `internal/auth/` | Low | DRY violation |
| Type definition duplication | `dashboard/src/` | Low | 3 files consolidated |
| CI test execution | `.github/workflows/` | Low | Quality gate |

### Medium Priority

| Issue | Location | Effort | Impact |
|-------|----------|--------|--------|
| Permission checking pattern | All services | Medium | Cross-cutting concern |
| Conversion functions location | `internal/service/chat.go` | Low | Code organization |
| Config type conversion | `internal/config/` | Low | 12 instances |
| API route error handling | `dashboard/src/app/api/` | Low | Error semantics |
| Rate limit script logic | `internal/auth/ratelimit.go` | Medium | Correctness |

### Low Priority

| Issue | Location | Effort | Impact |
|-------|----------|--------|--------|
| Unused Anthropic function | `internal/provider/anthropic/` | Trivial | 15 LOC removed |
| Magic constants | `internal/provider/` | Low | Code organization |
| Unused DebugModal | `dashboard/src/components/` | Trivial | Dead code |
| Memoization opportunities | `dashboard/src/components/` | Low | Performance |

---

## 8. Recommended Actions

### Phase 1: Quick Wins (1-2 days)

1. **Create shared utility files:**
   - `internal/auth/metadata.go` - token extraction
   - `internal/service/config_helpers.go` - config extraction
   - `dashboard/src/types/index.ts` - shared TypeScript types
   - `dashboard/src/utils/formatters.ts` - utility functions

2. **Remove dead code:**
   - `extractText()` in `anthropic/client.go`
   - Evaluate `DebugModal.tsx` usage

3. **Fix CI pipeline:**
   - Add test execution to GitHub Actions

### Phase 2: Structural Improvements (1-2 weeks)

1. **Provider layer refactoring:**
   - Create factory function for OpenAI-compatible providers
   - Extract `executeWithRetry()` helper

2. **Service layer refactoring:**
   - Create provider dispatcher interface
   - Extract converters to separate file
   - Create validation helpers

3. **Dashboard componentization:**
   - Split ConversationPanel into 5+ smaller components
   - Create reusable markdown configuration

### Phase 3: Infrastructure (2-4 weeks)

1. **Test coverage expansion:**
   - Add database layer tests (repository.go)
   - Add untested provider tests
   - Add integration test suite

2. **Authentication refactoring:**
   - Create common Authenticator interface
   - Fix rate limit script logic

3. **RAG pipeline improvements:**
   - Create typed payload schema
   - Fix silent failure patterns
   - Add structured error types

---

## Conclusion

The Airborne codebase is a well-architected multi-provider LLM gateway with good separation of concerns at the package level. However, it has accumulated technical debt through code duplication, particularly in the provider layer (780+ lines) and service layer (config extraction, provider routing). The dashboard has a monolithic component (1,303 lines) that should be decomposed.

The most critical gaps are:
1. **23,438 lines** of database code with no tests
2. **780 lines** of provider boilerplate that could be eliminated with a factory pattern
3. **1,303-line** React component that violates single responsibility

Addressing these issues will significantly improve maintainability, reduce bug surface area, and make the codebase more welcoming to new contributors.

---

*Report generated by Claude:Opus 4.5*
