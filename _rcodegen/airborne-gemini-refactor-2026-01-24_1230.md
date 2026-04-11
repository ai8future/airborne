Date Created: Saturday, January 24, 2026 at 12:30 PM
TOTAL_SCORE: 85/100

# Airborne Codebase Refactor Report

## Executive Summary
The Airborne codebase is a high-quality, production-grade Go application. It demonstrates strong adherence to Go idioms, robust error handling, and a clear separation of concerns between transport (gRPC), business logic (Service), and external integrations (Providers). The code is readable, well-commented, and includes necessary production features like structured logging (`slog`), tracing (via request IDs), and metrics (token usage).

However, the architecture exhibits strict coupling in the `ChatService`, making it difficult to add new AI providers without modifying core service logic. Additionally, the configuration loading mechanism mixes side effects (network calls to Doppler) with logic, hindering testability.

## Detailed Analysis

### 1. Provider Architecture (Score: 80/100)
**Strengths:**
- **Interface Design:** The `provider.Provider` interface is excellent. It correctly abstracts complex behaviors like streaming (`GenerateReplyStream`), tool calling, and RAG support.
- **Shared Logic:** The use of `httputil.CapturedClientConfig` is a standout feature. It unifies HTTP client creation, debugging (capture), and configuration validation across different SDKs (OpenAI, Anthropic). This prevents the common anti-pattern of every provider re-implementing basic HTTP boilerplate.

**Weaknesses:**
- **Tight Coupling:** `ChatService` explicitly instantiates and holds references to concrete provider implementations (`openaiProvider`, `geminiProvider`, `anthropicProvider`).
- **Violation of Open/Closed Principle:** Adding a new provider requires:
    1.  Adding a field to `ChatService` struct.
    2.  Initializing it in `NewChatService`.
    3.  Updating the switch statement in `selectProviderWithTenant`.
    4.  Updating the switch statement in `getFallbackProvider`.
    5.  Updating `mapProviderToProto` and other helpers.

### 2. Configuration Management (Score: 85/100)
**Strengths:**
- **Flexibility:** The system handles YAML files, environment variables, "frozen" JSON configs, and remote secrets (Doppler) seamlessly.
- **Defaults:** Sensible defaults are provided for all critical values.

**Weaknesses:**
- **Side Effects in Load:** The `Load()` function calls `fetchDopplerSecret`, which makes a real HTTP request. This makes unit testing the config loader difficult and introduces a hidden startup dependency.
- **Global State:** Reliance on `os.Getenv` inside helper functions like `expandEnv` makes parallel testing of config loading flaky.

### 3. Service Layer (Score: 90/100)
**Strengths:**
- **Code Reuse:** The `prepareRequest` method in `ChatService` smartly extracts common validation and setup logic for both unary and streaming endpoints, reducing duplication.
- **Safety:** Tenant context (`auth.TenantFromContext`) and permission checks are consistently applied.
- **Resilience:** Fallback logic and error sanitization (`sanitize.SanitizeForClient`) are well-implemented.

### 4. Code Quality & Style (Score: 90/100)
- **Formatting:** Code is standard `gofmt` style.
- **Concurrency:** Goroutines are used appropriately for non-critical path tasks like persistence (`persistConversation`). Context propagation (with timeouts) is handled correctly.
- **Logging:** Structured logging is used effectively throughout.

## Recommendations

### High Priority: Refactor Provider Management
Transition from hardcoded provider fields to a **Provider Registry**.

**Current State:**
```go
type ChatService struct {
    openaiProvider    provider.Provider
    geminiProvider    provider.Provider
    // ...
}
```

**Proposed State:**
```go
type ChatService struct {
    providers map[string]provider.Provider
    // ...
}

// Usage
func (s *ChatService) selectProvider(...) {
    p, ok := s.providers[name]
    if !ok {
        return nil, fmt.Errorf("provider not found")
    }
    return p, nil
}
```
This change would allow providers to be registered dynamically or via a loop in `NewChatService`, adhering to the Open/Closed Principle.

### Medium Priority: Decouple Config Loading
Extract the secret fetching logic into an interface.

**Proposed Change:**
Define a `SecretFetcher` interface.
```go
type SecretFetcher interface {
    Fetch(project, secretName string) string
}
```
Inject this into the config loader. This allows tests to provide a mock fetcher without making network calls.

### Low Priority: Standardize Error Handling
While `sanitize.SanitizeForClient` is used, some raw errors might still leak in edge cases. Ensure a consistent error wrapping strategy (e.g., `%w`) is used everywhere to preserve stack traces for internal logs while keeping client errors clean.

## Conclusion
Airborne is a robust application with a solid foundation. The recommended refactoring for the `ChatService` will significantly improve maintainability and extensibility as the number of supported AI providers grows.
