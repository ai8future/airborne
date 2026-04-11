Date Created: Saturday, January 24, 2026 at 8:31 PM
TOTAL_SCORE: 75/100

# Airborne Codebase Refactoring Audit

## Executive Summary

The Airborne codebase demonstrates a solid, professional Go project structure. It adheres to standard idioms (Project Layout), uses gRPC/Protobuf effectively, and maintains a clear separation of concerns between configuration, transport, and business logic. The `internal/provider` abstraction is a strong design choice, allowing for a unified interface across different AI models.

However, as the number of providers and features (RAG, Image Gen, etc.) grows, `ChatService` is becoming a bottleneck and a "God Object." There is also noticeable code duplication within provider implementations, particularly in handling streaming and HTTP client configuration.

## Score Breakdown

*   **Structure & Organization (20/20):** Excellent use of `cmd`, `internal`, and `pkg`. Clear package boundaries.
*   **Code Quality & Readability (15/20):** Generally clean, but `GenerateReply` and `GenerateReplyStream` in both service and provider layers are becoming overly long and complex.
*   **DRY (Don't Repeat Yourself) (10/20):** Significant boilerplate duplication across provider clients (`openai`, `anthropic`, `gemini`) for request setup and stream handling.
*   **Maintainability & Coupling (10/20):** `ChatService` is tightly coupled to specific provider implementations. Adding a new provider requires modifying the service struct, initialization, and switch statements.
*   **Conventions & Standards (20/20):** Strong adherence to Go conventions, error handling, and context usage.

## Key Findings

### Positives
*   **Unified Provider Interface:** `internal/provider/provider.go` defines a comprehensive contract that simplifies the consumer code.
*   **Configuration Management:** `internal/config` and `internal/service/config` show a robust approach to merging tenant-level defaults with request-level overrides.
*   **Security Awareness:** SSRF protection and sensitive data masking are built-in.

### Negatives
*   **ChatService Coupling:** `ChatService` explicitly holds references to `openaiProvider`, `geminiProvider`, etc. This violates the Open-Closed Principle.
*   **Provider Boilerplate:** Each provider client re-implements similar logic for `NewCapturedClientConfig`, timeout management, and sometimes retry logic.
*   **Magic Strings:** Provider names ("openai", "gemini") are scattered as string literals throughout the codebase.
*   **Logic Leakage:** Pricing/Cost calculation logic is leaking into `ChatService.persistConversation`.

## Detailed Refactoring Opportunities

### 1. Decouple ChatService with a Provider Registry (High Impact)
Currently, `ChatService` looks like this:
```go
type ChatService struct {
    openaiProvider    provider.Provider
    geminiProvider    provider.Provider
    anthropicProvider provider.Provider
    // ...
}
```
This forces modification of `ChatService` for every new provider.

**Refactoring Goal:**
Introduce a `ProviderRegistry` map.
```go
type ChatService struct {
    providers map[string]provider.Provider
    // ...
}
```
*   **Action:** Update `NewChatService` to accept a registry or list of providers.
*   **Action:** Replace switch statements in `SelectProvider` and `getFallbackProvider` with map lookups.

### 2. Standardize Provider Clients (Medium Impact)
`internal/provider/openai/client.go` and `internal/provider/anthropic/client.go` share significant structure:
*   Timeout/Context setup.
*   `httputil.NewCapturedClientConfig` usage.
*   `GenerateReply` vs `GenerateReplyStream` setup logic.

**Refactoring Goal:**
Create a `BaseClient` or helper functions in `internal/provider/base` (or similar) to handle the common HTTP client setup and debug logging. While full generics might be overkill due to different SDKs, the setup phase can be shared.

### 3. Centralize Cost Calculation (Medium Impact)
`ChatService.persistConversation` contains complex logic for calculating costs, especially for Gemini (handling cached tokens, thinking tokens, etc.).

**Refactoring Goal:**
Move all cost calculation logic into `internal/pricing`.
*   **Action:** Ensure `pricing.CalculateGeminiCost` and `pricing.CalculateCost` cover all cases.
*   **Action:** `ChatService` should only pass the usage struct and model name to the pricing package and get back a cost object.

### 4. Centralize Constants (Low Impact)
Provider strings ("openai", "gemini") are hardcoded in multiple places (`config.go`, `chat.go`, `provider.go`).

**Refactoring Goal:**
Define these as constants in a common package (e.g., `pkg/types` or `internal/provider`).
```go
const (
    ProviderOpenAI    = "openai"
    ProviderGemini    = "gemini"
    ProviderAnthropic = "anthropic"
)
```

## Proposed Roadmap

1.  **Phase 1 (Cleanup):** Extract pricing logic from `ChatService` to `internal/pricing`. Define provider name constants.
2.  **Phase 2 (Decoupling):** Implement `ProviderRegistry` and refactor `ChatService` to use it.
3.  **Phase 3 (Provider Polish):** Audit provider clients for shared code and extract common helpers for configuration and logging.
