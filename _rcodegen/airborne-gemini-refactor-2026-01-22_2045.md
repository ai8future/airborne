Date Created: Thursday, January 22, 2026 at 8:45 PM EST
TOTAL_SCORE: 85/100

# Airborne Refactoring Report

## 1. Executive Summary

The Airborne codebase demonstrates a strong foundation with a clear, modular architecture following standard Go project layouts (`cmd/`, `internal/`, `pkg/`). The code is idiomatic, well-formatted, and makes good use of modern Go features (e.g., `slog`). Automated testing is present and functional.

However, as the number of providers (OpenAI, Anthropic, Gemini) has grown, significant code duplication has emerged. Additionally, the core `ChatService` is accumulating responsibilities that blur the lines between orchestration and business logic, leading to some brittle conditional logic.

## 2. Detailed Findings

### A. High Code Duplication in Providers (Severity: Medium)

**Observation:**
The files `internal/provider/openai/client.go`, `internal/provider/anthropic/client.go`, and `internal/provider/gemini/client.go` share substantial boilerplate code. Specifically:
- **Client Configuration:** All use `httputil.NewCapturedClientConfig` and similar initialization patterns.
- **Request Lifecycle:** The retry loops, timeout management (`context.WithTimeout`), and logging patterns are nearly identical across all three implementations.
- **Stream Handling:** The structure of `GenerateReplyStream` is very similar, differing primarily in the vendor-specific SDK calls.

**Impact:**
- Fixes to common logic (e.g., changing how timeouts or retries work) must be applied in three places.
- Increases the cognitive load when adding a new provider.

**Recommendation:**
Refactor the common "request runner" logic into a shared helper or base struct. This component would handle:
1.  Context timeouts.
2.  Retry loops with backoff.
3.  Standardized logging.
4.  Error wrapping/classification.

The provider implementations would then focus solely on mapping Airborne's `GenerateParams` to the vendor SDK's request format and mapping the response back.

### B. ChatService Complexity & SRP Violations (Severity: Medium)

**Observation:**
`internal/service/chat.go` has grown to over 800 lines and handles multiple distinct responsibilities:
- gRPC request/response mapping.
- Authentication/Authorization checks.
- Provider selection logic.
- RAG (Retrieval-Augmented Generation) context retrieval and prompt injection.
- Slash command parsing (`/image`).
- Asynchronous DB persistence.
- HTML rendering.

**Impact:**
- The class is difficult to test in isolation without complex mocking.
- Changes to one aspect (e.g., how RAG is formatted) require modifying the core service file.

**Recommendation:**
Decompose `ChatService` into smaller collaborators:
- **`RAGOrchestrator`**: Encapsulate the logic for retrieving and formatting RAG context.
- **`PersistenceService`**: Handle the async logging of conversation turns.
- **`ProviderSelector`**: Isolate the logic for choosing a provider based on tenant config and request overrides.

### C. Brittle Conditional Logic (Severity: Low)

**Observation:**
In `internal/service/chat.go`, there is explicit logic that checks for specific provider names:
```go
if req.EnableFileSearch && ... && selectedProvider.Name() != "openai" {
    // ... manual RAG retrieval ...
}
```
This violates the Open/Closed Principle. Although `Provider` has a `SupportsFileSearch()` method, the service manually checks for "openai" to bypass manual RAG (presumably because OpenAI handles it natively).

**Impact:**
- If another provider adds native file search support, this code must be modified.
- It defeats the purpose of the interface capability check.

**Recommendation:**
Refine the `Provider` interface capabilities. Perhaps distinguish between `SupportsNativeRAG()` (provider handles it) and `SupportsRAG()` (needs external context injection). The service should rely on these capability flags rather than string matching on the provider name.

## 3. Code Quality Metrics

- **Readability**: High. Variable names are clear, and functions are generally well-scoped (except for the main service methods).
- **Testability**: Good. `chat_test.go` uses dependency injection effectively to mock providers.
- **Safety**: Good. Context cancellation and timeouts are handled consistently.

## 4. Conclusion

Airborne is in a healthy state. The recommended refactorings are typical for a project effectively moving from "prototype/MVP" to a "production-ready" system supporting multiple integrations. Addressing the provider duplication now will pay significant dividends for future maintenance.
