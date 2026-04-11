Date Created: Sunday, January 18, 2026 at 12:00:00 PM EST
TOTAL_SCORE: 85/100

# Airborne Codebase Audit & Refactoring Report

## Executive Summary

The Airborne codebase demonstrates a solid architectural foundation, particularly in its handling of multiple LLM providers. The use of a standard `Provider` interface and a `compat` package for OpenAI-compatible APIs significantly reduces code duplication and simplifies the addition of new providers. The project generally follows Go idioms, uses structured logging (`slog`), and implements necessary security measures like SSRF protection and error sanitization.

However, as the application grows, the `ChatService` is becoming a bottleneck of complexity, handling too many distinct responsibilities. Additionally, the use of unbounded goroutines for database persistence presents a potential stability risk in high-load scenarios. Addressing these issues now will ensure the system remains maintainable and scalable.

## Detailed Findings

### 1. Architecture & Structure (Score: 25/30)

*   **Strengths:**
    *   **Provider Abstraction:** The `internal/provider` package defines a clear and comprehensive interface. This allows the service layer to be agnostic of the underlying LLM implementation.
    *   **Compatibility Layer:** The `internal/provider/compat` package is a high-value asset. By abstracting the OpenAI protocol, it allows providers like `deepseek`, `together`, and potentially others to be implemented with minimal boilerplate.
    *   **Project Layout:** The directory structure follows standard Go project layouts (`cmd`, `internal`, `pkg`), making it easy to navigate.

*   **Areas for Improvement:**
    *   **Service Layer Bloat:** `internal/service/chat.go` is assuming too many roles. It handles request validation, provider selection, RAG orchestration, response formatting (HTML rendering), and asynchronous persistence. This violates the Single Responsibility Principle.
    *   **Provider Boilerplate:** Even with the `compat` package, each provider has its own package and `client.go` that largely just initializes the compatible client. While small, this is still repetitive.

### 2. Code Quality & Maintainability (Score: 40/50)

*   **Strengths:**
    *   **Readability:** Functions are generally well-named and focused. Complex logic is often broken down (e.g., `prepareRequest` in `ChatService`).
    *   **Context Management:** There is consistent use of `context.Context` for timeouts and cancellation, which is crucial for network-bound applications.
    *   **Error Handling:** The `internal/errors` package provides a good mechanism for sanitizing errors before they reach the client (`SanitizeForClient`).

*   **Areas for Improvement:**
    *   **Magic Strings:** Provider names like `"openai"`, `"gemini"`, `"anthropic"` are hardcoded as string literals in multiple files (`chat.go`, `client.go`, etc.). These should be defined as constants in a shared package (e.g., `pkg/types` or `internal/provider`) to prevent typos and ease refactoring.
    *   **Duplicate Logic in Streaming:** The `GenerateReplyStream` method in `ChatService` has a large switch statement that mirrors logic in `GenerateReply`, particularly regarding HTML rendering and token usage recording. This logic could be unified.

### 3. Safety & Stability (Score: 15/20)

*   **Strengths:**
    *   **Security:** Explicit checks for `admin` permissions for custom Base URLs protect against SSRF. Tenant isolation logic is present.
    *   **Database Access:** Use of `pgx` is robust.

*   **Areas for Improvement:**
    *   **Unbounded Concurrency:** In `ChatService.persistConversation`, the code launches a new goroutine (`go func() { ... }`) for every request. In a high-throughput scenario, this could lead to resource exhaustion (too many goroutines, DB connection starvation).
    *   **Panic Safety:** While not explicitly seen, the lack of a worker pool or panic recovery in these background goroutines could crash the application if a panic occurs within the persistence logic.

## Refactoring Plan

### Priority 1: Stability (High Impact)
**Objective:** Prevent resource exhaustion from asynchronous persistence.

*   **Action:** Implement a **Worker Pool** or use a **Buffered Channel** with a fixed number of consumer goroutines for `persistConversation`.
*   **Benefit:** Limits the number of concurrent DB writes and prevents goroutine explosions during traffic spikes.

### Priority 2: Maintainability (Medium Impact)
**Objective:** Centralize constants and reduce magic strings.

*   **Action:** Create a `ProviderID` type and constants in `internal/provider` (or a dedicated `internal/constants` package).
    ```go
    type ProviderID string
    const (
        ProviderOpenAI    ProviderID = "openai"
        ProviderAnthropic ProviderID = "anthropic"
        // ...
    )
    ```
*   **Action:** Replace all string literals `"openai"`, etc., with these constants.

### Priority 3: Architecture (Medium Impact)
**Objective:** Decompose `ChatService`.

*   **Action:** Extract specific responsibilities into helper structs or services:
    *   `RequestValidator`: Move validation logic here.
    *   `ResponseFormatter`: Handle Markdown/HTML rendering.
    *   `PersistenceService`: Encapsulate the async DB writing logic (including the worker pool mentioned above).
*   **Benefit:** `ChatService` becomes a pure orchestrator, making it easier to test and read.

### Priority 4: Cleanup (Low Impact)
**Objective:** Reduce `GenerateReplyStream` complexity.

*   **Action:** Abstract the `StreamChunk` processing into a handler function that can be shared or at least tested in isolation.

## Conclusion

The Airborne project is in good health. The recommended refactorings are typical for a project transitioning from "prototype/MVP" to "production-ready system." Prioritizing the worker pool for persistence is the most critical technical step to ensure reliability.
