# Code Quality & Refactoring Report: Airborne

**Date Created:** 2026-01-15 12:00
**Date Updated:** 2026-01-15

> **Note:** This report contains architectural refactoring suggestions for future consideration. These are not bugs or security issues - they are opportunities to reduce code duplication as the provider count grows.

## Executive Summary

The `airborne` codebase demonstrates a solid foundation with clear separation of concerns between the gRPC service layer, business logic, and provider integrations. However, as the number of AI providers grows (OpenAI, Anthropic, Gemini, etc.), significant code duplication has emerged, particularly within the `internal/provider` package.

The primary opportunities for improvement lie in:
1.  **Reducing boilerplate** in provider implementations (retries, logging, HTTP client setup).
2.  **Decoupling** the `ChatService` from concrete provider implementations to support a plugin-like architecture.
3.  **Standardizing** error handling and observability across all integrations.

## Architectural Analysis

### 1. Provider Registry Pattern

**Current State:**
The `ChatService` struct explicitly holds references to concrete provider implementations (`openaiProvider`, `geminiProvider`, `anthropicProvider`). Adding a new provider requires modifying the `ChatService` struct, the `NewChatService` constructor, and the `selectProviderWithTenant` switch statement.

**Recommendation:**
Implement a **Provider Registry** or **Factory** pattern.
*   Create a `provider.Registry` that maps provider names (string) to `provider.Provider` instances.
*   Providers register themselves at startup (e.g., in `main.go` or `init()` functions).
*   `ChatService` depends only on the `Registry`.

**Benefits:**
*   **Open/Closed Principle:** Add new providers without modifying existing service logic.
*   **Dynamic Configuration:** Easier to enable/disable providers based on configuration.

### 2. Centralized Resilience Layer (Retry & Circuit Breaker)

**Current State:**
Each provider (`internal/provider/*/client.go`) implements its own `retry` loop, `sleepWithBackoff` function, and `isRetryableError` check. This logic is nearly identical across all files (~50 lines of duplicated code per provider).

**Recommendation:**
Extract resilience logic into a decorator or middleware.
*   Create a `ResilientProvider` struct that wraps any `provider.Provider`.
*   Implement `GenerateReply` and `GenerateReplyStream` in this wrapper to handle retries, timeouts, and backoffs.
*   Define a standard `IsRetryable(error) bool` helper in the `provider` package or a separate `errors` package.

**Benefits:**
*   **DRY (Don't Repeat Yourself):** Removes hundreds of lines of duplicated code.
*   **Consistency:** Global retry policies (e.g., "max 3 retries") are applied uniformly.
*   **Observability:** Centralized place for "retry attempt" logging and metrics.

### 3. Unified Proto Mapping

**Current State:**
`internal/service/chat.go` contains numerous helper functions (`convertUsage`, `convertCitation`, `convertToolCall`) at the bottom of the file. As the API surface grows, this file will become cluttered.

**Recommendation:**
Move all Proto <-> Domain conversion logic to a dedicated `internal/mapper` or `pkg/converter` package.

**Benefits:**
*   **readability:** Cleans up the core service logic.
*   **Testability:** Mappers can be unit tested independently of the service logic.

## Specific Refactoring Opportunities

### A. The `internal/provider` Package

1.  **Common HTTP Transport:**
    *   Currently, `httpcapture.New()` is called inside every `GenerateReply`.
    *   **Refactor:** Pass a shared, pre-configured `*http.Client` (with capture and logging middleware) to the provider constructors.

2.  **Standardized Response Parsing:**
    *   While APIs differ, the high-level flow (Request -> HTTP -> Check Error -> Parse Body -> Map to Result) is constant.
    *   **Refactor:** Consider a generic `ExecuteRequest[Req, Resp]` helper if the divergence allows, though the Registry and Resilience patterns (Point 2) offer higher ROI.

3.  **Consolidate `isRetryableError`:**
    *   Move the error classification logic (detecting 429s, 5xxs, timeouts) to a shared helper `provider.IsRetryable(err)`.

### B. The `internal/service/chat.go` File

1.  **Remove Provider Switch Statements:**
    *   Replace the `switch req.PreferredProvider` block with `registry.Get(providerName)`.

2.  **Streamline `prepareRequest`:**
    *   The `prepareRequest` method is doing too much (validation, auth checks, RAG retrieval).
    *   **Refactor:** Split into `validator.ValidateRequest(req)` and `rag.EnrichRequest(ctx, req)`.

### C. `internal/rag`

1.  **Vector Store Abstraction:**
    *   The `collectionName` logic (`tenantID + "_" + storeID`) leaks implementation details about how Qdrant namespaces data.
    *   **Refactor:** Ensure the `vectorstore` interface handles namespacing internally, allowing the service to just pass `TenantID` and `StoreID` without knowing how they are combined.

## Code Quality & Style

*   **Function Length:** `GenerateReplyStream` in `gemini/client.go` and `openai/client.go` is very long due to inline stream processing. Extract specific event handlers (e.g., `handleToolCall`, `handleTextDelta`) into small private methods.
*   **Comments:** The code is generally well-commented. Continue this practice, especially for public interfaces.
*   **Magic Numbers:** Constants like `pollInitial = 500 * time.Millisecond` are defined in each package. Move shared defaults to a `provider/defaults.go` file.

## Implementation Plan

1.  **Phase 1: Shared Utilities (Low Risk)**
    *   Create `internal/mapper` and move conversion functions.
    *   Create `provider.IsRetryable` and shared constants.

2.  **Phase 2: Provider Refactoring (Medium Risk)**
    *   Refactor one provider (e.g., `compat` or `deepseek` as a test) to use the new Resilience Wrapper pattern.
    *   Once verified, roll out to `openai`, `anthropic`, and `gemini`.

3.  **Phase 3: Registry & Service Decoupling (High Risk)**
    *   Implement `ProviderRegistry`.
    *   Update `ChatService` to use the registry.
    *   Remove hard dependencies.

## Conclusion

The codebase is currently in a functional and safe state but is poised for "sprawl" if the duplication in the provider layer is not addressed. Adopting a Registry pattern and centralizing resilience logic are the two highest-value activities to ensure `airborne` remains maintainable as it scales.
